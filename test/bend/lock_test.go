//go:build bendrules

package bend

import (
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// The locks are shortened for the test, as the unit tests of the node do:
// a stake lasts a multiple of ten seconds (one momentum), a fusion can be
// cancelled four momentums after it was made.
const (
	stakeUnitSec   = 10
	fuseMomentums  = 4
	unit           = 100000000 // one ZNN or QSR
	stakeMinUnits  = 1
	fusionMinUnits = 10
)

// lockNode is a fresh chain driven against the Stake contract (ZNN), or
// against the fusions of the Plasma contract (QSR) when stake is false.
type lockNode struct {
	*node
	stake    bool
	contract types.Address
	ledger   *api.LedgerApi
	entries  []types.Hash // accepted locks, by the number the model gives them
	owners   []int
}

func newLockNode(t *testing.T, stake bool) *lockNode {
	unitSec, minSec, maxSec, fuse := constants.StakeTimeUnitSec, constants.StakeTimeMinSec, constants.StakeTimeMaxSec, constants.FuseExpiration
	t.Cleanup(func() {
		constants.StakeTimeUnitSec, constants.StakeTimeMinSec, constants.StakeTimeMaxSec, constants.FuseExpiration = unitSec, minSec, maxSec, fuse
	})
	constants.StakeTimeUnitSec, constants.StakeTimeMinSec, constants.StakeTimeMaxSec = stakeUnitSec, stakeUnitSec, 12*stakeUnitSec
	constants.FuseExpiration = fuseMomentums

	n := &lockNode{node: newNode(t), stake: stake, contract: types.StakeContract}
	if !stake {
		n.contract = types.PlasmaContract
		n.token = types.QsrTokenStandard
	}
	n.ledger = api.NewLedgerApi(n.z)
	return n
}

func (n *lockNode) now() int64 {
	m, err := n.z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	if err != nil {
		n.t.Fatal(err)
	}
	return m.Timestamp.Unix()
}

// held says whether the contract has an entry under this hash for owner.
func (n *lockNode) held(hash types.Hash, owner int) bool {
	_, context, err := api.GetFrontierContext(n.z.Chain(), n.contract)
	if err != nil {
		n.t.Fatal(err)
	}
	if n.stake {
		_, err = definition.GetStakeInfo(context.Storage(), hash, users[owner].Address)
	} else {
		_, err = definition.GetFusionInfo(context.Storage(), users[owner].Address, hash)
	}
	if err != nil && err != constants.ErrDataNonExistent {
		n.t.Fatal(err)
	}
	return err == nil
}

func (n *lockNode) send(kp *wallet.KeyPair, data []byte, amount uint64) (*nom.AccountBlock, error) {
	tx, err := n.sup.GenerateFromTemplate(&nom.AccountBlock{
		BlockType:     nom.BlockTypeUserSend,
		Address:       kp.Address,
		ToAddress:     n.contract,
		TokenStandard: n.token,
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
// account receive what was sent to it. It returns how many blocks each
// account received and their sum.
func (n *lockNode) settle() (blocks []int, paid []uint64) {
	n.z.InsertNewMomentum()
	n.z.InsertNewMomentum()
	blocks, paid = make([]int, len(users)), make([]uint64, len(users))
	for i, kp := range users {
		unreceived, err := n.ledger.GetUnreceivedBlocksByAddress(kp.Address, 0, 50)
		if err != nil {
			n.t.Fatal(err)
		}
		for _, b := range unreceived.List {
			n.z.InsertReceiveBlock(b.AccountBlock.Header(), nil, nil, mock.SkipVmChanges)
			blocks[i]++
			paid[i] += b.Amount.Uint64()
		}
	}
	return blocks, paid
}

// do makes one call on the node. A lock is accepted when the contract has
// the entry afterwards. A cancel is accepted when the contract answered it
// with a block to the caller, whatever amount that block carries.
func (n *lockNode) do(c call) (accepted, settled bool, note string) {
	switch c.kind {
	case 'T':
		n.z.InsertNewMomentum()
		return true, true, ""
	case 'C':
		var data []byte
		if n.stake {
			data = definition.ABIStake.PackMethodPanic(definition.StakeMethodName, c.expiry)
		} else {
			data = definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, users[c.payee].Address)
		}
		b, err := n.send(users[c.from], data, c.amount)
		if err != nil {
			if !errors.Is(err, constants.ErrInsufficientBalance) {
				n.t.Errorf("%v: refused for a reason outside the rule: %v", c, err)
			}
			return false, false, err.Error()
		}
		n.settle()
		if !n.held(b.Hash, c.from) {
			return false, true, "no entry"
		}
		n.entries = append(n.entries, b.Hash)
		n.owners = append(n.owners, c.from)
		return true, true, ""
	}
	hash := types.NewHash([]byte(fmt.Sprintf("no entry %d", c.id)))
	if c.id < len(n.entries) {
		hash = n.entries[c.id]
	}
	var data []byte
	if n.stake {
		data = definition.ABIStake.PackMethodPanic(definition.CancelStakeMethodName, hash)
	} else {
		data = definition.ABIPlasma.PackMethodPanic(definition.CancelFuseMethodName, hash)
	}
	if _, err := n.send(users[c.from], data, 0); err != nil {
		n.t.Errorf("%v: refused for a reason outside the rule: %v", c, err)
		return false, false, err.Error()
	}
	blocks, paid := n.settle()
	return blocks[c.from] > 0, true, fmt.Sprintf("answered with %d block(s) carrying %d", blocks[c.from], paid[c.from])
}

type lockStep func(n *lockNode) call

// lock makes an entry. units gets the maker's balance in whole coins and
// returns the amount in whole coins; periods is the length of a stake in
// units of ten seconds (a fusion has one fixed length).
func lock(from int, units func(bal uint64) uint64, periods int64) lockStep {
	return func(n *lockNode) call {
		c := call{kind: 'C', lock: true, from: from, payee: from, amount: units(n.balance(from)/unit) * unit}
		// The entry is made at the momentum that confirms the call, ten
		// seconds from now, and its lock runs from there.
		if n.stake {
			c.expiry = periods * stakeUnitSec // the node is given the length
		} else {
			c.expiry = n.now() + 10 + 10*fuseMomentums
		}
		return c
	}
}

func cancel(by, id int) lockStep {
	return func(*lockNode) call { return call{kind: 'X', from: by, id: id} }
}

func pause() lockStep { return func(*lockNode) call { return call{kind: 'T'} } }

func lockScripted() ([]string, map[string][]lockStep) {
	names := []string{"cancel-twice", "cancel-early", "cancel-by-another", "cancel-unknown", "two-locks", "lock-overspend"}
	return names, map[string][]lockStep{
		"cancel-twice":      {lock(0, exactly(50), 1), pause(), pause(), pause(), cancel(0, 0), cancel(0, 0), cancel(0, 0)},
		"cancel-early":      {lock(0, exactly(50), 6), cancel(0, 0), cancel(0, 0), pause(), pause(), pause(), cancel(0, 0), cancel(0, 0)},
		"cancel-by-another": {lock(0, exactly(50), 1), pause(), pause(), pause(), cancel(1, 0), cancel(2, 0), cancel(0, 0), cancel(1, 0)},
		"cancel-unknown":    {cancel(0, 0), lock(1, exactly(20), 1), cancel(1, 3), pause(), pause(), cancel(1, 0)},
		"two-locks":         {lock(0, exactly(50), 1), lock(1, exactly(70), 2), lock(0, exactly(30), 1), cancel(0, 0), cancel(1, 1), cancel(0, 2), cancel(0, 0), cancel(0, 2), cancel(1, 0), cancel(1, 1)},
		"lock-overspend":    {lock(0, allPlus(1), 1), lock(0, all, 1), lock(0, exactly(10), 1), pause(), pause(), cancel(0, 0), lock(0, all, 1)},
	}
}

func lockRandom(seed int64, length int) []lockStep {
	rng := rand.New(rand.NewSource(seed))
	steps := make([]lockStep, 0, length)
	for i := 0; i < length; i++ {
		by, id, periods := rng.Intn(len(users)), rng.Intn(4), int64(1+rng.Intn(6))
		amount := []func(uint64) uint64{exactly(uint64(10 + rng.Intn(90))), exactly(uint64(10 + rng.Intn(90))), allPlus(1)}[rng.Intn(3)]
		owner := rng.Intn(4) != 0
		switch rng.Intn(8) {
		case 0, 1:
			steps = append(steps, pause())
		case 2, 3, 4:
			steps = append(steps, lock(by, amount, periods))
		default:
			steps = append(steps, func(n *lockNode) call {
				// mostly the account that made the entry, sometimes another
				from := by
				if owner && id < len(n.owners) {
					from = n.owners[id]
				}
				return call{kind: 'X', from: from, id: id}
			})
		}
	}
	return steps
}

type lockResult struct {
	name     string
	bals     []uint64
	now      int64
	calls    []call
	asked    []call
	notes    []string
	nodeLine string
}

func runLock(t *testing.T, stake bool, name string, steps []lockStep) lockResult {
	n := newLockNode(t, stake)
	defer n.z.StopPanic()
	r := lockResult{name: name, bals: n.balances(), now: n.now()}
	var marks strings.Builder
	for _, s := range steps {
		c := s(n)
		c.cancel = stake && c.kind == 'X'
		before := n.now()
		ok, settled, note := n.do(c)
		r.asked, r.notes = append(r.asked, c), append(r.notes, note)
		if c.kind == 'C' && stake {
			c.expiry += before + 10 // the model is given the moment it ends
		}
		switch {
		case c.kind == 'T':
			r.calls = append(r.calls, c)
			continue
		case settled:
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
	for id, hash := range n.entries {
		if n.held(hash, n.owners[id]) {
			left++
		}
	}
	fmt.Fprintf(&marks, " entries %d", left)
	r.nodeLine = marks.String()
	return r
}

// TestEscrowModelMatchesStake is case BE-3: the lock of the Stake contract.
func TestEscrowModelMatchesStake(t *testing.T) {
	compareLock(t, true, "BE-3")
}

// TestEscrowModelMatchesFusion is case BE-4: the fusions of the Plasma
// contract.
func TestEscrowModelMatchesFusion(t *testing.T) {
	compareLock(t, false, "BE-4")
}

func compareLock(t *testing.T, stake bool, id string) {
	var results []lockResult
	names, scripted := lockScripted()
	for _, name := range names {
		results = append(results, runLock(t, stake, name, scripted[name]))
	}
	seed, count := envCount(t, "BEND_SEED", 1, 0), int(envCount(t, "BEND_RANDOM", 40, 0))
	for i := 0; i < count; i++ {
		results = append(results, runLock(t, stake, fmt.Sprintf("random-%d", seed+int64(i)), lockRandom(seed+int64(i), 14)))
	}

	var src strings.Builder
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
	zero := 0
	for i, r := range results {
		verdict := "same"
		if lines[i] != r.nodeLine {
			verdict = "DIFFERENT"
			t.Errorf("%s: the node and the model disagree\n  node:  %s\n  model: %s", r.name, r.nodeLine, lines[i])
			for j, c := range r.asked {
				t.Logf("  %2d %v  %s", j, c, r.notes[j])
			}
		}
		t.Logf("%-22s %s  node %s", r.name, verdict, r.nodeLine)
		marks := strings.SplitN(r.nodeLine, " ", 2)[0]
		j := 0
		for k, c := range r.asked {
			if c.kind == 'T' {
				continue
			}
			counts[string(c.kind)+string(marks[j])]++
			if c.kind == 'X' && marks[j] == 'A' && strings.HasSuffix(r.notes[k], "carrying 0") {
				zero++
			}
			j++
		}
	}
	t.Logf("%d scenarios", len(results))
	t.Logf("  lock    accepted %4d  refused %4d", counts["CA"], counts["CR"])
	t.Logf("  cancel  accepted %4d  refused %4d", counts["XA"], counts["XR"])
	t.Logf("  cancels accepted that paid zero: %d", zero)
}
