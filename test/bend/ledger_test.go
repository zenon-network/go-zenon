//go:build bendrules

// Package bend compares the Bend model of test/bend/ledger with the node.
//
// The laws in ledger/LAWS.bend are proven about the model, not about znnd.
// This test is the link between the two: it gives the same sequences of
// blocks to the model and to the real chain, verifier and VM (in one
// process, through zenon/mock), and fails when they disagree on which
// blocks are accepted or on the balances at the end.
//
// It sits behind the bendrules build tag, so `go test ./...` never starts
// it. See docs/BEND_LAWS.md.
package bend

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// The four accounts of the model, in model order.
var users = []*wallet.KeyPair{g.User1, g.User2, g.User3, g.User4}

var znn = types.ZnnTokenStandard

// The reasons the node may give for refusing a block of a scenario. Any
// other reason means the scenario tested something else than intended, and
// fails the test.
var refusals = []error{
	constants.ErrInsufficientBalance,
	verifier.ErrABFromBlockMissing,
	verifier.ErrABFromBlockAlreadyReceived,
	verifier.ErrABFromBlockReceiverMismatch,
	verifier.ErrABPreviousMissing,
	verifier.ErrABPrevHasCementedOnTop,
	verifier.ErrABPrevHeightExists,
}

type op struct {
	kind   byte // 'S' send, 'R' receive, 'T' momentum
	acc    int
	to     int
	amount uint64
	height uint64
	id     int
}

func (o op) bend() string {
	switch o.kind {
	case 'S':
		return fmt.Sprintf("L.Snd{%s, %s, %s, %s}", nat(uint64(o.acc)), nat(o.height), nat(uint64(o.to)), nat(o.amount))
	case 'R':
		return fmt.Sprintf("L.Rcv{%s, %s, %s}", nat(uint64(o.acc)), nat(o.height), nat(uint64(o.id)))
	}
	return "L.Tick{}"
}

func (o op) String() string {
	switch o.kind {
	case 'S':
		return fmt.Sprintf("send account %d height %d to %d amount %d", o.acc, o.height, o.to, o.amount)
	case 'R':
		return fmt.Sprintf("receive account %d height %d send %d", o.acc, o.height, o.id)
	}
	return "momentum"
}

// nat writes a number as a Bend Nat. Bend's Nat literals stop at 2^32-1.
func nat(v uint64) string {
	if v < 1<<32 {
		return strconv.FormatUint(v, 10) + "n"
	}
	return fmt.Sprintf("Nat.add(Nat.mul(%dn, 1000000000n), %dn)", v/1_000_000_000, v%1_000_000_000)
}

func natList(vs []uint64) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = nat(v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// envCount reads a whole number of at least min from the environment, or
// def when the variable is not set. A value that is not such a number
// fails the test: a mistyped count must not turn into a run that tries
// nothing and passes.
func envCount(t *testing.T, name string, def, min int64) int64 {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < min {
		t.Fatalf("%s=%q: want a whole number, at least %d", name, v, min)
	}
	return n
}

// node drives one fresh in-process chain.
type node struct {
	t     *testing.T
	z     mock.MockZenon
	sup   *vm.Supervisor
	token types.ZenonTokenStandard // the token whose balances are compared
	sends []types.Hash             // accepted sends, by the number the model gives them
}

func newNode(t *testing.T) *node {
	z := mock.NewMockZenon(t)
	n := &node{t: t, z: z, sup: vm.NewSupervisor(z.Chain(), z.Consensus()), token: znn}
	z.InsertMomentumsTo(3)
	return n
}

func (n *node) balance(i int) uint64 {
	amount, err := n.z.Chain().GetFrontierAccountStore(users[i].Address).GetBalance(n.token)
	if err != nil {
		n.t.Fatal(err)
	}
	if amount == nil {
		return 0
	}
	if !amount.IsUint64() || amount.Uint64() >= 1<<47 {
		n.t.Fatalf("balance %v of account %d is outside what the model can run (Bend Nat below 2^48)", amount, i)
	}
	return amount.Uint64()
}

func (n *node) balances() []uint64 {
	out := make([]uint64, len(users))
	for i := range users {
		out[i] = n.balance(i)
	}
	return out
}

func (n *node) height(i int) uint64 {
	return n.z.Chain().GetFrontierAccountStore(users[i].Address).Identifier().Height
}

func (n *node) heights() []uint64 {
	out := make([]uint64, len(users))
	for i := range users {
		out[i] = n.height(i)
	}
	return out
}

// block builds a complete, signed account block the way a client would,
// at the height the scenario asks for: the next one, one that is already
// taken, or one that leaves a gap.
func (n *node) block(o op) *nom.AccountBlock {
	kp := users[o.acc]
	store := n.z.Chain().GetFrontierAccountStore(kp.Address)
	b := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      n.z.Chain().ChainIdentifier(),
		Address:              kp.Address,
		Height:               o.height,
		MomentumAcknowledged: n.z.Chain().GetFrontierMomentumStore().Identifier(),
		FusedPlasma:          constants.AccountBlockBasePlasma,
		Amount:               big.NewInt(0),
	}
	if o.height > 1 {
		if prev, err := store.ByHeight(o.height - 1); err != nil {
			n.t.Fatal(err)
		} else if prev != nil {
			b.PreviousHash = prev.Hash
		} else {
			// no block below this height: name one that does not exist
			b.PreviousHash = types.NewHash([]byte(fmt.Sprintf("gap below %d", o.height)))
		}
	}
	if o.kind == 'S' {
		b.BlockType = nom.BlockTypeUserSend
		b.ToAddress = users[o.to].Address
		b.TokenStandard = znn
		b.Amount = new(big.Int).SetUint64(o.amount)
	} else {
		b.BlockType = nom.BlockTypeUserReceive
		if o.id < len(n.sends) {
			b.FromBlockHash = n.sends[o.id]
		} else {
			b.FromBlockHash = types.NewHash([]byte(fmt.Sprintf("no send %d", o.id)))
		}
	}
	b.Hash = b.ComputeHash()
	b.PublicKey = kp.Public
	b.Signature = kp.Sign(b.Hash.Bytes())
	return b
}

// submit gives a block to the node on the path a published block takes:
// Supervisor.ApplyBlock (verifier, then VM), then the account pool.
func (n *node) submit(b *nom.AccountBlock) error {
	tx, err := n.sup.ApplyBlock(b)
	if err != nil {
		return err
	}
	insert := n.z.Chain().AcquireInsert("bend ledger rule")
	defer insert.Unlock()
	return n.z.Chain().AddAccountBlockTransaction(insert, tx)
}

// do applies one op to the node and says whether it was accepted.
func (n *node) do(o op) (bool, error) {
	if o.kind == 'T' {
		n.z.InsertNewMomentum()
		return true, nil
	}
	b := n.block(o)
	err := n.submit(b)
	if err != nil {
		for _, known := range refusals {
			if errors.Is(err, known) {
				return false, err
			}
		}
		n.t.Errorf("%v: refused for a reason outside the rule: %v", o, err)
		return false, err
	}
	if o.kind == 'S' {
		n.sends = append(n.sends, b.Hash)
	}
	return true, nil
}

// A scenario is a list of steps; each step sees the node as the steps
// before it left it and returns the op to try.
type step func(n *node, pending map[int]bool) op

type result struct {
	name     string
	bals     []uint64
	heights  []uint64
	ops      []op
	reasons  []string
	nodeLine string
}

func runScenario(t *testing.T, name string, steps []step) result {
	n := newNode(t)
	defer n.z.StopPanic()
	r := result{name: name, bals: n.balances(), heights: n.heights()}
	var marks strings.Builder
	dirty := map[int]bool{} // accounts with a block no momentum has confirmed yet
	for _, s := range steps {
		o := s(n, dirty)
		ok, err := n.do(o)
		r.ops = append(r.ops, o)
		if ok {
			marks.WriteByte('A')
			r.reasons = append(r.reasons, "")
			if o.kind == 'T' {
				dirty = map[int]bool{}
			} else {
				dirty[o.acc] = true
			}
		} else {
			marks.WriteByte('R')
			r.reasons = append(r.reasons, err.Error())
		}
	}
	for _, b := range n.balances() {
		fmt.Fprintf(&marks, " %d", b)
	}
	r.nodeLine = marks.String()
	return r
}

// Steps.

func tick() step { return func(*node, map[int]bool) op { return op{kind: 'T'} } }

// send at the next height. amount gets the sender's balance.
func send(from, to int, amount func(bal uint64) uint64) step {
	return func(n *node, _ map[int]bool) op {
		return op{kind: 'S', acc: from, to: to, amount: amount(n.balance(from)), height: n.height(from) + 1}
	}
}

func receive(acc, id int) step {
	return func(n *node, _ map[int]bool) op {
		return op{kind: 'R', acc: acc, id: id, height: n.height(acc) + 1}
	}
}

// at moves the height of a step by delta: 0 leaves it, -1 asks for the
// height the account's last block has, +1 leaves a gap.
func at(delta int64, s step) step {
	return func(n *node, d map[int]bool) op {
		o := s(n, d)
		o.height = uint64(int64(o.height) + delta)
		return o
	}
}

func exactly(v uint64) func(uint64) uint64 { return func(uint64) uint64 { return v } }
func all(bal uint64) uint64                { return bal }
func allPlus(v uint64) func(uint64) uint64 { return func(bal uint64) uint64 { return bal + v } }
func halfPlus(v uint64) func(uint64) uint64 {
	return func(bal uint64) uint64 { return bal/2 + v }
}

// sixTenths is six tenths of what account 0 holds at genesis, whatever it
// holds now: two of them do not fit together.
func sixTenths(uint64) uint64 { return 12000 * g.Zexp / 10 * 6 }

// The scripted scenarios: one for each place where a double spend would
// have to be stopped and that the model covers.
func scripted() map[string][]step {
	return map[string][]step{
		// more than the balance, in one block
		"overspend": {send(0, 1, allPlus(1)), send(0, 1, all), send(0, 1, exactly(1)), tick(), send(0, 1, exactly(1))},
		// two sends that each fit the balance but not together, both before
		// any momentum has confirmed the first
		"overspend-unconfirmed": {send(0, 1, sixTenths), send(0, 2, sixTenths), send(0, 2, exactly(0)), tick(), send(0, 2, sixTenths), send(0, 2, all), send(0, 2, exactly(1))},
		// one send received twice by the account it is addressed to
		"double-receive": {send(0, 1, exactly(700)), tick(), receive(1, 0), receive(1, 0), tick(), receive(1, 0), send(1, 0, all), tick(), receive(0, 1), receive(1, 0)},
		// one send received by two accounts, in either order
		"two-receivers": {send(0, 1, exactly(500)), tick(), receive(2, 0), receive(0, 0), receive(1, 0), receive(2, 0), tick(), receive(3, 0), receive(1, 0)},
		// a send that no momentum has confirmed cannot be received
		"receive-unconfirmed": {send(0, 1, exactly(9)), receive(1, 0), tick(), receive(1, 0), receive(1, 1), receive(1, 7)},
		// a second block at a height a momentum has confirmed, and a gap
		"fork-confirmed": {send(0, 1, exactly(40)), tick(), at(-1, send(0, 2, exactly(40))), at(-1, send(0, 1, all)), at(+1, send(0, 1, exactly(1))), receive(1, 0), tick(), at(-1, receive(1, 0)), send(0, 3, exactly(1))},
	}
}

// random builds a scenario from a seed. A block at a taken height is only
// tried on an account whose blocks are all confirmed: against a block that
// is still pending the node may replace the pending block instead, which
// the model does not describe (TestReplacementSpendsOnce covers it).
func random(seed int64, length int) []step {
	rng := rand.New(rand.NewSource(seed))
	steps := make([]step, 0, length+1)
	sinceTick := 0
	for i := 0; i < length; i++ {
		if sinceTick >= 4 || rng.Intn(5) == 0 {
			steps = append(steps, tick())
			sinceTick = 0
			continue
		}
		sinceTick++
		acc, to := rng.Intn(len(users)), rng.Intn(len(users))
		pick, id, delta := rng.Intn(6), rng.Intn(6), rng.Intn(8)
		var s step
		if rng.Intn(2) == 0 {
			amount := []func(uint64) uint64{exactly(0), exactly(uint64(1 + rng.Intn(1000))), halfPlus(1), all, allPlus(1), allPlus(uint64(rng.Intn(1_000_000)))}[pick]
			s = send(acc, to, amount)
		} else {
			s = func(n *node, d map[int]bool) op {
				// mostly an existing send, sometimes one that does not exist
				want := id
				if len(n.sends) > 0 && id < 5 {
					want = id % len(n.sends)
				} else {
					want = len(n.sends) + id
				}
				return receive(acc, want)(n, d)
			}
		}
		inner := s
		steps = append(steps, func(n *node, d map[int]bool) op {
			o := inner(n, d)
			switch {
			case delta == 0 && !d[o.acc] && o.height > 1:
				o.height--
			case delta == 1:
				o.height++
			}
			return o
		})
	}
	return append(steps, tick())
}

// model runs every scenario through the Bend model, in one container, and
// returns one line per scenario in the node's format.
func model(t *testing.T, results []result) []string {
	var src strings.Builder
	src.WriteString("import Base\nimport ./ledger/ledger.bend as L\nimport ./ledger/trace.bend as T\n\ndef main() -> IO(Unit):\n  do IO<Unit>:\n")
	for _, r := range results {
		blocks := make([]string, len(r.ops))
		for i, o := range r.ops {
			blocks[i] = o.bend()
		}
		fmt.Fprintf(&src, "    IO.print(T.Trace.run([%s], L.State{%s, %s, [], 0n}))\n", strings.Join(blocks, ", "), natList(r.bals), natList(r.heights))
	}
	return runBend(t, "ledger", src.String(), len(results))
}

// runBend runs a generated Bend program against the models in this
// directory and returns the lines it prints, which must be want many.
func runBend(t *testing.T, name, program string, want int) []string {
	image := os.Getenv("BEND_IMAGE")
	if image == "" {
		image = "go-zenon-bend:2.0.40"
	}
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Under Colima only the home directory is shared with the docker VM, so
	// the generated program is written next to the models, not to a temp dir.
	dir := filepath.Join(here, ".scratch", fmt.Sprintf("conformance-%s-%d", name, os.Getpid()))
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	models, err := filepath.Glob(filepath.Join(here, "*", "*.bend"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range models {
		rel, _ := filepath.Rel(here, f)
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "run.bend"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	// No network, the models mounted read-only, and a time limit: a model
	// that counts down a balance one unit at a time would run for days.
	container := fmt.Sprintf("go-zenon-bend-%s-%d", name, os.Getpid())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "run", "--rm", "--name", container, "--network", "none", "-v", dir+":/work:ro", image, "run.bend").CombinedOutput()
	if err != nil {
		// its own short limit: a daemon that stopped answering must not
		// hold the failure message back
		rmCtx, rmCancel := context.WithTimeout(context.Background(), 20*time.Second)
		_ = exec.CommandContext(rmCtx, "docker", "rm", "-f", container).Run()
		rmCancel()
		t.Fatalf("running the model: %v\n%s", err, out)
	}
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != want {
		t.Fatalf("the model printed %d lines for %d scenarios:\n%s", len(lines), want, out)
	}
	return lines
}

// TestLedgerModelMatchesNode is case BL-1.
func TestLedgerModelMatchesNode(t *testing.T) {
	var results []result
	names := []string{"overspend", "overspend-unconfirmed", "double-receive", "two-receivers", "receive-unconfirmed", "fork-confirmed"}
	for _, name := range names {
		results = append(results, runScenario(t, name, scripted()[name]))
	}
	seed, count := envCount(t, "BEND_SEED", 1, 0), int(envCount(t, "BEND_RANDOM", 40, 0))
	for i := 0; i < count; i++ {
		results = append(results, runScenario(t, fmt.Sprintf("random-%d", seed+int64(i)), random(seed+int64(i), 16)))
	}

	lines := model(t, results)
	blocks, refused := 0, map[string]int{}
	for i, r := range results {
		verdict := "same"
		if lines[i] != r.nodeLine {
			verdict = "DIFFERENT"
			t.Errorf("%s: the node and the model disagree\n  node:  %s\n  model: %s", r.name, r.nodeLine, lines[i])
			for j, o := range r.ops {
				t.Logf("  %2d %v %s", j, o, r.reasons[j])
			}
		}
		t.Logf("%-22s %s  node %s", r.name, verdict, r.nodeLine)
		for j, o := range r.ops {
			if o.kind == 'T' {
				continue
			}
			blocks++
			if r.reasons[j] != "" {
				for _, known := range refusals {
					if strings.Contains(r.reasons[j], known.Error()) {
						refused[known.Error()]++
					}
				}
			}
		}
	}
	t.Logf("%d scenarios, %d account blocks", len(results), blocks)
	for _, known := range refusals {
		t.Logf("  refused %4d times: %s", refused[known.Error()], known)
	}
}

// TestReplacementSpendsOnce is case BL-2. It checks on the node alone a
// place the model does not describe: a second block at a height whose
// block no momentum has confirmed yet. The pool either refuses it or drops
// the pending block for it. Either way one of the two sends takes effect,
// never both, and the one that lost cannot be received.
func TestReplacementSpendsOnce(t *testing.T) {
	replaced, kept := 0, 0
	for round := 0; round < 12; round++ {
		replaced, kept = replacementRound(t, round, replaced, kept)
	}
	t.Logf("12 rounds: the pending block was replaced %d times and kept %d times", replaced, kept)
}

// replacementRound is one round on its own node, stopped when the round
// ends however it ends.
func replacementRound(t *testing.T, round, replaced, kept int) (int, int) {
	{
		n := newNode(t)
		defer n.z.StopPanic()
		before := n.balance(0)
		h := n.height(0) + 1
		first := n.block(op{kind: 'S', acc: 0, to: 1, amount: uint64(1000 + round), height: h})
		if err := n.submit(first); err != nil {
			t.Fatalf("first send: %v", err)
		}
		second := n.block(op{kind: 'S', acc: 0, to: 2, amount: uint64(5000 + round), height: h})
		err := n.submit(second)
		winner, loser, winnerTo := first, second, 1
		if err == nil {
			winner, loser, winnerTo = second, first, 2
			replaced++
		} else {
			kept++
		}
		t.Logf("round %d: second block at height %d: %v", round, h, map[bool]string{true: "replaced the pending one", false: "refused: " + fmt.Sprint(err)}[err == nil])

		if got, want := n.balance(0), before-winner.Amount.Uint64(); got != want {
			t.Errorf("round %d: sender balance %d, want %d (one send, not two)", round, got, want)
		}
		if got := n.height(0); got != h {
			t.Errorf("round %d: sender height %d, want %d", round, got, h)
		}
		n.z.InsertNewMomentum()
		store := n.z.Chain().GetFrontierMomentumStore()
		if b, _ := store.GetAccountBlockByHash(winner.Hash); b == nil {
			t.Errorf("round %d: the block that won is not in the ledger", round)
		}
		if b, _ := store.GetAccountBlockByHash(loser.Hash); b != nil {
			t.Errorf("round %d: the block that lost is in the ledger", round)
		}
		// the loser cannot be received, by either account
		n.sends = []types.Hash{loser.Hash, winner.Hash}
		for _, acc := range []int{1, 2} {
			if err := n.submit(n.block(op{kind: 'R', acc: acc, id: 0, height: n.height(acc) + 1})); !errors.Is(err, verifier.ErrABFromBlockMissing) {
				t.Errorf("round %d: account %d receiving the block that lost: %v", round, acc, err)
			}
		}
		// the winner is received once, by the account it is addressed to
		toBefore := n.balance(winnerTo)
		if err := n.submit(n.block(op{kind: 'R', acc: winnerTo, id: 1, height: n.height(winnerTo) + 1})); err != nil {
			t.Errorf("round %d: receiving the block that won: %v", round, err)
		}
		if err := n.submit(n.block(op{kind: 'R', acc: winnerTo, id: 1, height: n.height(winnerTo) + 1})); !errors.Is(err, verifier.ErrABFromBlockAlreadyReceived) {
			t.Errorf("round %d: receiving the block that won a second time: %v", round, err)
		}
		if got, want := n.balance(winnerTo), toBefore+winner.Amount.Uint64(); got != want {
			t.Errorf("round %d: receiver balance %d, want %d", round, got, want)
		}
	}
	return replaced, kept
}

// TestRollbackKeepsTotal is case BL-3. It checks on the node alone the
// other place the model does not describe: a momentum rollback. A send is
// confirmed and received, then the chain is rolled back to before the
// send. Afterwards the value the two accounts hold, plus the send if it is
// back in transit, is what it was at the start, whatever the pool kept.
func TestRollbackKeepsTotal(t *testing.T) {
	n := newNode(t)
	defer n.z.StopPanic()
	start := n.z.Chain().GetFrontierMomentumStore().Identifier()
	total := n.balance(0) + n.balance(1)
	const amount = 123456

	send := n.block(op{kind: 'S', acc: 0, to: 1, amount: amount, height: n.height(0) + 1})
	if err := n.submit(send); err != nil {
		t.Fatal(err)
	}
	n.z.InsertNewMomentum()
	n.sends = []types.Hash{send.Hash}
	recv := n.block(op{kind: 'R', acc: 1, id: 0, height: n.height(1) + 1})
	if err := n.submit(recv); err != nil {
		t.Fatal(err)
	}
	n.z.InsertNewMomentum()
	if got := n.balance(0) + n.balance(1); got != total {
		t.Fatalf("before the rollback the two accounts hold %d, want %d", got, total)
	}

	insert := n.z.Chain().AcquireInsert("bend ledger rule rollback")
	err := n.z.Chain().RollbackTo(insert, start)
	insert.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	check := func(when string) {
		store := n.z.Chain().GetFrontierAccountStore(users[0].Address)
		sent, _ := store.ByHash(send.Hash)
		received := n.z.Chain().GetFrontierAccountStore(users[1].Address).IsReceived(send.Hash)
		inTransit := uint64(0)
		if sent != nil && !received {
			inTransit = amount
		}
		got := n.balance(0) + n.balance(1) + inTransit
		t.Logf("%s: send in the sender's chain: %v, received: %v, accounts plus transit: %d", when, sent != nil, received, got)
		if got != total {
			t.Errorf("%s: accounts plus transit %d, want %d", when, got, total)
		}
		if received && sent == nil {
			t.Errorf("%s: the receive is kept while its send is gone", when)
		}
	}
	check("after the rollback")
	n.z.InsertNewMomentum()
	check("one momentum later")
	// the receive that was rolled back cannot be added a second time on top
	// of whatever is there now
	if received := n.z.Chain().GetFrontierAccountStore(users[1].Address).IsReceived(send.Hash); received {
		if err := n.submit(n.block(op{kind: 'R', acc: 1, id: 0, height: n.height(1) + 1})); !errors.Is(err, verifier.ErrABFromBlockAlreadyReceived) {
			t.Errorf("second receive after the rollback: %v", err)
		}
	}
	n.z.InsertNewMomentum()
	check("two momentums later")
}
