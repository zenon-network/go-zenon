// Package e2e is the end-to-end driver for a running go-zenon network.
//
// It builds, signs and publishes account blocks over JSON-RPC, using the
// repository's own wallet, block, ABI and proof-of-work packages, and reads
// the results back through the public RPC getters. It needs no SDK.
//
// The cases live in the *_test.go files behind the "e2e" build tag, so
// "go test ./..." never starts them. See docs/TESTING.md, procedure P9.
package e2e

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/tyler-smith/go-bip39"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/pow"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/rpc/server"
	"github.com/zenon-network/go-zenon/wallet"
)

// DevMnemonic is the committed devnet mnemonic (cmd/devnet-keygen). It holds
// no value outside a local devnet.
const DevMnemonic = "abstract affair idle position alien fluid board ordinary exist afraid chapter wood wood guide sun walnut crew perfect place firm poverty model side million"

// Derivation indexes of the devnet accounts (docker/devnet/README.md).
const (
	IdxProducer1 = 0 // pillar 1 producer; also the genesis spork address
	IdxOwner1    = 1 // pillar 1 owner; has fused plasma at genesis
	IdxOwner2    = 5
	IdxOwner3    = 7
	IdxFunder    = 3 // funded general account
)

// Contract-receive status values (vm/vm.go: resultSuccess, resultFail).
const (
	StatusSuccess uint64 = 1
	StatusFail    uint64 = 2
)

// Net is a connection to one network through one or more RPC endpoints.
// The first endpoint is used for publishing; all of them are used when a
// case asks whether every node agrees.
type Net struct {
	URLs    []string
	ChainID uint64

	clients []*server.Client
	ks      *wallet.KeyStore

	// Containers are node containers reached with docker exec, for nodes
	// that publish no RPC port. Checks that say "every node" use them too.
	Containers []string

	mu       sync.Mutex
	locks    map[types.Address]*sync.Mutex
	accepted map[types.Address]int
	signed   map[types.Address][]*nom.AccountBlock // accepted blocks, as the driver signed them
	// due: the sends an address is known to be owed, by hash, with the
	// content they must have. A send the driver signed, or a descendant of a
	// contract receive that the driver waited for. ReceiveAll receives these
	// and no other, and every one of them not received yet.
	due map[types.Address]map[types.Hash]*nom.AccountBlock
	// received: send hash -> the receive the driver published for it and
	// the node accepted. A send is received once; listed again while that
	// receive is still on the chain, it fails.
	received map[types.Hash]types.Hash
	// reserved: sends a ReceiveAll is publishing a receive for right now; no
	// other call may receive them meanwhile.
	reserved map[types.Hash]bool
	// madeBy: for a send a contract made, the call (the send block a driver
	// account signed, or the update call a pillar sent) whose receive made
	// it, followed through a second contract for a hop.
	madeBy map[types.Hash]*nom.AccountBlock
	// madeIn: for a send a contract made, the hash of the contract receive
	// that made it.
	madeIn map[types.Hash]types.Hash
	// madeOK: whether that receive executed successfully.
	madeOK map[types.Hash]bool
	// expected: payments a case announced that no driver account asks for,
	// such as an accelerator payout a pillar's update triggers. A contract
	// send of unknown origin is owed only if it matches one of these, and
	// then claims it.
	expected []*Expectation
	// hops: sends one contract made to another in answer to a call. What
	// the second contract sends on for them is owed too; resolveHops
	// follows them once their receives exist.
	hops    []*nom.AccountBlock
	refused []types.Hash
	// contradictions: disagreements between endpoints seen while looking
	// for the origin of a payment. Once seen, ReceiveAll fails: a later
	// agreement does not undo it.
	contradictions []string
	// refusedBy: the account each refused block was for.
	refusedBy map[types.Hash]types.Address
}

// Expectation is a payment a case expects from a contract without a driver
// account asking for it.
type Expectation struct {
	To, From types.Address
	Token    types.ZenonTokenStandard
	Amount   *big.Int
	// Method: the first four bytes of the call whose receive must have made
	// the payment. Group: announcements with one group name must all be met
	// by sends of one and the same receive.
	Method  []byte
	Group   string
	Data    []byte     // the data the payment must carry (nil: any)
	Claimed types.Hash // the send that matched it, once one did
	Call    types.Hash // the call whose receive made that send
	Recv    types.Hash // the receive that made it
}

// Expect announces a payment from a contract that no driver call makes,
// made by the receive of a call to the given method (its four-byte
// selector), together with the other announcements of the same group.
// ReceiveAll for that address does not finish until it has arrived.
func (n *Net) Expect(to, from types.Address, token types.ZenonTokenStandard, amount *big.Int, method []byte, group string, data []byte) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.expected = append(n.expected, &Expectation{To: to, From: from, Token: token, Amount: new(big.Int).Set(amount), Method: append([]byte(nil), method[:4]...), Group: group,
		Data: append([]byte(nil), data...)})
}

// claimExpected marks the first unclaimed expectation the send matches as
// met by it, and reports whether there was one. The caller holds n.mu.
func (n *Net) claimExpected(b *nom.AccountBlock) bool {
	for _, e := range n.expected {
		if e.Claimed == b.Hash {
			return true
		}
	}
	call, recv := n.madeBy[b.Hash], n.madeIn[b.Hash]
	if call == nil || recv == (types.Hash{}) || !n.madeOK[b.Hash] {
		return false // only a receive that executed successfully pays an announcement
	}
	for _, e := range n.expected {
		if e.Claimed != (types.Hash{}) || e.To != b.ToAddress || e.From != b.Address || e.Token != b.TokenStandard || b.Amount == nil || e.Amount.Cmp(b.Amount) != 0 {
			continue
		}
		if e.Data != nil && !bytes.Equal(e.Data, b.Data) {
			continue
		}
		if len(call.Data) < 4 || !bytes.Equal(call.Data[:4], e.Method) {
			continue
		}
		sameCall := true
		for _, o := range n.expected {
			if o != e && o.Group == e.Group && o.Call != (types.Hash{}) && (o.Call != call.Hash || o.Recv != recv) {
				sameCall = false
			}
		}
		if !sameCall {
			continue
		}
		e.Claimed, e.Call, e.Recv = b.Hash, call.Hash, recv
		return true
	}
	return false
}

// unmet lists the announced payments to the address that no send has met.
func (n *Net) unmet(to types.Address) []*Expectation {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []*Expectation
	for _, e := range n.expected {
		if e.To == to && e.Claimed == (types.Hash{}) {
			out = append(out, e)
		}
	}
	return out
}

// Dial connects to every endpoint and derives the devnet key store.
func Dial(urls []string, chainID uint64) (*Net, error) {
	n := &Net{URLs: urls, ChainID: chainID, locks: map[types.Address]*sync.Mutex{}, accepted: map[types.Address]int{}, signed: map[types.Address][]*nom.AccountBlock{}, due: map[types.Address]map[types.Hash]*nom.AccountBlock{}, received: map[types.Hash]types.Hash{}, refusedBy: map[types.Hash]types.Address{}, reserved: map[types.Hash]bool{}, madeBy: map[types.Hash]*nom.AccountBlock{}, madeIn: map[types.Hash]types.Hash{}, madeOK: map[types.Hash]bool{}}
	for _, u := range urls {
		c, err := server.Dial(u)
		if err != nil {
			return nil, fmt.Errorf("dial %s: %w", u, err)
		}
		n.clients = append(n.clients, c)
	}
	entropy, err := bip39.EntropyFromMnemonic(DevMnemonic)
	if err != nil {
		return nil, err
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return nil, err
	}
	n.ks = &wallet.KeyStore{Entropy: entropy, Seed: bip39.NewSeed(mnemonic, ""), Mnemonic: mnemonic}
	return n, nil
}

// Key returns the key pair at the given derivation index.
func (n *Net) Key(index uint32) *wallet.KeyPair {
	_, kp, err := n.ks.DeriveForIndexPath(index)
	if err != nil {
		panic(err)
	}
	return kp
}

// Call runs one RPC method on the first endpoint.
// Every call has a deadline: a node that stops answering is an error after a
// minute, not a wait without end.
func (n *Net) Call(result interface{}, method string, args ...interface{}) error {
	return n.CallOn(0, result, method, args...)
}

// CallTimeout is Call with a limit on how long the node may take to answer.
func (n *Net) CallTimeout(d time.Duration, result interface{}, method string, args ...interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return n.clients[0].CallContext(ctx, result, method, args...)
}

// CallOn runs one RPC method on endpoint i.
func (n *Net) CallOn(i int, result interface{}, method string, args ...interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return n.clients[i].CallContext(ctx, result, method, args...)
}

// CallOnCtx is CallOn bounded by ctx as well as by the minute.
func (n *Net) CallOnCtx(ctx context.Context, i int, result interface{}, method string, args ...interface{}) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	return n.clients[i].CallContext(ctx, result, method, args...)
}

// Frontier returns the frontier momentum of the first endpoint.
func (n *Net) Frontier() (*api.Momentum, error) {
	var m *api.Momentum
	if err := n.Call(&m, "ledger.getFrontierMomentum"); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("no frontier momentum")
	}
	return m, nil
}

// WaitHeight blocks until the first endpoint reaches the height.
func (n *Net) WaitHeight(height uint64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		m, err := n.Frontier()
		if err == nil && m.Height >= height {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("height %d not reached in %s (last error: %v)", height, timeout, err)
		}
		time.Sleep(time.Second)
	}
}

// Balance returns the confirmed balance of one token.
func (n *Net) Balance(addr types.Address, zts types.ZenonTokenStandard) (*big.Int, error) {
	var info *api.AccountInfo
	if err := n.Call(&info, "ledger.getAccountInfoByAddress", addr); err != nil {
		return nil, err
	}
	if info == nil {
		return nil, fmt.Errorf("no account info for %s", addr)
	}
	if info.Address != addr {
		return nil, fmt.Errorf("asked for the account info of %s, got that of %s", addr, info.Address)
	}
	if info.BalanceInfoMap[zts] == nil {
		return big.NewInt(0), nil // the node lists only tokens the account holds
	}
	return info.BalanceInfoMap[zts].Balance, nil
}

// StageError says where a block was stopped: while the driver prepared it
// ("prepare"), by the node when it was published ("publish"), before the
// publish because a deadline passed ("deadline"), or nowhere known: the
// publish got no answer from the node ("publish-unknown"). A case that
// expects the node to refuse a block must see "publish".
type StageError struct {
	Stage string
	Err   error
}

func (e *StageError) Error() string { return e.Stage + ": " + e.Err.Error() }
func (e *StageError) Unwrap() error { return e.Err }

// Accepted is the number of blocks of the account the node accepted from
// this driver. For an account that only this driver uses it is the account
// height the chain must show.
func (n *Net) Accepted(addr types.Address) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.accepted[addr]
}

// Signed lists the blocks of the account that the node accepted, in order,
// exactly as the driver signed them. It is the driver's own record of what
// the account chain must contain, independent of what a node reports.
func (n *Net) Signed(addr types.Address) []*nom.AccountBlock {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]*nom.AccountBlock(nil), n.signed[addr]...)
}

// SignedBlock finds an accepted block of any account by its hash.
func (n *Net) SignedBlock(hash types.Hash) *nom.AccountBlock {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, list := range n.signed {
		for _, b := range list {
			if b.Hash == hash {
				return b
			}
		}
	}
	return nil
}

// KnownOnContainers asks every container whether it has the account block.
// It returns the containers that do, and an error if one could not be asked.
func (n *Net) KnownOnContainers(hash types.Hash) ([]string, error) {
	var have []string
	for _, c := range n.Containers {
		// Unknown is the answer {"result": null} and nothing else: a reply
		// without a result member says nothing about the block.
		var reply map[string]json.RawMessage
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"ledger.getAccountBlockByHash","params":[%q]}`, hash.String())
		out, err := exec.Command("docker", "exec", c, "wget", "-qO-", "-T", "10", "--post-data", body,
			"--header", "Content-Type: application/json", "http://localhost:35997").Output()
		if err != nil {
			return nil, fmt.Errorf("container %s: %v", c, err)
		}
		if err = json.Unmarshal(out, &reply); err != nil {
			return nil, fmt.Errorf("container %s: %s (%v)", c, out, err)
		}
		result, answered := reply["result"]
		if e, failed := reply["error"]; !answered || failed && string(e) != "null" || string(reply["jsonrpc"]) != `"2.0"` || string(reply["id"]) != "1" {
			return nil, fmt.Errorf("container %s gave no result for block %s: %s", c, hash, out)
		}
		if string(result) != "null" {
			have = append(have, c)
		}
	}
	return have, nil
}

// owe records a send as due to its recipient, with the content it must
// have; the caller holds n.mu.
func (n *Net) owe(b *nom.AccountBlock) {
	if n.due[b.ToAddress] == nil {
		n.due[b.ToAddress] = map[types.Hash]*nom.AccountBlock{}
	}
	if n.due[b.ToAddress][b.Hash] == nil {
		c := *b
		n.due[b.ToAddress][b.Hash] = &c
	}
}

// Due returns the content a send to the address must have if the address
// is known to be owed it, and nil otherwise.
func (n *Net) Due(to types.Address, hash types.Hash) *nom.AccountBlock {
	n.mu.Lock()
	known := n.due[to][hash]
	n.mu.Unlock()
	if known == nil {
		n.resolveHops() // an error shows again where ReceiveAll resolves the hops
		n.mu.Lock()
		known = n.due[to][hash]
		n.mu.Unlock()
	}
	if known == nil {
		known = n.contractMade(to, hash)
	}
	return known
}

// owedOpen lists the sends owed to the address that the driver has not
// received yet.
func (n *Net) owedOpen(to types.Address) []*nom.AccountBlock {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []*nom.AccountBlock
	for h, b := range n.due[to] {
		if _, done := n.received[h]; !done {
			out = append(out, b)
		}
	}
	return out
}

// contractMade checks, on every endpoint, that the send is a block an
// embedded contract made while answering a call: it is on the contract's own
// chain, and the contract receive that made it lists it among its descendant
// blocks. Descendants take the heights just below their receive (see
// nom.AccountBlockTransaction GetCommits), so that receive is the nearest
// one above the send, with only sibling sends in between. Contracts also pay
// out when a pillar calls their update method, which no driver account
// signs; such a payment is owed only if a case announced it with Expect, by
// payer, recipient, token and amount, and it then claims that announcement.
func (n *Net) contractMade(to types.Address, hash types.Hash) *nom.AccountBlock {
	var first *api.AccountBlock
	var call *api.AccountBlock
	var producer types.Hash
	var producerStatus uint64
	var producerAt api.AccountBlockConfirmationDetail
	// Three outcomes are kept apart: a failed read or data not there yet
	// returns nil, to be read again; returned data that conflicts with what
	// another endpoint returned is a contradiction; only agreement owes the
	// send.
	for i := range n.URLs {
		blk, err := n.Block(i, hash)
		if err != nil || blk == nil {
			return nil
		}
		if first != nil {
			if blk.Address != first.Address || blk.ToAddress != first.ToAddress || blk.BlockType != first.BlockType || blk.TokenStandard != first.TokenStandard ||
				blk.Amount == nil || blk.Amount.Cmp(first.Amount) != 0 || blk.Height != first.Height || !bytes.Equal(blk.Data, first.Data) {
				n.contradict("%s shows contract send %s differently from %s: from %s to %s, type %d, %s of %s at height %d", n.URLs[i], hash, n.URLs[0],
					blk.Address, blk.ToAddress, blk.BlockType, blk.Amount, blk.TokenStandard, blk.Height)
				return nil
			}
		} else if blk.ToAddress != to || blk.BlockType != nom.BlockTypeContractSend || !types.IsEmbeddedAddress(blk.Address) || blk.Amount == nil {
			return nil
		} else {
			first = blk
		}
		listed := false
		for h, steps := blk.Height+1, 0; steps < 32 && !listed; h, steps = h+1, steps+1 {
			var page struct {
				List []*api.AccountBlock `json:"list"`
			}
			if err := n.CallOn(i, &page, "ledger.getAccountBlocksByHeight", blk.Address, h, 1); err != nil || len(page.List) != 1 || page.List[0] == nil || page.List[0].Height != h || page.List[0].Address != blk.Address {
				return nil
			}
			below := page.List[0]
			if below.BlockType == nom.BlockTypeContractSend {
				continue // a sibling: another block the same receive made
			}
			if below.BlockType != nom.BlockTypeContractReceive {
				return nil
			}
			// The receive another endpoint showed making the send must be the
			// one here too, whether or not it lists the send.
			if producer != (types.Hash{}) && producer != below.Hash {
				n.contradict("%s: send %s was made by receive %s, %s says %s", n.URLs[i], hash, below.Hash, n.URLs[0], producer)
				return nil
			}
			for _, d := range below.DescendantBlocks {
				listed = listed || d.Hash == hash
			}
			if !listed {
				if producer != (types.Hash{}) {
					n.contradict("%s: receive %s does not list send %s, which %s says it made", n.URLs[i], below.Hash, hash, n.URLs[0])
				}
				return nil
			}
			// The call that receive answered, the same on every endpoint.
			c, err := n.Block(i, below.FromBlockHash)
			if err != nil || c == nil {
				return nil
			}
			if c.ToAddress != blk.Address || !nom.IsSendBlock(c.BlockType) || call != nil && (c.Hash != call.Hash || !bytes.Equal(c.Data, call.Data)) {
				n.contradict("%s: the call answered by the receive that made %s is %s, not the same as on %s", n.URLs[i], hash, c.Hash, n.URLs[0])
				return nil
			}
			call = c
			producer = below.Hash
			st := receiveStatus(below)
			if i > 0 && st != producerStatus {
				n.contradict("%s: receive %s that made %s ended with status %d, %s says %d", n.URLs[i], below.Hash, hash, st, n.URLs[0], producerStatus)
				return nil
			}
			producerStatus = st
			// The producing receive must be confirmed in a momentum that lists
			// it, and in the same one on every endpoint.
			if below.ConfirmationDetail == nil {
				return nil // not confirmed yet on this endpoint
			}
			if err := n.confirmedIn(i, below); err != nil {
				var failed readError
				if !errors.As(err, &failed) {
					n.contradict("%s: receive %s that made %s: %v", n.URLs[i], below.Hash, hash, err)
				}
				return nil // a failed read is read again
			}
			if i == 0 {
				producerAt = *below.ConfirmationDetail
			} else if below.ConfirmationDetail.MomentumHash != producerAt.MomentumHash || below.ConfirmationDetail.MomentumHeight != producerAt.MomentumHeight {
				n.contradict("%s confirms receive %s, which made %s, in momentum %s at height %d; %s in %s at height %d", n.URLs[i], below.Hash, hash,
					below.ConfirmationDetail.MomentumHash, below.ConfirmationDetail.MomentumHeight, n.URLs[0], producerAt.MomentumHash, producerAt.MomentumHeight)
				return nil
			}
		}
		if !listed {
			return nil
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.madeBy[hash] == nil {
		c := call.AccountBlock
		n.madeBy[hash], n.madeIn[hash], n.madeOK[hash] = &c, producer, producerStatus == StatusSuccess
	}
	if !n.claimExpected(&first.AccountBlock) {
		return nil
	}
	n.owe(&first.AccountBlock)
	return n.due[to][hash]
}

// resolveHops follows every recorded contract-to-contract send whose
// receive exists by now: the send must read back as the one recorded, the
// receive must be the receiving contract's block for that send, and what it
// sent on is owed to its recipients.
// A hop that reads back with other content than recorded, or whose receive
// is not the receiving contract's block for it, is an error. A hop whose
// receive does not exist yet stays recorded; it returns how many do.
func (n *Net) resolveHops() (int, error) {
	n.mu.Lock()
	hops := append([]*nom.AccountBlock(nil), n.hops...)
	n.mu.Unlock()
	for _, hop := range hops {
		// The hop, its pair and what that pair made must read the same on
		// every endpoint before the hop is followed.
		var recv *api.AccountBlock
		pending, lost := false, 0
		for i, url := range n.URLs {
			send, err := n.Block(i, hop.Hash)
			if err != nil {
				return 0, err
			}
			if send == nil {
				lost++
				pending = true
				continue
			}
			if send.Address != hop.Address || send.ToAddress != hop.ToAddress || send.BlockType != hop.BlockType ||
				send.TokenStandard != hop.TokenStandard || send.Amount == nil || hop.Amount == nil || send.Amount.Cmp(hop.Amount) != 0 || !bytes.Equal(send.Data, hop.Data) {
				return 0, fmt.Errorf("%s: contract send %s reads back as type %d from %s to %s, %s of %s; it was recorded as type %d from %s to %s, %s of %s",
					url, hop.Hash, send.BlockType, send.Address, send.ToAddress, send.Amount, send.TokenStandard, hop.BlockType, hop.Address, hop.ToAddress, hop.Amount, hop.TokenStandard)
			}
			if send.PairedAccountBlock == nil {
				pending = true
				continue
			}
			r, err := n.Block(i, send.PairedAccountBlock.Hash)
			if err != nil {
				return 0, err
			}
			if r == nil {
				pending = true
				continue
			}
			if r.FromBlockHash != hop.Hash || r.Address != hop.ToAddress || r.BlockType != nom.BlockTypeContractReceive {
				return 0, fmt.Errorf("%s: contract send %s is paired with %s, a block of type %d on %s receiving %s", url, hop.Hash, r.Hash, r.BlockType, r.Address, r.FromBlockHash)
			}
			if recv == nil {
				recv = r
				continue
			}
			if r.Hash != recv.Hash || receiveStatus(r) != receiveStatus(recv) || len(r.DescendantBlocks) != len(recv.DescendantBlocks) {
				return 0, fmt.Errorf("%s: contract send %s is received by %s, status %d, with %d blocks; %s says %s, status %d, with %d",
					url, hop.Hash, r.Hash, receiveStatus(r), len(r.DescendantBlocks), n.URLs[0], recv.Hash, receiveStatus(recv), len(recv.DescendantBlocks))
			}
			for k := range r.DescendantBlocks {
				if !sameBlock(r.DescendantBlocks[k], recv.DescendantBlocks[k]) {
					return 0, fmt.Errorf("%s: the receive %s of contract send %s made %+v where %s says %+v", url, r.Hash, hop.Hash, r.DescendantBlocks[k], n.URLs[0], recv.DescendantBlocks[k])
				}
			}
		}
		if lost == len(n.URLs) {
			return 0, fmt.Errorf("contract send %s, recorded as owed onward, is unknown to every endpoint: lost", hop.Hash)
		}
		if pending {
			continue
		}
		n.mu.Lock()
		for _, d := range recv.DescendantBlocks {
			if d.BlockType != nom.BlockTypeContractSend || d.Address != recv.Address {
				n.mu.Unlock()
				return 0, fmt.Errorf("the receive %s of contract send %s made a block of type %d from %s; a contract makes only contract sends of its own", recv.Hash, hop.Hash, d.BlockType, d.Address)
			}
			if d.Address == recv.Address && nom.IsSendBlock(d.BlockType) {
				n.owe(d)
				if n.madeBy[d.Hash] == nil {
					n.madeBy[d.Hash], n.madeIn[d.Hash], n.madeOK[d.Hash] = n.madeBy[hop.Hash], recv.Hash, receiveStatus(recv) == StatusSuccess
				}
				if types.IsEmbeddedAddress(d.ToAddress) {
					n.hops = append(n.hops, d)
				}
			}
		}
		for i, x := range n.hops {
			if x.Hash == hop.Hash {
				n.hops = append(n.hops[:i], n.hops[i+1:]...)
				break
			}
		}
		n.mu.Unlock()
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.hops), nil
}

// receiveStatus is the VM status a contract receive records in its data, or
// 0 if it records none.
func receiveStatus(b *api.AccountBlock) uint64 {
	if b == nil || len(b.Data) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(b.Data)
}

// sameBlock compares the fields that make a block an obligation: hash,
// type, sender, recipient, token, amount and data.
func sameBlock(a, b *nom.AccountBlock) bool {
	if a == nil || b == nil || a.Amount == nil || b.Amount == nil {
		return false
	}
	return a.Hash == b.Hash && a.BlockType == b.BlockType && a.Address == b.Address && a.ToAddress == b.ToAddress &&
		a.TokenStandard == b.TokenStandard && a.Amount.Cmp(b.Amount) == 0 && bytes.Equal(a.Data, b.Data)
}

// ReceivedSend reports whether the driver received the send: a receive of it
// that the node accepted.
func (n *Net) ReceivedSend(hash types.Hash) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	_, ok := n.received[hash]
	return ok
}

// contradict records a disagreement between endpoints.
func (n *Net) contradict(format string, args ...interface{}) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.contradictions = append(n.contradictions, fmt.Sprintf(format, args...))
}

// RefusedBy is the account a refused block was for.
func (n *Net) RefusedBy(hash types.Hash) types.Address {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.refusedBy[hash]
}

// Unconfirmed lists the hashes of the account's blocks that each endpoint
// and container holds but has not put in a momentum (its pool). Each node's
// pool is read in one call of the RPC's largest page, so the list is one
// snapshot rather than pages that could shift between reads: a block can
// not fall between two pages. The page must hold exactly the blocks its count
// says. A pool larger than one page cannot be read as one snapshot; stable is
// then false, with the hashes the first page returned (the caller inspects
// them before giving up), and whether the pool holds a given block is not
// established.
func (n *Net) Unconfirmed(addr types.Address) (out map[string][]types.Hash, stable bool, err error) {
	const whole = 1024 // rpc/api.RpcMaxPageSize
	out = map[string][]types.Hash{}
	stable = true
	type page struct {
		List  *[]*api.AccountBlock `json:"list"`
		Count *int                 `json:"count"`
	}
	read := func(name string, get func(p *page) error) error {
		var p page
		if err := get(&p); err != nil {
			return fmt.Errorf("%s: unconfirmed blocks of %s: %v", name, addr, err)
		}
		if p.List == nil || p.Count == nil || *p.Count < 0 {
			return fmt.Errorf("%s: unconfirmed blocks of %s: not a page", name, addr)
		}
		want := *p.Count
		if want > whole {
			want = whole
			stable = false
		}
		if len(*p.List) != want {
			return fmt.Errorf("%s: unconfirmed blocks of %s: the page holds %d blocks, its count %d leaves %d", name, addr, len(*p.List), *p.Count, want)
		}
		out[name] = []types.Hash{}
		for _, b := range *p.List {
			if b == nil {
				return fmt.Errorf("%s: unconfirmed blocks of %s: an empty entry", name, addr)
			}
			out[name] = append(out[name], b.Hash)
		}
		return nil
	}
	for i, url := range n.URLs {
		if err := read(url, func(p *page) error {
			return n.CallOn(i, p, "ledger.getUnconfirmedBlocksByAddress", addr, 0, whole)
		}); err != nil {
			return out, false, err
		}
	}
	for _, c := range n.Containers {
		if err := read(c, func(p *page) error {
			var reply struct {
				Result *page `json:"result"`
			}
			params := fmt.Sprintf(`[%q,0,%d]`, addr.String(), whole)
			if err := dockerRPCParams(context.Background(), c, "ledger.getUnconfirmedBlocksByAddress", params, &reply); err != nil {
				return err
			}
			if reply.Result == nil {
				return fmt.Errorf("no result")
			}
			*p = *reply.Result
			return nil
		}); err != nil {
			return out, false, err
		}
	}
	return out, stable, nil
}

// Refused lists the hashes of the blocks the node refused at publish.
func (n *Net) Refused() []types.Hash {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]types.Hash(nil), n.refused...)
}

// NodeFrontier is the frontier of one node: an endpoint URL or a container.
type NodeFrontier struct {
	Node    string
	Height  uint64
	Hash    types.Hash
	Version string
}

// Frontiers reads frontier and version from every endpoint and container.
// An error from any of them is an error: "every node" means every node.
func (n *Net) Frontiers() ([]NodeFrontier, error) {
	return n.FrontiersCtx(context.Background())
}

// FrontiersCtx is Frontiers with every read, the container ones included,
// bounded by ctx.
func (n *Net) FrontiersCtx(ctx context.Context) ([]NodeFrontier, error) {
	var out []NodeFrontier
	for i, url := range n.URLs {
		var m *api.Momentum
		var info struct {
			Version string `json:"version"`
		}
		if err := n.CallOnCtx(ctx, i, &m, "ledger.getFrontierMomentum"); err != nil || m == nil {
			return nil, fmt.Errorf("%s: frontier: %v", url, err)
		}
		if err := n.CallOnCtx(ctx, i, &info, "stats.processInfo"); err != nil {
			return nil, fmt.Errorf("%s: process info: %v", url, err)
		}
		out = append(out, NodeFrontier{url, m.Height, m.Hash, info.Version})
	}
	for _, c := range n.Containers {
		var m struct {
			Result *struct {
				Height uint64     `json:"height"`
				Hash   types.Hash `json:"hash"`
			} `json:"result"`
		}
		var info struct {
			Result *struct {
				Version string `json:"version"`
			} `json:"result"`
		}
		if err := dockerRPCCtx(ctx, c, "ledger.getFrontierMomentum", &m); err != nil || m.Result == nil {
			return nil, fmt.Errorf("container %s: frontier: %v", c, err)
		}
		if err := dockerRPCCtx(ctx, c, "stats.processInfo", &info); err != nil || info.Result == nil {
			return nil, fmt.Errorf("container %s: process info: %v", c, err)
		}
		out = append(out, NodeFrontier{c, m.Result.Height, m.Result.Hash, info.Result.Version})
	}
	return out, nil
}

func dockerRPC(container, method string, reply interface{}) error {
	return dockerRPCCtx(context.Background(), container, method, reply)
}

func dockerRPCCtx(ctx context.Context, container, method string, reply interface{}) error {
	return dockerRPCParams(ctx, container, method, "[]", reply)
}

func dockerRPCParams(ctx context.Context, container, method, params string, reply interface{}) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":%q,"params":%s}`, method, params)
	out, err := exec.CommandContext(ctx, "docker", "exec", container, "wget", "-qO-", "-T", "10", "--post-data", body,
		"--header", "Content-Type: application/json", "http://localhost:35997").Output()
	if err != nil {
		return err
	}
	return json.Unmarshal(out, reply)
}

func (n *Net) lock(addr types.Address) *sync.Mutex {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.locks[addr] == nil {
		n.locks[addr] = &sync.Mutex{}
	}
	return n.locks[addr]
}

type plasmaQuote struct {
	AvailablePlasma     uint64 `json:"availablePlasma"`
	BasePlasma          uint64 `json:"basePlasma"`
	RequiredDifficulty  uint64 `json:"requiredDifficulty"`
	RequiredFusedPlasma uint64 `json:"requiredFusedPlasma"`
}

type plasmaParam struct {
	Address   types.Address  `json:"address"`
	BlockType uint64         `json:"blockType"`
	ToAddress *types.Address `json:"toAddress"`
	Data      []byte         `json:"data"`
}

// Mutator changes a prepared block before it is hashed and signed (stage
// "fields"), or after signing (stage "signed"). Negative cases use it to
// build blocks that a correct client would never send.
type Mutator struct {
	Fields func(b *nom.AccountBlock) // before plasma, hash and signature
	Plasma func(b *nom.AccountBlock) // after the plasma fields are set, before hashing
	Signed func(b *nom.AccountBlock) // after signing; the hash is not recomputed
	// SkipPoW gives the block a nonce that does not satisfy its difficulty.
	SkipPoW bool
}

// Prepare fills every field of the template the way a wallet does: version,
// chain identifier, position in the account chain, acknowledged momentum,
// plasma or proof of work, hash and signature.
func (n *Net) Prepare(kp *wallet.KeyPair, tmpl *nom.AccountBlock, mut *Mutator) (*nom.AccountBlock, error) {
	b := tmpl.Copy()
	b.Version = 1
	b.ChainIdentifier = n.ChainID
	b.Address = kp.Address
	if b.Amount == nil {
		b.Amount = big.NewInt(0)
	}
	if nom.IsReceiveBlock(b.BlockType) {
		b.Amount = big.NewInt(0)
		b.TokenStandard = types.ZeroTokenStandard
	}

	var prev *api.AccountBlock
	if err := n.Call(&prev, "ledger.getFrontierAccountBlock", kp.Address); err != nil {
		return nil, fmt.Errorf("frontier account block: %w", err)
	}
	if prev != nil {
		b.PreviousHash = prev.Hash
		b.Height = prev.Height + 1
	} else {
		b.PreviousHash = types.ZeroHash
		b.Height = 1
	}

	m, err := n.Frontier()
	if err != nil {
		return nil, err
	}
	b.MomentumAcknowledged = types.HashHeight{Hash: m.Hash, Height: m.Height}

	if mut != nil && mut.Fields != nil {
		mut.Fields(b)
	}

	param := plasmaParam{Address: b.Address, BlockType: b.BlockType, Data: b.Data}
	if nom.IsSendBlock(b.BlockType) {
		to := b.ToAddress
		param.ToAddress = &to
	}
	var q plasmaQuote
	if err := n.Call(&q, "embedded.plasma.getRequiredPoWForAccountBlock", param); err != nil {
		// The node refuses to price some invalid blocks.
		// A case built with a mutator publishes it anyway, without plasma,
		// so that the refusal it checks is the one of the publish path.
		if mut == nil {
			return nil, fmt.Errorf("plasma quote: %w", err)
		}
	}
	if q.RequiredDifficulty == 0 {
		b.FusedPlasma = q.RequiredFusedPlasma
	} else {
		b.FusedPlasma = q.AvailablePlasma
		b.Difficulty = q.RequiredDifficulty
	}
	if mut != nil && mut.Plasma != nil {
		mut.Plasma(b)
	}
	if b.Difficulty != 0 && mut != nil && mut.SkipPoW {
		// A nonce that is known not to satisfy the difficulty: zero would do
		// almost always, and the driver checks instead of hoping.
		for i := byte(0); pow.CheckPoWNonce(b); i++ {
			b.Nonce.Data[0] = i + 1
		}
	}
	if b.Difficulty != 0 && (mut == nil || !mut.SkipPoW) {
		nonce := pow.GetPoWNonce(new(big.Int).SetUint64(b.Difficulty), pow.GetAccountBlockHash(b))
		copy(b.Nonce.Data[:], nonce)
	}

	b.Hash = b.ComputeHash()
	b.PublicKey = kp.Public
	b.Signature = kp.Sign(b.Hash.Bytes())
	if mut != nil && mut.Signed != nil {
		mut.Signed(b)
	}
	return b, nil
}

// Publish hands a block to the node as it is.
func (n *Net) Publish(b *nom.AccountBlock) error {
	return n.Call(nil, "ledger.publishRawTransaction", &api.AccountBlock{AccountBlock: *b})
}

// Send prepares and publishes a block. A quote can go stale when the
// frontier moves between the quote and the publish (dynamic plasma prices
// change per momentum), so a plasma refusal is retried on a fresh quote.
// Blocks with a mutator are never retried: their refusal is the result.
func (n *Net) Send(kp *wallet.KeyPair, tmpl *nom.AccountBlock, mut *Mutator) (*nom.AccountBlock, error) {
	return n.sendBy(kp, tmpl, mut, time.Time{})
}

// sendBy is Send with a deadline (none if zero): after the account lock and
// each preparation, a block is published only while the deadline has not
// passed; otherwise nothing is sent and the stage is "deadline".
func (n *Net) sendBy(kp *wallet.KeyPair, tmpl *nom.AccountBlock, mut *Mutator, deadline time.Time) (*nom.AccountBlock, error) {
	l := n.lock(kp.Address)
	l.Lock()
	defer l.Unlock()
	for attempt := 0; ; attempt++ {
		// Checked before each preparation too, so a deadline that passed
		// while the lock was held elsewhere is reported as such.
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return nil, &StageError{"deadline", fmt.Errorf("the deadline passed before a block could be prepared")}
		}
		b, err := n.Prepare(kp, tmpl, mut)
		if err != nil {
			return nil, &StageError{"prepare", err}
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return nil, &StageError{"deadline", fmt.Errorf("the deadline passed before block %s could be published", b.Hash)}
		}
		err = n.Publish(b)
		if err == nil {
			n.mu.Lock()
			n.accepted[kp.Address]++
			n.signed[kp.Address] = append(n.signed[kp.Address], b)
			if nom.IsSendBlock(b.BlockType) {
				n.owe(b)
			}
			// The first accepted receive of a send retires it; a second one
			// is not recorded here, and ReceiveAll fails on the send if the
			// node lists it as pending again.
			if nom.IsReceiveBlock(b.BlockType) {
				if _, done := n.received[b.FromBlockHash]; !done {
					n.received[b.FromBlockHash] = b.Hash
				}
			}
			n.mu.Unlock()
			return b, nil
		}
		// Only an error the node sent back is a refusal. Without one (a
		// timeout, a lost reply) the node may have accepted the block, so
		// it is neither recorded as refused nor tried again, and the stage
		// says the outcome is unknown: no case takes that as a refusal.
		var reply server.Error
		if !errors.As(err, &reply) {
			return b, &StageError{"publish-unknown", err}
		}
		// Every block the node refused is recorded, also one that is tried
		// again below: none of them may be on the chain afterwards.
		n.mu.Lock()
		n.refused = append(n.refused, b.Hash)
		n.refusedBy[b.Hash] = b.Address
		n.mu.Unlock()
		stale := strings.Contains(err.Error(), "TotalPlasma") || strings.Contains(err.Error(), "price")
		if mut != nil || !stale || attempt >= 3 {
			return b, &StageError{"publish", err}
		}
		time.Sleep(time.Second)
	}
}

// sleepCtx waits d or until ctx ends, and reports whether it waited the
// whole of d.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// BlockCtx is Block bounded by ctx as well.
func (n *Net) BlockCtx(ctx context.Context, i int, hash types.Hash) (*api.AccountBlock, error) {
	var b *api.AccountBlock
	err := n.CallOnCtx(ctx, i, &b, "ledger.getAccountBlockByHash", hash)
	if err == nil && b != nil && b.Hash != hash {
		return nil, fmt.Errorf("%s: asked for block %s, got block %s", n.URLs[i], hash, b.Hash)
	}
	return b, err
}

// Block reads one account block by hash from endpoint i. A block that comes
// back under another hash is an error, not a block: every lookup by hash
// goes through here.
func (n *Net) Block(i int, hash types.Hash) (*api.AccountBlock, error) {
	var b *api.AccountBlock
	err := n.CallOn(i, &b, "ledger.getAccountBlockByHash", hash)
	if err == nil && b != nil && b.Hash != hash {
		return nil, fmt.Errorf("%s: asked for block %s, got block %s", n.URLs[i], hash, b.Hash)
	}
	return b, err
}

// confirmedInCtx is confirmedIn bounded by ctx.
func (n *Net) confirmedInCtx(ctx context.Context, i int, b *api.AccountBlock) error {
	return n.confirmedInWith(func(result interface{}, method string, args ...interface{}) error {
		return n.CallOnCtx(ctx, i, result, method, args...)
	}, i, b)
}

// confirmedIn checks a block's confirmation: a positive momentum height and
// a non-empty momentum hash, and that momentum is the one the endpoint has
// at that height.
func (n *Net) confirmedIn(i int, b *api.AccountBlock) error {
	return n.confirmedInWith(func(result interface{}, method string, args ...interface{}) error {
		return n.CallOn(i, result, method, args...)
	}, i, b)
}

func (n *Net) confirmedInWith(call func(result interface{}, method string, args ...interface{}) error, i int, b *api.AccountBlock) error {
	c := b.ConfirmationDetail
	if c == nil || c.MomentumHeight == 0 || c.MomentumHash == (types.Hash{}) {
		return fmt.Errorf("block %s has no usable confirmation: %+v", b.Hash, c)
	}
	var page struct {
		List []*api.Momentum `json:"list"`
	}
	if err := call(&page, "ledger.getMomentumsByHeight", c.MomentumHeight, 1); err != nil {
		return readError{err}
	}
	if len(page.List) != 1 || page.List[0] == nil || page.List[0].Momentum == nil || page.List[0].Height != c.MomentumHeight || page.List[0].Hash != c.MomentumHash {
		return fmt.Errorf("block %s is confirmed in momentum %s at height %d, which %s does not have at that height", b.Hash, c.MomentumHash, c.MomentumHeight, n.URLs[i])
	}
	for _, h := range page.List[0].Content {
		if h != nil && h.Address == b.Address && h.Hash == b.Hash && h.Height == b.Height {
			return nil
		}
	}
	return fmt.Errorf("block %s (%s, height %d) is said to be confirmed in momentum %s at height %d, which does not list it", b.Hash, b.Address, b.Height, c.MomentumHash, c.MomentumHeight)
}

// readError is a read that failed, as opposed to a reply that disagrees: a
// transport error or a deadline says nothing about what the node holds.
type readError struct{ err error }

func (e readError) Error() string { return e.err.Error() }
func (e readError) Unwrap() error { return e.err }

// SameBlock is sameBlock for a case: the fields that make a block an
// obligation are equal.
func (n *Net) SameBlock(a, b *nom.AccountBlock) bool { return sameBlock(a, b) }

// ConfirmedOn is confirmedIn for a case: the block's confirmation on
// endpoint i names a momentum there that lists the block.
func (n *Net) ConfirmedOn(i int, b *api.AccountBlock) error { return n.confirmedIn(i, b) }

// WaitConfirmed blocks until the block is in a momentum on the first
// endpoint, that momentum checked, and returns it as the node reports it.
func (n *Net) WaitConfirmed(hash types.Hash, timeout time.Duration) (*api.AccountBlock, error) {
	deadline := time.Now().Add(timeout)
	for {
		b, err := n.Block(0, hash)
		if err == nil && b != nil && b.ConfirmationDetail != nil {
			if err = n.confirmedIn(0, b); err == nil {
				return b, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("block %s not confirmed in %s (last error: %v)", hash, timeout, err)
		}
		time.Sleep(time.Second)
	}
}

// Result is the outcome of a call to an embedded contract.
type Result struct {
	Send    *api.AccountBlock // the user's send block, confirmed
	Receive *api.AccountBlock // the contract's receive block, confirmed
	Status  uint64            // StatusSuccess or StatusFail
}

// OK reports whether the contract method executed without an error.
func (r *Result) OK() bool { return r.Status == StatusSuccess }

// CallContract sends data and an amount to an embedded contract and waits
// until both the send and the contract's receive are in momentums.
//
// A method can refuse in two places. A refusal while validating the send
// comes back as the error of this function, with the node's message. A
// refusal while the contract executes produces a receive block with
// StatusFail; the reason is then only in the node's log.
func (n *Net) CallContract(kp *wallet.KeyPair, to types.Address, data []byte, zts types.ZenonTokenStandard, amount *big.Int, timeout time.Duration) (*Result, error) {
	if amount == nil {
		amount = big.NewInt(0)
	}
	b, err := n.Send(kp, &nom.AccountBlock{
		BlockType:     nom.BlockTypeUserSend,
		ToAddress:     to,
		Data:          data,
		TokenStandard: zts,
		Amount:        amount,
	}, nil)
	if err != nil {
		return nil, err
	}
	return n.WaitContract(b.Hash, timeout)
}

// WaitContract waits for the contract receive that pairs with a send.
func (n *Net) WaitContract(sendHash types.Hash, timeout time.Duration) (*Result, error) {
	return n.waitAnswer(sendHash, nil, timeout)
}

// WaitAnswer is WaitContract for a send a contract made, such as a mint
// instruction to the token contract: the send must read back as the block
// recorded (from the receive that made it), not one a driver account signed.
func (n *Net) WaitAnswer(made *nom.AccountBlock, timeout time.Duration) (*Result, error) {
	return n.waitAnswer(made.Hash, made, timeout)
}

func (n *Net) waitAnswer(sendHash types.Hash, made *nom.AccountBlock, timeout time.Duration) (*Result, error) {
	deadline := time.Now().Add(timeout)
	// One deadline for every read below, the peers' and the momentum checks
	// included; nothing is accepted after it.
	wctx, wcancel := context.WithDeadline(context.Background(), deadline)
	defer wcancel()
	for {
		send, err := n.BlockCtx(wctx, 0, sendHash)
		if err == nil && send != nil && send.ConfirmationDetail != nil && send.PairedAccountBlock != nil {
			recv, err2 := n.BlockCtx(wctx, 0, send.PairedAccountBlock.Hash)
			if err2 == nil && recv != nil && recv.ConfirmationDetail != nil {
				// Both confirmations must name a momentum the node has there.
				for _, b := range []*api.AccountBlock{send, recv} {
					if err := n.confirmedInCtx(wctx, 0, b); err != nil {
						return nil, err
					}
				}
				// The receive must be the contract's own block for this send, not
				// just some confirmed block the node chose to pair with it.
				switch {
				case recv.FromBlockHash != sendHash:
					return nil, fmt.Errorf("contract receive %s is for send %s, want %s", recv.Hash, recv.FromBlockHash, sendHash)
				case recv.Address != send.ToAddress:
					return nil, fmt.Errorf("contract receive %s is on chain %s, want the called contract %s", recv.Hash, recv.Address, send.ToAddress)
				case recv.BlockType != nom.BlockTypeContractReceive:
					return nil, fmt.Errorf("contract receive %s has block type %d, want %d", recv.Hash, recv.BlockType, nom.BlockTypeContractReceive)
				case recv.ConfirmationDetail.MomentumHeight < send.ConfirmationDetail.MomentumHeight:
					return nil, fmt.Errorf("contract receive %s is confirmed before its send", recv.Hash)
				case len(recv.Data) != 8:
					return nil, fmt.Errorf("contract receive %s has %d data bytes, want 8", recv.Hash, len(recv.Data))
				}
				if st := binary.BigEndian.Uint64(recv.Data); st != StatusSuccess && st != StatusFail {
					return nil, fmt.Errorf("contract receive %s has status %d, want %d or %d", recv.Hash, st, StatusSuccess, StatusFail)
				}
				// What the node shows as the send must be the block the driver
				// signed: everything owed for it, a refund included, is worked
				// out from these fields.
				signed := made
				if signed == nil {
					signed = n.SignedBlock(sendHash)
				}
				if signed == nil {
					return nil, fmt.Errorf("send %s was not signed by this driver", sendHash)
				}
				amount := signed.Amount
				if amount == nil {
					amount = new(big.Int)
				}
				if send.Hash != sendHash || send.Address != signed.Address || send.ToAddress != signed.ToAddress || send.BlockType != signed.BlockType ||
					send.TokenStandard != signed.TokenStandard || send.Amount == nil || send.Amount.Cmp(amount) != 0 || !bytes.Equal(send.Data, signed.Data) {
					return nil, fmt.Errorf("the node shows send %s as block %s, type %d, from %s to %s, %s of %s; the driver signed type %d from %s to %s, %s of %s",
						sendHash, send.Hash, send.BlockType, send.Address, send.ToAddress, send.Amount, send.TokenStandard,
						signed.BlockType, signed.Address, signed.ToAddress, amount, signed.TokenStandard)
				}
				// How the receive ended, the same on every endpoint: only then can
				// what it made pay an announced payment.
				// The receipt must read the same on every other endpoint: the same
				// block of the same contract for the same send, the same status, the
				// same blocks made, and, where both are confirmed, the same
				// momentum. A contradiction, once seen, is an error; an endpoint
				// that does not have the receipt yet gets until 30 s from now.
				ctx, cancel := context.WithTimeout(wctx, 30*time.Second)
				for i := 1; i < len(n.URLs); i++ {
					var r *api.AccountBlock
					var err error
					// Until the peer has the receipt confirmed, or the cap passes.
					for {
						r, err = n.BlockCtx(ctx, i, recv.Hash)
						if err != nil || r != nil && r.ConfirmationDetail != nil || !sleepCtx(ctx, time.Second) {
							break
						}
					}
					if err != nil || r == nil || r.ConfirmationDetail == nil || ctx.Err() != nil {
						cancel()
						return nil, fmt.Errorf("%s does not have the contract receive %s of send %s confirmed within its time (error %v)", n.URLs[i], recv.Hash, sendHash, err)
					}
					if err := n.confirmedInCtx(ctx, i, r); err != nil {
						cancel()
						return nil, fmt.Errorf("%s: the contract receive %s of send %s: %v", n.URLs[i], recv.Hash, sendHash, err)
					}
					why := ""
					switch {
					case r.BlockType != recv.BlockType || r.Address != recv.Address || r.FromBlockHash != recv.FromBlockHash:
						why = fmt.Sprintf("type %d on %s receiving %s", r.BlockType, r.Address, r.FromBlockHash)
					case r.Height != recv.Height || r.MomentumAcknowledged != recv.MomentumAcknowledged:
						why = fmt.Sprintf("height %d acknowledging momentum %d %s", r.Height, r.MomentumAcknowledged.Height, r.MomentumAcknowledged.Hash)
					case receiveStatus(r) != receiveStatus(recv):
						why = fmt.Sprintf("status %d", receiveStatus(r))
					case len(r.DescendantBlocks) != len(recv.DescendantBlocks):
						why = fmt.Sprintf("%d blocks made", len(r.DescendantBlocks))
					case r.ConfirmationDetail.MomentumHash != recv.ConfirmationDetail.MomentumHash || r.ConfirmationDetail.MomentumHeight != recv.ConfirmationDetail.MomentumHeight:
						why = fmt.Sprintf("confirmed in momentum %s at height %d", r.ConfirmationDetail.MomentumHash, r.ConfirmationDetail.MomentumHeight)
					}
					for k := 0; why == "" && k < len(r.DescendantBlocks); k++ {
						if !sameBlock(r.DescendantBlocks[k], recv.DescendantBlocks[k]) {
							why = fmt.Sprintf("block %d made differs: %+v", k, r.DescendantBlocks[k])
						}
					}
					if why != "" {
						cancel()
						return nil, fmt.Errorf("%s shows the contract receive %s of send %s differently from %s: %s", n.URLs[i], recv.Hash, sendHash, n.URLs[0], why)
					}
					// The cap holds to the decision: a peer whose membership
					// reply came after it is not accepted.
					if ctx.Err() != nil {
						cancel()
						return nil, fmt.Errorf("%s: the contract receive %s of send %s was not established within the peer's 30 s", n.URLs[i], recv.Hash, sendHash)
					}
				}
				cancel()
				if wctx.Err() != nil {
					return nil, fmt.Errorf("contract receive for %s: the %s deadline passed while it was checked", sendHash, timeout)
				}
				ok := receiveStatus(recv) == StatusSuccess
				// What the contract sent in answer is owed to its recipients.
				n.mu.Lock()
				for _, d := range recv.DescendantBlocks {
					if d.Address == send.ToAddress && nom.IsSendBlock(d.BlockType) {
						n.owe(d)
						if n.madeBy[d.Hash] == nil {
							n.madeBy[d.Hash], n.madeIn[d.Hash], n.madeOK[d.Hash] = signed, recv.Hash, ok
						}
						if types.IsEmbeddedAddress(d.ToAddress) {
							n.hops = append(n.hops, d) // e.g. a reward: the contract asks the token contract to mint
						}
					}
				}
				n.mu.Unlock()
				return &Result{Send: send, Receive: recv, Status: binary.BigEndian.Uint64(recv.Data)}, nil
			}
		}
		if time.Now().After(deadline) || !sleepCtx(wctx, time.Second) {
			return nil, fmt.Errorf("contract receive for %s not confirmed in %s (last error: %v)", sendHash, timeout, err)
		}
	}
}

// Unreceived lists the sends waiting for the address. The answer must be a
// whole, well-formed page: count, list and more all present, as many
// entries as the count says, and nothing beyond this page.
func (n *Net) Unreceived(addr types.Address) ([]*api.AccountBlock, error) {
	var page struct {
		Count *int                 `json:"count"`
		List  *[]*api.AccountBlock `json:"list"`
		More  *bool                `json:"more"`
	}
	if err := n.Call(&page, "ledger.getUnreceivedBlocksByAddress", addr, 0, 50); err != nil {
		return nil, err
	}
	if page.Count == nil || page.List == nil || page.More == nil || *page.Count != len(*page.List) || *page.More {
		return nil, fmt.Errorf("unreceived blocks of %s: not one whole page (count %v, entries %v, more %v)", addr, deref(page.Count), entries(page.List), page.More != nil && *page.More)
	}
	for _, b := range *page.List {
		if b == nil {
			return nil, fmt.Errorf("unreceived blocks of %s: an empty entry", addr)
		}
	}
	return *page.List, nil
}

func deref(i *int) interface{} {
	if i == nil {
		return "missing"
	}
	return *i
}

func entries(l *[]*api.AccountBlock) interface{} {
	if l == nil {
		return "missing"
	}
	return len(*l)
}

// Receive publishes the receive block for one send.
func (n *Net) Receive(kp *wallet.KeyPair, from types.Hash, mut *Mutator) (*nom.AccountBlock, error) {
	return n.Send(kp, &nom.AccountBlock{BlockType: nom.BlockTypeUserReceive, FromBlockHash: from}, mut)
}

// receiveBy is Receive that publishes nothing once the deadline has passed,
// however long the account lock and the preparation took.
func (n *Net) receiveBy(kp *wallet.KeyPair, from types.Hash, deadline time.Time) (*nom.AccountBlock, error) {
	return n.sendBy(kp, &nom.AccountBlock{BlockType: nom.BlockTypeUserReceive, FromBlockHash: from}, nil, deadline)
}

// ReceiveAll receives the sends the account is owed and has not received
// yet, every one of them, and no other: a send the driver signed to it, a
// send a contract made in answer to a call the driver waited for, or a
// payment a case announced with Expect. The case says how many that is;
// more or fewer fails. Each pending send must read back, on every endpoint,
// with the content the driver knows it must have. A send is received once.
// At the end every receive must be in a momentum, be the block the driver
// signed for that send, and the mailbox must be empty.
func (n *Net) ReceiveAll(kp *wallet.KeyPair, want int, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	done := map[types.Hash]*nom.AccountBlock{} // send hash -> the receive signed for it
	// Every pending send seen that the driver did not know to be owed. Each
	// must be accounted for before the call finishes: owed and received by
	// then. One that went away unexplained fails, it does not vanish.
	strangers := map[types.Hash]bool{}
	for {
		pending, err := n.Unreceived(kp.Address)
		if err != nil {
			return len(done), err
		}
		fresh := 0
		var stranger types.Hash
		lagging := ""
		inPage := map[types.Hash]bool{}
		for _, p := range pending {
			if inPage[p.Hash] {
				return len(done), fmt.Errorf("send %s is listed twice in one page of pending sends for %s", p.Hash, kp.Address)
			}
			inPage[p.Hash] = true
		}
		for _, p := range pending {
			if r, ok := done[p.Hash]; ok {
				// The mailbox leaves out a send once the account holds a receive
				// of it, confirmed or not (LedgerApi.GetUnreceivedBlocksByAddress),
				// and the publish returned only after the node took the receive
				// in. So a send listed again after its receive was accepted is a
				// replay or a lost receive, whatever a lookup of the receive says.
				look, err := n.Block(0, r.Hash)
				return len(done), fmt.Errorf("send %s is listed as pending for %s again after the driver's receive %s of it was accepted (a lookup of that receive gave %v, error %v)", p.Hash, kp.Address, r.Hash, look != nil, err)
			}
			if p.ToAddress != kp.Address {
				return len(done), fmt.Errorf("the node lists send %s to %s as pending for %s", p.Hash, p.ToAddress, kp.Address)
			}
			n.mu.Lock()
			earlier, again := n.received[p.Hash]
			n.mu.Unlock()
			if again {
				// Listed again after a receive the node accepted: a replay, or the
				// receive was lost in a rollback. The driver cannot tell the two
				// apart safely (a receive not yet in a momentum also reads as
				// unknown by hash), and receiving again could make a double
				// receive, so both fail: either is a finding.
				return len(done), fmt.Errorf("send %s is listed as pending for %s again; the driver's receive %s of it was accepted (a replay, or the receive was lost)", p.Hash, kp.Address, earlier)
			}
			// Only a send this address is known to be owed is received. One
			// the driver has not signed or seen a contract make may be known
			// a moment later (another group's call), so it is left and
			// looked at again; if it stays, the mailbox is not empty at the
			// end and that fails.
			owed := n.Due(kp.Address, p.Hash)
			if owed == nil {
				stranger = p.Hash
				strangers[p.Hash] = true
				continue
			}
			// The pending entry, and the block under that hash on every
			// endpoint, must have the content the driver knows the send has.
			// The data counts too: a node does not recompute the hash of what
			// it answers with.
			same := func(b *api.AccountBlock) bool {
				return b.ToAddress == kp.Address && sameBlock(&b.AccountBlock, owed)
			}
			if !same(p) {
				return len(done), fmt.Errorf("pending send %s is listed as type %d from %s, %s of %s, data %x; the driver knows it as type %d from %s, %s of %s, data %x",
					p.Hash, p.BlockType, p.Address, p.Amount, p.TokenStandard, p.Data, owed.BlockType, owed.Address, owed.Amount, owed.TokenStandard, owed.Data)
			}
			// Another endpoint may not have the send yet: then it is left for
			// the next poll, and if it is still missing at the deadline, that
			// fails. The reads end at the deadline.
			missing := ""
			ctx, cancel := context.WithDeadline(context.Background(), deadline)
			for i, url := range n.URLs {
				blk, err := n.BlockCtx(ctx, i, p.Hash)
				if err != nil {
					cancel()
					return len(done), fmt.Errorf("%s: pending send %s: %v", url, p.Hash, err)
				}
				if blk == nil {
					missing = url
					break
				}
				if !same(blk) {
					cancel()
					return len(done), fmt.Errorf("%s: pending send %s reads back as type %d, from %s to %s, %s of %s, data %x", url, p.Hash, blk.BlockType, blk.Address, blk.ToAddress, blk.Amount, blk.TokenStandard, blk.Data)
				}
			}
			cancel()
			if missing != "" {
				lagging = fmt.Sprintf("%s does not have pending send %s yet", missing, p.Hash)
				continue
			}
			// No receive starts after the deadline, however long the reads
			// before it took.
			if !time.Now().Before(deadline) {
				return len(done), fmt.Errorf("the %s deadline passed before send %s to %s could be received", timeout, p.Hash, kp.Address)
			}
			// Count before publishing: a surplus send is not received at all.
			if len(done)+1 > want {
				return len(done), fmt.Errorf("more than the %d sends the case expected are owed and pending for %s; the next is %s", want, kp.Address, p.Hash)
			}
			// Reserve the send through the publish, so that two ReceiveAll
			// calls on one account cannot both receive it.
			n.mu.Lock()
			_, taken := n.received[p.Hash]
			if taken || n.reserved[p.Hash] {
				n.mu.Unlock()
				return len(done), fmt.Errorf("send %s to %s is being or has been received by another call", p.Hash, kp.Address)
			}
			n.reserved[p.Hash] = true
			n.mu.Unlock()
			b, err := n.receiveBy(kp, p.Hash, deadline)
			n.mu.Lock()
			delete(n.reserved, p.Hash)
			n.mu.Unlock()
			if err != nil {
				return len(done), fmt.Errorf("receive %s: %w", p.Hash, err)
			}
			done[p.Hash] = b
			fresh++
			// An announced payment can also come through a call the driver
			// made (its own update); then this send meets the announcement.
			n.mu.Lock()
			n.claimExpected(owed)
			n.mu.Unlock()
		}
		n.mu.Lock()
		seen := append([]string(nil), n.contradictions...)
		n.mu.Unlock()
		if len(seen) != 0 {
			return len(done), fmt.Errorf("the endpoints contradicted each other about where a payment came from: %s", strings.Join(seen, "; "))
		}
		// Follow every recorded hop first: a payment it made is owed even if
		// the mailbox does not list it. While a hop is unresolved, what it
		// will pay is not known, so the call does not finish.
		unresolved, err := n.resolveHops()
		if err != nil {
			return len(done), err
		}
		open := n.owedOpen(kp.Address)
		unmet := n.unmet(kp.Address)
		unexplained := ""
		for h := range strangers {
			if _, received := done[h]; !received {
				unexplained = h.String()
			}
		}
		if fresh == 0 && len(done) == want && len(open) == 0 && len(unmet) == 0 && unresolved == 0 && unexplained != "" {
			return len(done), fmt.Errorf("a pending send %s was listed for %s that the driver did not know to be owed, and it is not among the sends received", unexplained, kp.Address)
		}
		if fresh == 0 && len(done) == want && len(open) == 0 && len(unmet) == 0 && unresolved == 0 {
			for from, signed := range done {
				got, err := n.WaitConfirmed(signed.Hash, time.Until(deadline))
				if err != nil {
					return len(done), err
				}
				if got.Hash != signed.Hash || got.BlockType != signed.BlockType || got.FromBlockHash != from || got.Address != kp.Address {
					return len(done), fmt.Errorf("receive %s reads back as block %s, type %d, a receive of %s by %s; the driver signed type %d receiving %s by %s",
						signed.Hash, got.Hash, got.BlockType, got.FromBlockHash, got.Address, signed.BlockType, from, kp.Address)
				}
			}
			// Received means gone from the mailbox: nothing may be listed now.
			left, err := n.Unreceived(kp.Address)
			if err != nil {
				return len(done), err
			}
			if len(left) != 0 {
				return len(done), fmt.Errorf("%d sends are still listed as pending for %s after all %d receives were confirmed; the first is %s (owed according to the driver: %v)", len(left), kp.Address, want, left[0].Hash, n.Due(kp.Address, left[0].Hash) != nil)
			}
			return len(done), nil
		}
		if time.Now().After(deadline) {
			missing := ""
			if len(open) != 0 {
				missing = fmt.Sprintf("; owed and not received: %s and %d more", open[0].Hash, len(open)-1)
			}
			if len(unmet) != 0 {
				missing += fmt.Sprintf("; announced and not arrived: %s of %s from %s", unmet[0].Amount, unmet[0].Token, unmet[0].From)
			}
			if unresolved != 0 {
				missing += fmt.Sprintf("; %d contract-to-contract sends not yet received by their contract", unresolved)
			}
			if lagging != "" {
				missing += "; " + lagging
			}
			return len(done), fmt.Errorf("received %d of %d sends in %s (a pending send the driver does not know to be owed: %s%s)", len(done), want, timeout, stranger, missing)
		}
		time.Sleep(time.Second)
	}
}

// Agreement checks that every endpoint has the block in the same momentum.
func (n *Net) Agreement(hash types.Hash, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var first *api.AccountBlockConfirmationDetail
		problem := ""
		for i := range n.clients {
			b, err := n.Block(i, hash)
			if err != nil || b == nil || b.ConfirmationDetail == nil {
				problem = fmt.Sprintf("%s: block %s not confirmed (error: %v)", n.URLs[i], hash, err)
				break
			}
			if err := n.confirmedIn(i, b); err != nil {
				return err
			}
			if first == nil {
				first = b.ConfirmationDetail
			} else if first.MomentumHash != b.ConfirmationDetail.MomentumHash || first.MomentumHeight != b.ConfirmationDetail.MomentumHeight {
				return fmt.Errorf("%s confirms %s in momentum %s at height %d, %s in %s at height %d", n.URLs[0], hash, first.MomentumHash, first.MomentumHeight, n.URLs[i], b.ConfirmationDetail.MomentumHash, b.ConfirmationDetail.MomentumHeight)
			}
		}
		if problem == "" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s", problem)
		}
		time.Sleep(time.Second)
	}
}
