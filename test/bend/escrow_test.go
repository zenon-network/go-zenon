//go:build bendrules

package bend

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"strings"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/crypto"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// A call to the HTLC or the PTLC contract, in the model's terms.
type call struct {
	kind   byte // 'C' create, 'U' unlock, 'X' reclaim, 'F' a call that fails, 'T' one more momentum
	claim  bool // the unlock is PTLC's: it pays the caller
	lock   bool // the create is a stake or a fusion: the maker is the payee
	cancel bool // the reclaim is a stake cancel: the entry stays, emptied
	from   int
	payee  int
	amount uint64
	expiry int64
	id     int
	key    bool
}

func (c call) bend() string {
	switch c.kind {
	case 'C':
		payee := c.payee
		if c.lock {
			payee = c.from
		}
		return fmt.Sprintf("E.Create{%s, %s, %s, %s}", nat(uint64(c.from)), nat(uint64(payee)), nat(c.amount), nat(uint64(c.expiry)))
	case 'U':
		key := map[bool]string{true: "True{}", false: "False{}"}[c.key]
		if c.claim {
			return fmt.Sprintf("E.Claim{%s, %s, %s}", nat(uint64(c.id)), key, nat(uint64(c.from)))
		}
		return fmt.Sprintf("E.Unlock{%s, %s}", nat(uint64(c.id)), key)
	case 'F':
		return fmt.Sprintf("E.Failed{%s, %s}", nat(uint64(c.from)), nat(c.amount))
	case 'X':
		if c.cancel {
			return fmt.Sprintf("E.Cancel{%s, %s}", nat(uint64(c.from)), nat(uint64(c.id)))
		}
		return fmt.Sprintf("E.Reclaim{%s, %s}", nat(uint64(c.from)), nat(uint64(c.id)))
	}
	return "E.Tick{}"
}

func (c call) String() string {
	switch c.kind {
	case 'C':
		return fmt.Sprintf("create by %d for %d amount %d expiry %d", c.from, c.payee, c.amount, c.expiry)
	case 'U':
		return fmt.Sprintf("unlock entry %d by %d, right preimage or signature: %v", c.id, c.from, c.key)
	case 'F':
		return fmt.Sprintf("sentinel registration without the deposit by %d amount %d", c.from, c.amount)
	case 'X':
		return fmt.Sprintf("reclaim or cancel entry %d by %d", c.id, c.from)
	}
	return "momentum"
}

// htlcNode is a fresh chain with the HTLC contract switched on, or the
// PTLC contract when ptlc is set.
type htlcNode struct {
	*node
	ptlc     bool
	contract types.Address
	htlc     *embedded.HtlcApi
	ptlcApi  *embedded.PtlcApi
	ledger   *api.LedgerApi
	entries  []types.Hash // accepted creates, by the number the model gives them
	holders  []int        // PTLC: the account whose key is each entry's point
	pending  int          // PTLC: the holder of the create being made
}

func preimage(id int) []byte {
	return crypto.Hash([]byte(fmt.Sprintf("bend escrow rule preimage %d", id)))
}

func newHtlcNode(t *testing.T, ptlc bool) *htlcNode {
	// Activating a spork on the mock chain writes to package variables of
	// common/types; put them back when the test ends.
	savedId, savedPtlcId := types.HtlcSpork.SporkId, types.PtlcSpork.SporkId
	savedMap := make(map[types.Hash]bool, len(types.ImplementedSporksMap))
	for k, v := range types.ImplementedSporksMap {
		savedMap[k] = v
	}
	t.Cleanup(func() {
		types.HtlcSpork.SporkId = savedId
		types.PtlcSpork.SporkId = savedPtlcId
		types.ImplementedSporksMap = savedMap
	})
	name := map[bool]string{false: "spork-htlc", true: "spork-ptlc"}[ptlc]

	n := newNode(t)
	z := n.z
	// a setup that fails ends the test from inside this function; stop the
	// node then, since the caller never gets it
	ready := false
	defer func() {
		if !ready {
			z.StopPanic()
		}
	}()
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data:      definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName, name, "activate "+name),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	sporks, err := embedded.NewSporkApi(z).GetAll(0, 10)
	if err != nil || len(sporks.List) == 0 {
		t.Fatalf("no spork after creating one: %v", err)
	}
	id := sporks.List[0].Id
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data:      definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName, id),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	contract := types.HtlcContract
	if ptlc {
		types.PtlcSpork.SporkId = id
		contract = types.PtlcContract
	} else {
		types.HtlcSpork.SporkId = id
	}
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(20)
	ready = true
	return &htlcNode{node: n, ptlc: ptlc, contract: contract, htlc: embedded.NewHtlcApi(z), ptlcApi: embedded.NewPtlcApi(z), ledger: api.NewLedgerApi(z)}
}

func (n *htlcNode) now() int64 {
	m, err := n.z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	if err != nil {
		n.t.Fatal(err)
	}
	return m.Timestamp.Unix()
}

func (n *htlcNode) exists(id int) bool {
	if id >= len(n.entries) {
		return false
	}
	return n.held(n.entries[id])
}

// held says whether the contract holds an entry under this hash.
func (n *htlcNode) held(hash types.Hash) bool {
	var err error
	if n.ptlc {
		_, err = n.ptlcApi.GetById(hash)
	} else {
		_, err = n.htlc.GetById(hash)
	}
	if err != nil && err != constants.ErrDataNonExistent {
		n.t.Fatal(err)
	}
	return err == nil
}

// maker is the account that made entry id, or -1.
func (n *htlcNode) maker(id int) int {
	if id >= len(n.entries) {
		return -1
	}
	var who types.Address
	if n.ptlc {
		info, err := n.ptlcApi.GetById(n.entries[id])
		if err != nil {
			return -1
		}
		who = info.TimeLocked
	} else {
		info, err := n.htlc.GetById(n.entries[id])
		if err != nil {
			return -1
		}
		who = info.TimeLocked
	}
	for i, kp := range users {
		if kp.Address == who {
			return i
		}
	}
	return -1
}

func (n *htlcNode) entryHash(id int) types.Hash {
	if id < len(n.entries) {
		return n.entries[id]
	}
	return types.NewHash([]byte(fmt.Sprintf("no entry %d", id)))
}

// send publishes one call to the contract. The node generates the block
// from a template here, as a wallet would ask it to; a refusal at this
// point is the VM's (the balance does not cover the amount).
func (n *htlcNode) send(kp *wallet.KeyPair, data []byte, amount uint64) (*nom.AccountBlock, error) {
	return n.sendTo(kp, n.contract, data, amount)
}

func (n *htlcNode) sendTo(kp *wallet.KeyPair, to types.Address, data []byte, amount uint64) (*nom.AccountBlock, error) {
	tx, err := n.sup.GenerateFromTemplate(&nom.AccountBlock{
		BlockType:     nom.BlockTypeUserSend,
		Address:       kp.Address,
		ToAddress:     to,
		TokenStandard: znn,
		Amount:        new(big.Int).SetUint64(amount),
		Data:          data,
	}, kp.Signer)
	if err != nil {
		return nil, err
	}
	insert := n.z.Chain().AcquireInsert("bend escrow rule")
	defer insert.Unlock()
	return tx.Block, n.z.Chain().AddAccountBlockTransaction(insert, tx)
}

// settle confirms the call, lets the contract answer it, and has every
// account receive what was sent to it: a payout, or the refund of a create
// the contract refused.
func (n *htlcNode) settle() [][]*api.AccountBlock {
	n.z.InsertNewMomentum()
	n.z.InsertNewMomentum()
	got := make([][]*api.AccountBlock, len(users))
	for i, kp := range users {
		unreceived, err := n.ledger.GetUnreceivedBlocksByAddress(kp.Address, 0, 50)
		if err != nil {
			n.t.Fatal(err)
		}
		for _, b := range unreceived.List {
			n.z.InsertReceiveBlock(b.AccountBlock.Header(), nil, nil, mock.SkipVmChanges)
			got[i] = append(got[i], b)
		}
	}
	return got
}

// fail makes a call that carries an amount and that the contract refuses:
// a sentinel registration by an account that has deposited no QSR. It is
// accepted, in the model's sense, when the amount came back in exactly one
// block. The contract must hold what it held before, and the refund must
// not be receivable a second time.
func (n *htlcNode) fail(c call) (accepted, settled bool) {
	held := func() *big.Int {
		amount, err := n.z.Chain().GetFrontierAccountStore(types.SentinelContract).GetBalance(znn)
		if err != nil {
			n.t.Fatal(err)
		}
		return amount
	}
	before := held()
	b, err := n.sendTo(users[c.from], types.SentinelContract, definition.ABISentinel.PackMethodPanic(definition.RegisterSentinelMethodName), c.amount)
	if err != nil {
		if !errors.Is(err, constants.ErrInsufficientBalance) {
			n.t.Errorf("%v: refused for a reason outside the rule: %v", c, err)
		}
		return false, false
	}
	all := n.settle()
	if executed(n.t, n.ledger, c, b.Hash) {
		n.t.Errorf("%v: the contract accepted a registration without the deposit", c)
		return false, true
	}
	got := all[c.from]
	for i, blocks := range all {
		if i != c.from && len(blocks) != 0 {
			n.t.Errorf("%v: %d block(s) went to account %d, which made no call", c, len(blocks), i)
		}
	}
	if after := held(); after.Cmp(before) != 0 {
		n.t.Errorf("%v: the contract held %v before and %v after", c, before, after)
	}
	if len(got) != 1 || got[0].Amount.Uint64() != c.amount {
		n.t.Errorf("%v: %d block(s) came back, want one carrying the amount", c, len(got))
		return false, true
	}
	// the refund, received above, is asked for again
	n.sends = []types.Hash{got[0].Hash}
	if err := n.submit(n.block(op{kind: 'R', acc: c.from, id: 0, height: n.height(c.from) + 1})); !errors.Is(err, verifier.ErrABFromBlockAlreadyReceived) {
		n.t.Errorf("%v: receiving the refund a second time: %v", c, err)
	}
	return true, true
}

// do makes one call on the node. accepted is the status the contract wrote
// into its receive block for the call. What the contract holds afterwards is
// checked against that status separately, so that a call the contract
// executed without effect, or refused with one, fails the test instead of
// passing as one or the other. settled is false when the VM refused the
// block before any momentum was made.
func (n *htlcNode) do(c call) (accepted, settled bool) {
	switch c.kind {
	case 'T':
		n.z.InsertNewMomentum()
		return true, true
	case 'F':
		return n.fail(c)
	case 'C':
		var data []byte
		if n.ptlc {
			// the point is the payee's public key, and no destination is fixed:
			// whoever shows the payee's signature over their own address is paid
			data = definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
				c.expiry, definition.PointTypeED25519, []byte(users[c.payee].Public), types.ZeroAddress)
		} else {
			data = definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
				users[c.payee].Address, c.expiry, uint8(definition.HashTypeSHA3), uint8(32), crypto.Hash(preimage(len(n.entries))))
		}
		b, err := n.send(users[c.from], data, c.amount)
		if err != nil {
			if !errors.Is(err, constants.ErrInsufficientBalance) && !errors.Is(err, constants.ErrInvalidTokenOrAmount) {
				n.t.Errorf("%v: refused for a reason outside the rule: %v", c, err)
			}
			return false, false
		}
		n.settle()
		ok := executed(n.t, n.ledger, c, b.Hash)
		if held := n.held(b.Hash); held != ok {
			n.t.Errorf("%v: the contract's status says accepted=%v, but it holds the entry: %v", c, ok, held)
		}
		if !ok {
			return false, true
		}
		n.entries = append(n.entries, b.Hash)
		n.holders = append(n.holders, c.payee)
		return true, true
	}
	before := n.exists(c.id)
	var data []byte
	if c.kind == 'U' && n.ptlc {
		// a signature of the entry's point over this entry and the caller,
		// or, for the wrong one, over a message that is one bit off
		holder := users[0]
		if c.id < len(n.holders) {
			holder = users[n.holders[c.id]]
		}
		msg := definition.GetPtlcUnlockMessage(n.z.Chain().ChainIdentifier(), definition.PointTypeED25519, n.entryHash(c.id), users[c.from].Address)
		if !c.key {
			msg[0] ^= 1
		}
		data = definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, n.entryHash(c.id), holder.Sign(msg))
	} else if c.kind == 'U' {
		key := preimage(c.id)
		if !c.key {
			key = preimage(c.id + 1000)
		}
		data = definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName, n.entryHash(c.id), key)
	} else if n.ptlc {
		data = definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, n.entryHash(c.id))
	} else {
		data = definition.ABIHtlc.PackMethodPanic(definition.ReclaimHtlcMethodName, n.entryHash(c.id))
	}
	b, err := n.send(users[c.from], data, 0)
	if err != nil {
		n.t.Errorf("%v: refused for a reason outside the rule: %v", c, err)
		return false, false
	}
	n.settle()
	ok := executed(n.t, n.ledger, c, b.Hash)
	// accepted: the entry was there and is gone; refused: it is as it was
	if after := n.exists(c.id); ok && !(before && !after) || !ok && after != before {
		n.t.Errorf("%v: the contract's status says accepted=%v, but the entry was held before: %v, after: %v", c, ok, before, after)
	}
	return ok, true
}

type escrowResult struct {
	name     string
	bals     []uint64
	now      int64
	calls    []call // as given to the model, momentums included
	asked    []call // as made on the node
	nodeLine string
}

// A scenario step sees the node as the steps before it left it.
type escrowStep func(n *htlcNode) call

func runEscrow(t *testing.T, ptlc bool, name string, steps []escrowStep) escrowResult {
	n := newHtlcNode(t, ptlc)
	defer n.z.StopPanic()
	r := escrowResult{name: name, bals: n.balances(), now: n.now()}
	var marks strings.Builder
	for _, s := range steps {
		c := s(n)
		c.claim = ptlc && c.kind == 'U'
		ok, settled := n.do(c)
		r.asked = append(r.asked, c)
		switch {
		case c.kind == 'T':
			r.calls = append(r.calls, c)
			continue
		case settled:
			// The contract judges a call at the momentum that confirms it,
			// and settling takes one more: in the model's terms, a momentum
			// on each side of the call.
			r.calls = append(r.calls, call{kind: 'T'}, c, call{kind: 'T'})
		default:
			r.calls = append(r.calls, c)
		}
		marks.WriteByte(map[bool]byte{true: 'A', false: 'R'}[ok])
	}
	for _, b := range n.balances() {
		fmt.Fprintf(&marks, " %d", b)
	}
	left := 0
	for id := range n.entries {
		if n.exists(id) {
			left++
		}
	}
	fmt.Fprintf(&marks, " entries %d", left)
	r.nodeLine = marks.String()
	return r
}

// Steps. An expiry is given in seconds from the time of the step. A call
// made at time t is judged at t+10 and leaves the time at t+20.

func create(from, payee int, amount func(bal uint64) uint64, in int64) escrowStep {
	return func(n *htlcNode) call {
		return call{kind: 'C', from: from, payee: payee, amount: amount(n.balance(from)), expiry: n.now() + in}
	}
}

func unlock(by, id int, key bool) escrowStep {
	return func(*htlcNode) call { return call{kind: 'U', from: by, id: id, key: key} }
}

func reclaim(by, id int) escrowStep {
	return func(*htlcNode) call { return call{kind: 'X', from: by, id: id} }
}

// refused is a call that takes the sentinel registration amount and fails.
func refused(by int) escrowStep {
	return func(*htlcNode) call {
		return call{kind: 'F', from: by, amount: constants.SentinelZnnRegisterAmount.Uint64()}
	}
}

func wait() escrowStep { return func(*htlcNode) call { return call{kind: 'T'} } }

// The written scenarios: each way of asking for one entry twice, and the
// two edges of the expiry.
func escrowScripted() ([]string, map[string][]escrowStep) {
	names := []string{"unlock-twice", "unlock-then-reclaim", "reclaim-twice", "reclaim-then-unlock", "wrong-preimage", "expiry-edge-unlock", "expiry-edge-reclaim", "create-expired", "create-overspend", "two-entries", "failed-call"}
	return names, map[string][]escrowStep{
		"unlock-twice":        {create(0, 1, exactly(500), 100), unlock(1, 0, true), unlock(1, 0, true), unlock(2, 0, true), reclaim(0, 0)},
		"unlock-then-reclaim": {create(0, 1, exactly(500), 60), unlock(2, 0, true), wait(), wait(), wait(), reclaim(0, 0), reclaim(1, 0)},
		"reclaim-twice":       {create(0, 1, exactly(500), 30), reclaim(2, 0), reclaim(1, 0), reclaim(0, 0), reclaim(0, 0), reclaim(2, 0)},
		"reclaim-then-unlock": {create(0, 1, exactly(500), 30), wait(), reclaim(0, 0), unlock(1, 0, true), unlock(0, 0, true)},
		"wrong-preimage":      {create(0, 1, exactly(500), 100), unlock(1, 0, false), unlock(0, 0, false), unlock(1, 3, true), unlock(1, 0, true), unlock(1, 0, false)},
		// judged at 30 with expiry 40: the last moment an unlock is accepted
		"expiry-edge-unlock": {create(0, 1, exactly(500), 40), unlock(1, 0, true), reclaim(0, 0)},
		// judged at 30 with expiry 30: the unlock is late and the reclaim is due
		"expiry-edge-reclaim": {create(0, 1, exactly(500), 30), unlock(1, 0, true), reclaim(0, 0), create(0, 1, exactly(700), 40), reclaim(0, 1), reclaim(0, 1)},
		// an entry that would be expired when it is made is refused and the amount comes back
		"create-expired":   {create(0, 1, exactly(500), 10), create(0, 1, exactly(500), 0), create(0, 1, exactly(500), 20), unlock(1, 0, true)},
		"create-overspend": {create(0, 1, allPlus(1), 100), create(0, 1, exactly(0), 100), create(0, 1, all, 100), create(0, 1, exactly(1), 100), create(0, 1, all, 100), unlock(3, 0, true), unlock(3, 0, true)},
		// a call that fails gives the amount back, each time, and not to an
		// account that could not pay it; around it the entries go on as before
		"failed-call": {refused(0), refused(0), refused(2), create(0, 1, exactly(500), 100), refused(1), refused(0), unlock(1, 0, true), create(0, 1, all, 100), refused(0), refused(3)},
		"two-entries": {create(0, 1, exactly(500), 200), create(2, 3, exactly(900), 60), unlock(0, 1, true), unlock(0, 1, true), reclaim(0, 0), unlock(0, 0, true), reclaim(2, 1), wait(), reclaim(0, 0)},
	}
}

func escrowRandom(seed int64, length int) []escrowStep {
	rng := rand.New(rand.NewSource(seed))
	steps := make([]escrowStep, 0, length)
	for i := 0; i < length; i++ {
		by, other, id := rng.Intn(len(users)), rng.Intn(len(users)), rng.Intn(5)
		pick, in, right := rng.Intn(4), int64(10*rng.Intn(10)), rng.Intn(10) < 7
		if rng.Intn(2) == 0 {
			in = int64(10 * (10 + rng.Intn(50))) // long enough to be unlocked
		}
		if rng.Intn(7) == 0 {
			steps = append(steps, refused(by))
			continue
		}
		switch rng.Intn(8) {
		case 0:
			steps = append(steps, wait())
		case 1, 2, 3:
			amount := []func(uint64) uint64{exactly(uint64(1 + rng.Intn(100000))), exactly(uint64(1 + rng.Intn(100000))), all, allPlus(1)}[pick]
			steps = append(steps, create(by, other, amount, in))
		case 4, 5:
			steps = append(steps, unlock(by, id, right))
		default:
			steps = append(steps, func(n *htlcNode) call {
				// mostly the account that made the entry, sometimes another
				from := by
				if m := n.maker(id); m >= 0 && pick != 0 {
					from = m
				}
				return call{kind: 'X', from: from, id: id}
			})
		}
	}
	return steps
}

// TestEscrowModelMatchesHtlc is case BE-1.
func TestEscrowModelMatchesHtlc(t *testing.T) {
	compareEscrow(t, false, "BE-1")
}

// TestEscrowModelMatchesPtlc is case BE-2: the same scenarios on the PTLC
// contract, where an unlock is a signature of the entry's point and pays
// the caller it names.
func TestEscrowModelMatchesPtlc(t *testing.T) {
	compareEscrow(t, true, "BE-2")
}

func compareEscrow(t *testing.T, ptlc bool, id string) {
	var results []escrowResult
	names, scripted := escrowScripted()
	for _, name := range names {
		results = append(results, runEscrow(t, ptlc, name, scripted[name]))
	}
	seed, count := envCount(t, "BEND_SEED", 1, 0), int(envCount(t, "BEND_RANDOM", 40, 0))
	for i := 0; i < count; i++ {
		results = append(results, runEscrow(t, ptlc, fmt.Sprintf("random-%d", seed+int64(i)), escrowRandom(seed+int64(i), 14)))
	}

	var src bytes.Buffer
	src.WriteString("import Base\nimport ./escrow/escrow.bend as E\nimport ./escrow/trace.bend as T\n\ndef main() -> IO(Unit):\n  do IO<Unit>:\n")
	for _, r := range results {
		calls := make([]string, len(r.calls))
		for i, c := range r.calls {
			calls[i] = c.bend()
		}
		fmt.Fprintf(&src, "    IO.print(T.Trace.run([%s], E.Vault{%s, [], %s, 0n}))\n", strings.Join(calls, ", "), natList(r.bals), nat(uint64(r.now)))
	}
	lines := runBend(t, "escrow-"+id, src.String(), len(results))

	counts := map[string]int{}
	for i, r := range results {
		verdict := "same"
		if lines[i] != r.nodeLine {
			verdict = "DIFFERENT"
			t.Errorf("%s: the node and the model disagree\n  node:  %s\n  model: %s", r.name, r.nodeLine, lines[i])
			for j, c := range r.asked {
				t.Logf("  %2d %v", j, c)
			}
		}
		t.Logf("%-22s %s  node %s", r.name, verdict, r.nodeLine)
		marks := strings.SplitN(r.nodeLine, " ", 2)[0]
		j := 0
		for _, c := range r.asked {
			if c.kind == 'T' {
				continue
			}
			counts[string(c.kind)+string(marks[j])]++
			j++
		}
	}
	t.Logf("%d scenarios", len(results))
	t.Logf("  create  accepted %4d  refused %4d", counts["CA"], counts["CR"])
	t.Logf("  unlock  accepted %4d  refused %4d", counts["UA"], counts["UR"])
	t.Logf("  reclaim accepted %4d  refused %4d", counts["XA"], counts["XR"])
	t.Logf("  failed calls refunded %4d  not made %4d", counts["FA"], counts["FR"])
}
