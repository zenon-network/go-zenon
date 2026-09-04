package chain

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"sync"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

func TestAccountPool_filterBlocksToCommit(t *testing.T) {
	ap := accountPool{}
	MaxAccountBlocksInMomentum = 2
	common.Expect(t, len(ap.filterBlocksToCommit([]*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeUserSend},
		{Height: 2, BlockType: nom.BlockTypeUserSend},
		{Height: 3, BlockType: nom.BlockTypeUserSend},
	})), 2)

	common.Expect(t, len(ap.filterBlocksToCommit([]*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeContractSend},
		{Height: 2, BlockType: nom.BlockTypeContractSend},
		{Height: 3, BlockType: nom.BlockTypeUserReceive},
	})), 0)
}

// memStable backs an account pool with one in-memory stable DB per address.
type memStable struct {
	dbs map[types.Address]db.DB
}

func (s *memStable) GetStableAccountDB(address types.Address) db.DB {
	if s.dbs == nil {
		s.dbs = map[types.Address]db.DB{}
	}
	if s.dbs[address] == nil {
		s.dbs[address] = db.NewMemDB()
	}
	return s.dbs[address]
}

// poolBlockChain builds a height-1 base block, two copies of a height-2 block
// that share an identifier but differ in a field that is stored without being
// hashed, and a height-3 descendant on top of the first copy.
func poolBlockChain() (base, a, b, descendant *nom.AccountBlock) {
	address := types.ParseAddressPanic("z1qqjfammypam0sjhzst0jfjth60vy9g8w0g5lhg")
	base = &nom.AccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       nom.BlockTypeUserReceive,
		Address:         address,
		Height:          1,
		Amount:          big.NewInt(0),
		Signature:       bytes.Repeat([]byte{0x11}, 64),
	}
	base.Hash = base.ComputeHash()
	a = &nom.AccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       nom.BlockTypeUserReceive,
		Address:         address,
		Height:          2,
		PreviousHash:    base.Hash,
		Amount:          big.NewInt(0),
		Signature:       bytes.Repeat([]byte{0xaa}, 64),
	}
	a.Hash = a.ComputeHash()
	b = a.Copy()
	b.Signature = bytes.Repeat([]byte{0xbb}, 64)
	descendant = &nom.AccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       nom.BlockTypeUserSend,
		Address:         address,
		Height:          3,
		PreviousHash:    a.Hash,
		Amount:          big.NewInt(0),
		Signature:       bytes.Repeat([]byte{0x33}, 64),
	}
	descendant.Hash = descendant.ComputeHash()
	return base, a, b, descendant
}

func poolTransaction(block *nom.AccountBlock) *nom.AccountBlockTransaction {
	return &nom.AccountBlockTransaction{Block: block, Changes: db.NewPatch()}
}

func TestAccountPool_ForceAddReplacesSameIdentifierWithDifferentBytes(t *testing.T) {
	base, a, b, descendant := poolBlockChain()
	common.Expect(t, a.Identifier(), b.Identifier())

	ap := newAccountPool(&memStable{})
	locker := &sync.Mutex{}
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(base)))
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(a)))
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(descendant)))
	common.FailIfErr(t, ap.ForceAddAccountBlockTransaction(locker, poolTransaction(b)))

	frontier := ap.GetFrontierAccountStore(a.Address)
	common.Expect(t, frontier.Identifier(), b.Identifier())
	stored, err := frontier.ByHeight(2)
	common.FailIfErr(t, err)
	common.ExpectBytes(t, stored.Signature, "0x"+hex.EncodeToString(b.Signature))
}

func TestAccountPool_AddKeepsFirstVariantOfSameIdentifier(t *testing.T) {
	base, a, b, _ := poolBlockChain()

	ap := newAccountPool(&memStable{})
	locker := &sync.Mutex{}
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(base)))
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(a)))
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(b)))

	stored, err := ap.GetFrontierAccountStore(a.Address).ByHeight(2)
	common.FailIfErr(t, err)
	common.ExpectBytes(t, stored.Signature, "0x"+hex.EncodeToString(a.Signature))
}

func TestAccountPool_RestoreUncommittedRevertsForcedReplacement(t *testing.T) {
	base, a, b, descendant := poolBlockChain()

	ap := newAccountPool(&memStable{})
	locker := &sync.Mutex{}
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(base)))
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(a)))
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(descendant)))

	patchesBefore := poolPatchDumps(ap, a.Address, base, a, descendant)

	snapshot := ap.SnapshotUncommitted(locker, []types.Address{a.Address})
	common.FailIfErr(t, ap.ForceAddAccountBlockTransaction(locker, poolTransaction(b)))
	common.Expect(t, ap.GetFrontierAccountStore(a.Address).Identifier(), b.Identifier())

	common.FailIfErr(t, ap.RestoreUncommitted(locker, snapshot))

	frontier := ap.GetFrontierAccountStore(a.Address)
	common.Expect(t, frontier.Identifier(), descendant.Identifier())
	stored, err := frontier.ByHeight(2)
	common.FailIfErr(t, err)
	common.ExpectBytes(t, stored.Signature, "0x"+hex.EncodeToString(a.Signature))
	common.Expect(t, len(ap.GetUncommittedAccountBlocksByAddress(a.Address)), 3)
	expectPatchDumps(t, patchesBefore, poolPatchDumps(ap, a.Address, base, a, descendant))
}

// poolPatchDumps returns the serialized patch of each block as the pool holds it.
func poolPatchDumps(ap *accountPool, address types.Address, blocks ...*nom.AccountBlock) [][]byte {
	dumps := make([][]byte, 0, len(blocks))
	for _, block := range blocks {
		patch := ap.GetPatch(address, block.Identifier())
		if patch == nil {
			dumps = append(dumps, nil)
			continue
		}
		dumps = append(dumps, patch.Dump())
	}
	return dumps
}

func expectPatchDumps(t *testing.T, before, after [][]byte) {
	t.Helper()
	common.Expect(t, len(after), len(before))
	for i := range before {
		if !bytes.Equal(before[i], after[i]) {
			t.Fatalf("patch %d changed after restore: %d bytes before, %d bytes after", i, len(before[i]), len(after[i]))
		}
	}
}

// embeddedReceiveChain builds a contract receive at height 2 that carries two
// descendant contract sends, on top of a height-1 base block.
func embeddedReceiveChain() (base, receive *nom.AccountBlock) {
	address := types.PillarContract
	base = &nom.AccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       nom.BlockTypeContractReceive,
		Address:         address,
		Height:          1,
		Amount:          big.NewInt(0),
	}
	base.Hash = base.ComputeHash()
	previous := base.Hash
	descendants := make([]*nom.AccountBlock, 0, 2)
	for height := uint64(2); height <= 3; height++ {
		d := &nom.AccountBlock{
			Version:         1,
			ChainIdentifier: 1,
			BlockType:       nom.BlockTypeContractSend,
			Address:         address,
			Height:          height,
			PreviousHash:    previous,
			Amount:          big.NewInt(0),
		}
		d.Hash = d.ComputeHash()
		previous = d.Hash
		descendants = append(descendants, d)
	}
	receive = &nom.AccountBlock{
		Version:          1,
		ChainIdentifier:  1,
		BlockType:        nom.BlockTypeContractReceive,
		Address:          address,
		Height:           4,
		PreviousHash:     previous,
		Amount:           big.NewInt(0),
		DescendantBlocks: descendants,
	}
	receive.Hash = receive.ComputeHash()
	return base, receive
}

func TestAccountPool_RestoreUncommittedKeepsBatchedEmbeddedTransaction(t *testing.T) {
	base, receive := embeddedReceiveChain()

	ap := newAccountPool(&memStable{})
	locker := &sync.Mutex{}
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(base)))
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(receive)))
	all := append([]*nom.AccountBlock{base}, receive.DescendantBlocks...)
	all = append(all, receive)
	common.Expect(t, ap.GetFrontierAccountStore(receive.Address).Identifier(), receive.Identifier())
	patchesBefore := poolPatchDumps(ap, receive.Address, all...)

	snapshot := ap.SnapshotUncommitted(locker, []types.Address{receive.Address})
	// Roll the account back to the base block, as a forced insert of a
	// competing block would.
	replacement := base.Copy()
	replacement.Height = 2
	replacement.PreviousHash = base.Hash
	replacement.Hash = replacement.ComputeHash()
	common.FailIfErr(t, ap.ForceAddAccountBlockTransaction(locker, poolTransaction(replacement)))
	common.Expect(t, ap.GetFrontierAccountStore(receive.Address).Identifier(), replacement.Identifier())

	common.FailIfErr(t, ap.RestoreUncommitted(locker, snapshot))

	frontier := ap.GetFrontierAccountStore(receive.Address)
	common.Expect(t, frontier.Identifier(), receive.Identifier())
	for _, block := range all {
		stored, err := frontier.ByHeight(block.Height)
		common.FailIfErr(t, err)
		if stored == nil || !stored.EqualBytes(block) {
			t.Fatalf("block at height %d not restored byte-identically", block.Height)
		}
	}
	expectPatchDumps(t, patchesBefore, poolPatchDumps(ap, receive.Address, all...))
}

func TestAccountPool_RestoreUncommittedEmptySnapshotClearsAddress(t *testing.T) {
	base, a, _, _ := poolBlockChain()

	ap := newAccountPool(&memStable{})
	locker := &sync.Mutex{}
	snapshot := ap.SnapshotUncommitted(locker, []types.Address{a.Address, a.Address})
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(base)))
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(a)))

	common.FailIfErr(t, ap.RestoreUncommitted(locker, snapshot))

	common.Expect(t, ap.GetFrontierAccountStore(a.Address).Identifier(), types.ZeroHashHeight)
	common.Expect(t, len(ap.GetAllUncommittedAccountBlocks()), 0)
	common.FailIfErr(t, ap.RestoreUncommitted(locker, nil))
}
