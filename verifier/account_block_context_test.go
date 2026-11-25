package verifier_test

import (
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// noChainBlocks returns, for an address that has no account chain at all,
// a send block above height 1 for each of a few heights, acknowledging the
// frontier momentum.
func noChainBlocks(t *testing.T, z mock.MockZenon) []*nom.AccountBlock {
	t.Helper()
	address := types.PubKeyToAddress([]byte("address-without-an-account-chain"))
	frontier, err := z.Chain().GetFrontierAccountStore(address).Frontier()
	common.FailIfErr(t, err)
	if frontier != nil {
		t.Fatalf("test address %v unexpectedly has an account chain", address)
	}
	momentum, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)

	var blocks []*nom.AccountBlock
	for _, height := range []uint64{2, 3, 1000} {
		blocks = append(blocks, &nom.AccountBlock{
			Version:              1,
			BlockType:            nom.BlockTypeUserSend,
			Address:              address,
			ToAddress:            address,
			Height:               height,
			PreviousHash:         types.NewHash([]byte("previous")),
			MomentumAcknowledged: momentum.Identifier(),
			Amount:               common.Big0,
			TokenStandard:        types.ZnnTokenStandard,
		})
	}
	return blocks
}

// A block above height 1 for an address that has no account chain at all
// must be rejected the same way as a block whose previous block is missing.
// Before the guard in getContext the frontier lookup for such an address
// returned nil and the verifier dereferenced it.
func TestAccountBlockVerifier_NoAccountChain_ReturnsPreviousMissing(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(5)

	v := verifier.NewAccountBlockVerifier(z.Chain(), z.Consensus())
	for _, block := range noChainBlocks(t, z) {
		if err := v.AccountBlock(block); err != verifier.ErrABPreviousMissing {
			t.Errorf("height %d: got %v, want %v", block.Height, err, verifier.ErrABPreviousMissing)
		}
	}
}

// AccountBlockTransaction reaches the same getContext as AccountBlock, so a
// transaction whose block is for an address without an account chain is
// rejected the same way.
func TestAccountBlockVerifier_NoAccountChain_TransactionReturnsPreviousMissing(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(5)

	v := verifier.NewAccountBlockVerifier(z.Chain(), z.Consensus())
	for _, block := range noChainBlocks(t, z) {
		if err := v.AccountBlockTransaction(&nom.AccountBlockTransaction{Block: block}); err != verifier.ErrABPreviousMissing {
			t.Errorf("height %d: got %v, want %v", block.Height, err, verifier.ErrABPreviousMissing)
		}
	}
}

// The guard only applies above height 1: the first block of a new account
// chain, at height 1 with a zero previous hash, is still accepted. User10
// has no account chain in the mock genesis; it gets plasma from a fusion
// and then receives a send as its first block. The mock generates the
// block through the supervisor, which runs both AccountBlock and
// AccountBlockTransaction on it before it is inserted.
func TestAccountBlockVerifier_FirstBlockOfANewAccountChainIsAccepted(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(5)

	frontier, err := z.Chain().GetFrontierAccountStore(g.User10.Address).Frontier()
	common.FailIfErr(t, err)
	if frontier != nil {
		t.Fatalf("User10 unexpectedly has an account chain at height %d", frontier.Height)
	}

	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User10.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	send := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User10.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(100),
	}, nil, mock.SkipVmChanges)
	z.InsertMomentumsTo(8) // the send, the fusion and its contract receive

	receive := z.InsertReceiveBlock(send.Header(), nil, nil, mock.SkipVmChanges)
	if receive.Height != 1 || !receive.PreviousHash.IsZero() {
		t.Fatalf("first block of User10 at height %d with previous %v, want height 1 and a zero previous hash", receive.Height, receive.PreviousHash)
	}
	frontier, err = z.Chain().GetFrontierAccountStore(g.User10.Address).Frontier()
	common.FailIfErr(t, err)
	if frontier == nil || frontier.Hash != receive.Hash {
		t.Fatalf("User10's account chain does not start with the inserted block: frontier %v", frontier)
	}
}

type momentumStoreCountingChain struct {
	chain.Chain
	getMomentumStoreCalls int
}

func (c *momentumStoreCountingChain) GetMomentumStore(identifier types.HashHeight) store.Momentum {
	c.getMomentumStoreCalls++
	return c.Chain.GetMomentumStore(identifier)
}

// Verifying or applying a crafted embedded-address block with an old
// MomentumAcknowledged must not materialize a rolled-back historical momentum
// store via GetMomentumStore.
func TestAccountBlockVerifier_EmbeddedAddressDoesNotCallGetMomentumStore(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(2)

	// Confirm and auto-receive a send to PlasmaContract so PlasmaContract has a
	// real frontier and an already-received historical send block.
	send := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User10.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertMomentumsTo(8)

	frontierStore := z.Chain().GetFrontierMomentumStore()
	oldMomentum, err := frontierStore.GetMomentumByHeight(1)
	common.FailIfErr(t, err)
	sendConfirmHeight, err := frontierStore.GetBlockConfirmationHeight(send.Hash)
	common.FailIfErr(t, err)
	sendConfirmMomentum, err := frontierStore.GetMomentumByHeight(sendConfirmHeight)
	common.FailIfErr(t, err)
	plasmaFrontier, err := z.Chain().GetFrontierAccountStore(types.PlasmaContract).Frontier()
	common.FailIfErr(t, err)
	if plasmaFrontier == nil {
		t.Fatal("expected PlasmaContract to have a frontier block")
	}

	unregisteredEmbedded := types.Address{types.ContractAddrByte, 0xaa, 0xbb}
	chainID := z.Chain().ChainIdentifier()

	cases := []struct {
		name    string
		block   *nom.AccountBlock
		wantErr error
	}{
		{
			name: "contract-receive at real frontier with old MA and non-zero ZTS",
			block: &nom.AccountBlock{
				Version:              1,
				ChainIdentifier:      chainID,
				BlockType:            nom.BlockTypeContractReceive,
				Address:              types.PlasmaContract,
				Height:               plasmaFrontier.Height + 1,
				PreviousHash:         plasmaFrontier.Hash,
				MomentumAcknowledged: oldMomentum.Identifier(),
				TokenStandard:        types.ZnnTokenStandard,
				FromBlockHash:        types.NewHash([]byte("garbage")),
			},
			wantErr: verifier.ErrABZtsMustBeZero,
		},
		{
			name: "arbitrary embedded-class address height 1 with old MA",
			block: &nom.AccountBlock{
				Version:              1,
				ChainIdentifier:      chainID,
				BlockType:            nom.BlockTypeUserSend,
				Address:              unregisteredEmbedded,
				Height:               1,
				PreviousHash:         types.ZeroHash,
				MomentumAcknowledged: oldMomentum.Identifier(),
				Amount:               common.Big0,
			},
			wantErr: verifier.ErrABTypeMustBeContract,
		},
		{
			name: "contract-receive replaying an already-received historical send",
			block: &nom.AccountBlock{
				Version:              1,
				ChainIdentifier:      chainID,
				BlockType:            nom.BlockTypeContractReceive,
				Address:              types.PlasmaContract,
				Height:               plasmaFrontier.Height + 1,
				PreviousHash:         plasmaFrontier.Hash,
				MomentumAcknowledged: sendConfirmMomentum.Identifier(),
				FromBlockHash:        send.Hash,
			},
			// Contract receives advance the sequencer index rather than the user
			// received-set, so replaying an already-received send is rejected by
			// sequencer() with ErrABSequencerNothing.
			wantErr: verifier.ErrABSequencerNothing,
		},
		{
			name: "embedded address with non-existent MA returns ErrABMAMissing",
			block: &nom.AccountBlock{
				Version:         1,
				ChainIdentifier: chainID,
				BlockType:       nom.BlockTypeContractReceive,
				Address:         types.PlasmaContract,
				Height:          plasmaFrontier.Height + 1,
				PreviousHash:    plasmaFrontier.Hash,
				MomentumAcknowledged: types.HashHeight{
					Hash:   types.NewHash([]byte("missing")),
					Height: 9999,
				},
			},
			wantErr: verifier.ErrABMAMissing,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counting := &momentumStoreCountingChain{Chain: z.Chain()}
			v := verifier.NewAccountBlockVerifier(counting, z.Consensus())
			if err := v.AccountBlock(tc.block); err != tc.wantErr {
				t.Fatalf("AccountBlock: got %v, want %v", err, tc.wantErr)
			}
			if err := v.AccountBlockTransaction(&nom.AccountBlockTransaction{Block: tc.block}); err != nil && err != tc.wantErr && tc.wantErr != verifier.ErrABMAMissing {
				// AccountBlockTransaction runs hash/signature rather than accountBlockVerifier.all(),
				// so only check that it never calls GetMomentumStore.
			}
			supervisor := vm.NewSupervisor(counting, z.Consensus())
			if _, err := supervisor.ApplyBlock(tc.block); err != tc.wantErr {
				t.Fatalf("Supervisor.ApplyBlock: got %v, want %v", err, tc.wantErr)
			}
			if counting.getMomentumStoreCalls != 0 {
				t.Fatalf("expected 0 GetMomentumStore calls, got %d", counting.getMomentumStoreCalls)
			}
		})
	}
}

// Sequencer checks for contract receives must reject out-of-order sends and
// sends confirmed after MomentumAcknowledged using frontierStore alone,
// without calling GetMomentumStore.
func TestAccountBlockVerifier_EmbeddedSequencerRejectsOutOfOrderWithoutRollback(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(2)

	send1 := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User10.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	send2 := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User10.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	// InsertNewMomentum commits send1 and send2 in the new momentum and then
	// auto-generates their contract receives as uncommitted pool blocks on top
	// of PlasmaContract's confirmed frontier. Verify against that confirmed
	// frontier, where send1 is at the sequencer head and send2 is second.
	frontierStore := z.Chain().GetFrontierMomentumStore()
	plasmaConfirmed := frontierStore.GetAccountStore(types.PlasmaContract)
	front, err := plasmaConfirmed.SequencerFront(frontierStore.GetAccountMailbox(types.PlasmaContract))
	common.FailIfErr(t, err)
	if front == nil || front.Hash != send1.Hash {
		t.Fatalf("expected send1 (%v) at sequencer front, got %v", send1.Hash, front)
	}
	confirmHeight, err := frontierStore.GetBlockConfirmationHeight(send2.Hash)
	common.FailIfErr(t, err)
	confirmMomentum, err := frontierStore.GetMomentumByHeight(confirmHeight)
	common.FailIfErr(t, err)
	plasmaFrontier := plasmaConfirmed.Identifier()

	counting := &momentumStoreCountingChain{Chain: z.Chain()}
	v := verifier.NewAccountBlockVerifier(counting, z.Consensus())

	// Receiving send1 (head of sequencer) passes verification with 0 GetMomentumStore calls.
	validReceive := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      z.Chain().ChainIdentifier(),
		BlockType:            nom.BlockTypeContractReceive,
		Address:              types.PlasmaContract,
		Height:               plasmaFrontier.Height + 1,
		PreviousHash:         plasmaFrontier.Hash,
		MomentumAcknowledged: confirmMomentum.Identifier(),
		FromBlockHash:        send1.Hash,
	}
	if err := v.AccountBlock(validReceive); err != nil {
		t.Fatalf("expected valid receive for sequencer front to pass, got %v", err)
	}

	// Receiving send2 out of order fails with ErrABSequencerNotNext and 0 GetMomentumStore calls.
	outOfOrder := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      z.Chain().ChainIdentifier(),
		BlockType:            nom.BlockTypeContractReceive,
		Address:              types.PlasmaContract,
		Height:               plasmaFrontier.Height + 1,
		PreviousHash:         plasmaFrontier.Hash,
		MomentumAcknowledged: confirmMomentum.Identifier(),
		FromBlockHash:        send2.Hash,
	}
	if err := v.AccountBlock(outOfOrder); err != verifier.ErrABSequencerNotNext {
		t.Fatalf("expected ErrABSequencerNotNext, got %v", err)
	}
	if counting.getMomentumStoreCalls != 0 {
		t.Fatalf("expected 0 GetMomentumStore calls, got %d", counting.getMomentumStoreCalls)
	}
}
