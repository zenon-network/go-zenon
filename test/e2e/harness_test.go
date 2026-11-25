//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
)

// wait is how long a case waits for a block to be in a momentum. Momentums
// are 10 seconds apart; a contract call needs two.
const wait = 2 * time.Minute

var (
	net     *Net
	runBase uint32   // first derivation index of this run's fresh accounts
	runTag  string   // unique per run on one chain; used in names
	runLock *os.File // held for the whole run: one driver per devnet at a time
	cov     = &coverage{rows: map[string]bool{}}

	// fast is true when driver and node are the test build with short
	// epochs and locks; then only the TestFast groups run.
	fast bool

	// live says which sporks are enforced on this chain when the run starts.
	live = map[string]bool{}

	znn = types.ZnnTokenStandard
	qsr = types.QsrTokenStandard
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func exit(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "e2e: "+format+"\n", args...)
	os.Exit(2)
}

// envNumber reads an optional number; a value that is not a number stops the
// run instead of silently becoming zero.
func envNumber(key string, def uint64) uint64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		exit("%s=%q is not a number", key, v)
	}
	return n
}

func TestMain(m *testing.M) {
	urls := strings.Split(envOr("E2E_RPC", "http://localhost:"+envOr("DEVNET_HTTP_PORT", "35997")+",http://localhost:"+envOr("DEVNET_PILLAR_HTTP_PORT", "35991")), ",")
	chainID := envNumber("E2E_CHAIN_ID", 69)
	minHeight := envNumber("E2E_MIN_HEIGHT", 0)
	if len(urls) < 2 {
		exit("E2E_RPC names %d endpoint; the agreement checks need at least two", len(urls))
	}

	var err error
	if net, err = Dial(urls, chainID); err != nil {
		exit("%v", err)
	}
	// One driver process per devnet at a time: two runs that start at the
	// same height derive the same accounts and names (runBase, runTag
	// below), and the per-account locks of Net do not reach across
	// processes. The lock is named after the chain itself (its identifier
	// and genesis momentum), so endpoints listed in another order or by
	// another address take the same lock. It is a lock on this host, the
	// one the devnet's containers run on (E2E_NODES reads them with docker
	// exec), and it is held until the process exits.
	var genesis struct {
		List []struct {
			Height uint64     `json:"height"`
			Hash   types.Hash `json:"hash"`
		} `json:"list"`
	}
	for deadline := time.Now().Add(3 * time.Minute); ; time.Sleep(2 * time.Second) {
		err = net.Call(&genesis, "ledger.getMomentumsByHeight", 1, 1)
		if err == nil && len(genesis.List) == 1 && genesis.List[0].Height == 1 {
			break
		}
		if time.Now().After(deadline) {
			exit("the genesis momentum could not be read within 3 minutes: %v %+v", err, genesis.List)
		}
	}
	lockName := fmt.Sprintf("go-zenon-e2e-%d-%s.lock", chainID, genesis.List[0].Hash)
	// In /tmp, not os.TempDir(): runs with different TMPDIR values must meet
	// the same file. It is readable by everyone, so another user's run can
	// open it read-only and take the same lock.
	if runLock, err = openRunLock(filepath.Join("/tmp", lockName)); err != nil {
		exit("run lock: %v", err)
	}
	if err = syscall.Flock(int(runLock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		exit("another driver run holds %s; one run per devnet at a time (%v)", runLock.Name(), err)
	}
	// E2E_NODES: containers of the devnet, read through docker exec, so that
	// "every node" covers the nodes that publish no RPC port as well.
	for _, name := range strings.Split(os.Getenv("E2E_NODES"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			net.Containers = append(net.Containers, "znnd-devnet-"+name)
		}
	}

	// Wait until every node answers, with a limit, before anything is read.
	var nodes []NodeFrontier
	for deadline := time.Now().Add(3 * time.Minute); ; time.Sleep(2 * time.Second) {
		if nodes, err = net.Frontiers(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			exit("the nodes did not all answer within 3 minutes: %v", err)
		}
	}

	// The driver and every node must be built from the same constants: all
	// stock, or all with the fast test overlay (docs/TESTING.md, P11). This is
	// checked first, on every node, because every expected value below
	// depends on it.
	fast = constants.MomentumsPerEpoch != 8640
	for _, n := range nodes {
		if nodeFast := strings.HasSuffix(n.Version, "+fast-constants"); nodeFast != fast {
			exit("node %s runs %q but the driver was built with fast constants = %v; build both the same way", n.Node, n.Version, fast)
		}
		if n.Version != nodes[0].Version {
			exit("node %s runs %q and node %s runs %q; this driver does not test mixed networks", n.Node, n.Version, nodes[0].Node, nodes[0].Version)
		}
	}

	// What is enforced is taken from the genesis file the chain was started
	// on, and every endpoint must list exactly that: the node's own list is
	// not the source of what the cases expect.
	// The cases' picture of what is enforced, and the height they wait for,
	// come from that file too, once every endpoint is shown to agree with it.
	sporks, last, err := sporksAsInGenesis(os.Getenv("E2E_GENESIS"))
	if err != nil {
		exit("%v", err)
	}
	if minHeight == 0 {
		// Start after the last scheduled spork, so the cases see one fixed state.
		minHeight = last + 3
	}
	if err = net.WaitHeight(minHeight, 15*time.Minute); err != nil {
		exit("%v", err)
	}
	// Nothing is published before the network is one chain that every genesis
	// pillar builds on: a block published while the pillars still disagree
	// names a momentum that a rollback can remove, and is then lost for good.
	if err = waitSettled(os.Getenv("E2E_GENESIS"), 10*time.Minute); err != nil {
		exit("%v", err)
	}
	f, err := net.Frontier()
	if err != nil {
		exit("%v", err)
	}
	// Fresh accounts and names for every run, so a run can be repeated on
	// the same chain without meeting the state of an earlier run. Slots go
	// up to 255 per run. The indexes are uint32, so the height must leave
	// room for a whole run's slots.
	if f.Height > (math.MaxUint32-1000)/256-1 {
		exit("start height %d is too high for this run's account indexes (uint32)", f.Height)
	}
	runBase = 1000 + uint32(f.Height)*256
	runTag = strconv.FormatUint(f.Height, 10)
	var names []string
	for name, height := range sporks {
		if height <= f.Height {
			live[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	fmt.Printf("e2e: endpoints %v, containers %v, chain %d, start height %d, account indexes from %d\n", urls, net.Containers, chainID, f.Height, runBase)
	fmt.Printf("e2e: sporks enforced: %v\n", names)
	fmt.Printf("e2e: %d nodes run %s, fast constants: %v\n", len(nodes), nodes[0].Version, fast)

	// On the fast build one compiled value is visible from outside: the
	// bridge administrator. It is checked on every endpoint, so a binary that
	// carries the version suffix but other constants does not get through.
	if contractLive("bridge") {
		for i, url := range urls {
			var info struct {
				Administrator types.Address `json:"administrator"`
			}
			if err = net.CallOn(i, &info, "embedded.bridge.getBridgeInfo"); err != nil {
				exit("%s: bridge info: %v", url, err)
			}
			if info.Administrator != constants.InitialBridgeAdministrator {
				exit("%s reports bridge administrator %s; the driver's constants say %s", url, info.Administrator, constants.InitialBridgeAdministrator)
			}
		}
	}

	if err = ensureFunderPlasma(); err != nil {
		exit("funder setup: %v", err)
	}

	code := m.Run()
	// Whatever the cases did: no block the node refused at publish, in any
	// group, may be known to any node when the run ends: not committed
	// (lookup by hash) and not held unconfirmed in a pool. The committed
	// lookups run before and after the pools are read, so a block moving
	// from a pool into a momentum meanwhile is seen; a pool that changes
	// while it is read is read again, up to five times.
	if why := refusedGone(); why != "" {
		fmt.Fprintf(os.Stderr, "e2e: %s\n", why)
		code = 1
	} else {
		fmt.Printf("e2e: %d refused blocks are absent from committed lookups and from the unconfirmed blocks of their accounts on %d endpoints and %d containers at the end of the run\n", len(net.Refused()), len(net.URLs), len(net.Containers))
	}
	if path := os.Getenv("E2E_COVERAGE"); path != "" {
		if !filepath.IsAbs(path) {
			fmt.Fprintf(os.Stderr, "e2e: E2E_COVERAGE=%q is relative; the driver runs in test/e2e, so give an absolute path\n", path)
			code = 2
		} else if err := cov.write(path); err != nil {
			fmt.Fprintln(os.Stderr, "e2e: coverage file:", err)
			code = 2
		}
	}
	os.Exit(code)
}

// Spork names used in live, by the id compiled into znnd.
var sporkNames = map[types.Hash]string{
	types.AcceleratorSpork.SporkId:        "accelerator",
	types.HtlcSpork.SporkId:               "htlc",
	types.BridgeAndLiquiditySpork.SporkId: "bridge",
	types.PtlcSpork.SporkId:               "ptlc",
	types.Libp2pSpork.SporkId:             "libp2p",
	types.DynamicPlasmaSpork.SporkId:      "dynamic-plasma",
	types.EmbeddedRefundSpork.SporkId:     "embedded-refund",
	types.EpochRewardTermsSpork.SporkId:   "epoch-reward-terms",
}

// waitSettled returns when, at two different heights, every
// endpoint and container has the same frontier momentum and, among the last
// 60 momentums below it, each pillar of the genesis file has produced at
// least one. Until then the pillars are not shown to build on one chain: at
// start-up a pillar that is still fetching does not take in the momentums
// produced meanwhile, produces on an older one, and the shorter branch is
// rolled back. The history it reads must be a chain: consecutive heights,
// each momentum naming the one before it, no hash twice.
//
// This is a precondition for the cases, not a verdict on start-up. What it
// saw while waiting is printed, and the rollbacks of every chain are
// recorded by test/devnet/down.sh.
func waitSettled(genesisPath string, timeout time.Duration) error {
	raw, err := os.ReadFile(genesisPath)
	if err != nil {
		return fmt.Errorf("E2E_GENESIS: %v", err)
	}
	var genesis struct {
		PillarConfig struct {
			Pillars []struct {
				Name                  string
				BlockProducingAddress types.Address
			}
		}
	}
	if err = json.Unmarshal(raw, &genesis); err != nil {
		return fmt.Errorf("E2E_GENESIS %s: %v", genesisPath, err)
	}
	if len(genesis.PillarConfig.Pillars) == 0 {
		return fmt.Errorf("E2E_GENESIS %s names no pillar", genesisPath)
	}
	type momentum struct {
		Height       uint64        `json:"height"`
		Hash         types.Hash    `json:"hash"`
		PreviousHash types.Hash    `json:"previousHash"`
		Producer     types.Address `json:"producer"`
	}
	// once reports why the network is not settled now, or "" and the tip.
	// Every read in it is bounded by ctx, which ends at the overall deadline.
	once := func(ctx context.Context) (string, uint64, types.Hash, int, map[uint64]types.Hash) {
		nodes, err := net.FrontiersCtx(ctx)
		if err != nil {
			return err.Error(), 0, types.Hash{}, 0, nil
		}
		for _, n := range nodes {
			if n.Height != nodes[0].Height || n.Hash != nodes[0].Hash {
				return fmt.Sprintf("frontiers differ: %v", nodes), 0, types.Hash{}, 0, nil
			}
		}
		top := nodes[0].Height
		if top < 2 || nodes[0].Hash == (types.Hash{}) {
			return "only the genesis momentum exists", 0, types.Hash{}, 0, nil
		}
		from := uint64(2)
		if top > 61 {
			from = top - 59
		}
		var page struct {
			List []momentum `json:"list"`
		}
		// One height below as well, so the first momentum's link is checked too.
		hctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = net.CallOnCtx(hctx, 0, &page, "ledger.getMomentumsByHeight", from-1, top-from+2)
		cancel()
		if err != nil {
			return err.Error(), 0, types.Hash{}, 0, nil
		}
		if uint64(len(page.List)) != top-from+2 {
			return fmt.Sprintf("asked for heights %d to %d, got %d momentums", from-1, top, len(page.List)), 0, types.Hash{}, 0, nil
		}
		hashes, produced := map[types.Hash]bool{}, map[types.Address]bool{}
		for i, m := range page.List {
			switch {
			case m.Height != from-1+uint64(i):
				return fmt.Sprintf("the history from height %d has height %d at position %d", from-1, m.Height, i), 0, types.Hash{}, 0, nil
			case m.Hash == (types.Hash{}) || hashes[m.Hash]:
				return fmt.Sprintf("the history has hash %s twice or empty, at height %d", m.Hash, m.Height), 0, types.Hash{}, 0, nil
			case i > 0 && m.PreviousHash != page.List[i-1].Hash:
				return fmt.Sprintf("momentum %d names %s as previous; the momentum below it is %s", m.Height, m.PreviousHash, page.List[i-1].Hash), 0, types.Hash{}, 0, nil
			}
			hashes[m.Hash] = true
			if i > 0 {
				produced[m.Producer] = true
			}
		}
		if page.List[len(page.List)-1].Hash != nodes[0].Hash {
			return fmt.Sprintf("the chain moved while it was read at height %d", top), 0, types.Hash{}, 0, nil
		}
		var missing []string
		for _, p := range genesis.PillarConfig.Pillars {
			if !produced[p.BlockProducingAddress] {
				missing = append(missing, p.Name)
			}
		}
		if len(missing) != 0 {
			return fmt.Sprintf("at height %d, which all %d nodes share, no momentum from height %d on is by %v", top, len(nodes), from, missing), 0, types.Hash{}, 0, nil
		}
		chain := map[uint64]types.Hash{}
		for _, m := range page.List {
			chain[m.Height] = m.Hash
		}
		return "", top, nodes[0].Hash, len(nodes), chain
	}
	started := time.Now()
	last, differed, restarts, lastDiffer := "no reading yet", 0, 0, ""
	var firstTop uint64
	var firstHash types.Hash
	deadline := started.Add(timeout)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	for first := true; ; first = false {
		if !first {
			// A second between readings, but never past the deadline.
			pause := time.Second
			if left := time.Until(deadline); left < pause {
				pause = left
			}
			time.Sleep(pause)
		}
		why, top, hash, nodes, chain := once(ctx)
		// The deadline holds for the reading as well: one that came back
		// after it does not count, and no read outlives it.
		if time.Now().After(deadline) {
			return fmt.Errorf("the network did not settle on one chain with every genesis pillar producing within %s: %s (readings with differing frontiers: %d, observations started over: %d)", timeout, last, differed, restarts)
		}
		if why != "" {
			last = why
			if strings.HasPrefix(why, "frontiers differ") {
				differed, lastDiffer = differed+1, why
			}
			// Two settled readings count only with no unsettled reading
			// between them: a disagreement, a step back to genesis, a broken
			// history or a failed read all start the observation over.
			if firstTop != 0 {
				firstTop, restarts = 0, restarts+1
			}
			continue
		}
		switch {
		case firstTop == 0:
			firstTop, firstHash = top, hash
			last = fmt.Sprintf("settled at height %d; waiting to see the chain grow on that momentum", top)
		case top == firstTop && hash == firstHash:
			// The same tip; keep waiting.
		case top > firstTop && chain[firstTop] == firstHash:
			fmt.Printf("e2e: settled after %s: all %d nodes have momentum %s at height %d, built on the momentum %s they agreed on at height %d, the history below is one linked chain, and each of the %d genesis pillars produced in it\n",
				time.Since(started).Round(time.Second), nodes, hash, top, firstHash, firstTop, len(genesis.PillarConfig.Pillars))
			// Frontiers read a second apart differ whenever a momentum is on
			// its way, so these counts are a record, not a finding by themselves.
			fmt.Printf("e2e: while waiting, %d readings had differing frontiers and the observation was started over %d times; the last difference: %s\n", differed, restarts, lastDiffer)
			return nil
		default:
			// Lower, replaced at the same height, or not built on the first
			// reading: the chain went back. Start over from this reading.
			last = fmt.Sprintf("the chain went back: height %d momentum %s was read first, then height %d momentum %s which is not built on it", firstTop, firstHash, top, hash)
			firstTop, firstHash, restarts = top, hash, restarts+1
		}
	}
}

// sporksAsInGenesis requires every endpoint to list every spork of the
// genesis file exactly once, with the same activation flag and enforcement
// height, and no other spork that this binary implements. It returns the
// activated sporks this binary implements, by name with their enforcement
// height, and the highest of those heights: both from the file, not from
// what a node answered.
func sporksAsInGenesis(path string) (map[string]uint64, uint64, error) {
	if path == "" {
		return nil, 0, fmt.Errorf("E2E_GENESIS is not set: give the genesis file the devnet was started on (docker/devnet/genesis.json for stock)")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("E2E_GENESIS: %v", err)
	}
	type spork struct {
		Id                types.Hash `json:"id"`
		Activated         bool       `json:"activated"`
		EnforcementHeight uint64     `json:"enforcementHeight"`
	}
	var genesis struct {
		SporkConfig *struct {
			Sporks []spork `json:"Sporks"`
		} `json:"SporkConfig"`
	}
	if err = json.Unmarshal(raw, &genesis); err != nil {
		return nil, 0, fmt.Errorf("E2E_GENESIS %s: %v", path, err)
	}
	want := map[types.Hash]spork{}
	if genesis.SporkConfig != nil {
		for _, s := range genesis.SporkConfig.Sporks {
			if _, twice := want[s.Id]; twice {
				return nil, 0, fmt.Errorf("E2E_GENESIS %s names spork %s twice", path, s.Id)
			}
			want[s.Id] = s
		}
	}
	for i, url := range net.URLs {
		// Both members must be there: an answer of {} is not an empty list.
		var list struct {
			Count *int     `json:"count"`
			List  *[]spork `json:"list"`
		}
		if err = net.CallOn(i, &list, "embedded.spork.getAll", 0, 50); err != nil {
			return nil, 0, fmt.Errorf("%s: spork list: %v", url, err)
		}
		if list.Count == nil || list.List == nil || *list.Count != len(*list.List) {
			return nil, 0, fmt.Errorf("%s: the spork list is not a whole page with count and list (count present %v, list present %v)", url, list.Count != nil, list.List != nil)
		}
		seen := map[types.Hash]bool{}
		for _, s := range *list.List {
			if seen[s.Id] {
				return nil, 0, fmt.Errorf("%s lists spork %s twice", url, s.Id)
			}
			seen[s.Id] = true
			w, ok := want[s.Id]
			if _, implemented := sporkNames[s.Id]; !ok && !implemented {
				continue // a spork an earlier run of the spork cases created; no binary implements it
			}
			if !ok || w != s {
				return nil, 0, fmt.Errorf("%s lists spork %s as activated=%v at height %d; the genesis file %s says %+v (in file: %v)", url, s.Id, s.Activated, s.EnforcementHeight, path, w, ok)
			}
		}
		for id := range want {
			if !seen[id] {
				return nil, 0, fmt.Errorf("%s does not list spork %s of the genesis file %s: this chain was not started on that file", url, id, path)
			}
		}
	}
	fmt.Printf("e2e: %d sporks on %d endpoints are those of %s\n", len(want), len(net.URLs), path)
	out, last := map[string]uint64{}, uint64(0)
	for id, s := range want {
		name, known := sporkNames[id]
		if !known || !s.Activated {
			continue
		}
		out[name] = s.EnforcementHeight
		if s.EnforcementHeight > last {
			last = s.EnforcementHeight
		}
	}
	return out, last, nil
}

// contractLive applies the spork ladder (vm/embedded, sporkVariantKey):
// HTLC implies Bridge and Liquidity, and those imply Accelerator.
func contractLive(name string) bool {
	switch name {
	case "htlc":
		return live["htlc"]
	case "bridge", "liquidity":
		return live["bridge"] || live["htlc"]
	case "accelerator":
		return live["accelerator"] || live["bridge"] || live["htlc"]
	case "ptlc":
		return live["ptlc"]
	}
	return true
}

// needs skips a group whose contract is not live on this chain. The spork
// ladder group checks that such a contract refuses every method.
func needs(t *testing.T, contract string) {
	t.Helper()
	if !contractLive(contract) {
		t.Skipf("the %s contract is not live on this chain", contract)
	}
}

// ensureFunderPlasma gives the funder account fused plasma from pillar
// owner 1, which holds fused QSR at genesis, so that no setup step needs
// proof of work.
func ensureFunderPlasma() error {
	funder := net.Key(IdxFunder)
	var info struct {
		CurrentPlasma uint64 `json:"currentPlasma"`
	}
	if err := net.Call(&info, "embedded.plasma.get", funder.Address); err != nil {
		return err
	}
	if info.CurrentPlasma >= 5_000_000 {
		return nil
	}
	res, err := net.CallContract(net.Key(IdxOwner1), types.PlasmaContract,
		definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, funder.Address), qsr, coins(5000), wait)
	if err != nil {
		return err
	}
	if !res.OK() {
		return fmt.Errorf("fuse for the funder failed at the contract")
	}
	return nil
}

// group starts a normal case group: it runs in parallel with the others
// and is skipped in a destructive run, which must be alone on the chain.
func group(t *testing.T) {
	t.Helper()
	if os.Getenv("E2E_DESTRUCTIVE") == "1" {
		t.Skip("skipped in a destructive run")
	}
	if fast {
		t.Skip("skipped on the fast test build; its windows are too short for these cases")
	}
	t.Parallel()
}

// fastGroup starts a group that needs the test build with fast constants.
func fastGroup(t *testing.T) {
	t.Helper()
	if os.Getenv("E2E_DESTRUCTIVE") == "1" || !fast {
		t.Skip("needs the test build with fast constants (docs/TESTING.md, P11)")
	}
	t.Parallel()
}

// eventually polls cond every five seconds until it holds, and fails the
// case if it does not within the timeout.
func eventually(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("not reached in %s: %s", timeout, what)
		}
		time.Sleep(5 * time.Second)
	}
}

// fundFrom is newAccount with another funding account. Devnet account 8 is
// kept for the cases that need large amounts.
func fundFrom(t *testing.T, funder *wallet.KeyPair, slot uint32, znnCoins, qsrCoins int64) *wallet.KeyPair {
	t.Helper()
	kp := net.Key(runBase + slot)
	callOK(t, net.Key(IdxFunder), types.PlasmaContract, definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, kp.Address), qsr, coins(2000))
	pending := 0
	if znnCoins > 0 {
		transfer(t, funder, kp.Address, znn, coins(znnCoins), nil)
		pending++
	}
	if qsrCoins > 0 {
		transfer(t, funder, kp.Address, qsr, coins(qsrCoins), nil)
		pending++
	}
	receiveAll(t, kp, pending)
	return kp
}

// coins converts whole coins of an 8-decimal token to base units.
func coins(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(100000000))
}

func units(n int64) *big.Int { return big.NewInt(n) }

// coverage collects "kind, item, class:case" rows from the cases that
// passed. test/inventory/record.py merges the file into coverage.tsv.
type coverage struct {
	mu   sync.Mutex
	rows map[string]bool
}

func (c *coverage) add(kind, item, token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rows[kind+"\t"+item+"\t"+token] = true
}

func (c *coverage) write(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	rows := make([]string, 0, len(c.rows))
	for r := range c.rows {
		rows = append(rows, r)
	}
	sort.Strings(rows)
	return os.WriteFile(path, []byte(strings.Join(rows, "\n")+"\n"), 0o644)
}

// caseID is the id in front of the first underscore of the subtest name:
// "TestPlasma/PL-1_fuse" gives "PL-1".
func caseID(t *testing.T) string {
	name := t.Name()
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.Index(name, "_"); i >= 0 {
		name = name[:i]
	}
	return name
}

// cover records that this case exercises an inventory item in an edge
// class (docs/TESTING.md section 1). The row is kept only if the case
// passes.
func cover(t *testing.T, class, kind string, items ...string) {
	t.Helper()
	id := caseID(t)
	t.Cleanup(func() {
		if t.Failed() || t.Skipped() {
			return
		}
		for _, item := range items {
			cov.add(kind, item, class+":"+id)
		}
	})
}

var apiTypes = map[string]string{
	"ledger": "LedgerApi", "stats": "StatsApi",
	"embedded.accelerator": "AcceleratorApi", "embedded.bridge": "BridgeApi", "embedded.htlc": "HtlcApi",
	"embedded.liquidity": "LiquidityApi", "embedded.pillar": "PillarApi", "embedded.plasma": "PlasmaApi",
	"embedded.ptlc": "PtlcApi", "embedded.sentinel": "SentinelApi", "embedded.spork": "SporkApi",
	"embedded.stake": "StakeApi", "embedded.swap": "SwapApi", "embedded.token": "TokenAPI",
}

// rpcItem maps "embedded.plasma.get" to the inventory name "PlasmaApi.Get".
func rpcItem(method string) string {
	i := strings.LastIndex(method, ".")
	ns, name := method[:i], method[i+1:]
	return apiTypes[ns] + "." + strings.ToUpper(name[:1]) + name[1:]
}

// get calls a read-only RPC method, fails the case on an error and records
// the method as exercised.
func get(t *testing.T, result interface{}, method string, args ...interface{}) {
	t.Helper()
	if err := net.Call(result, method, args...); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	cover(t, "E1", "rpc-method", rpcItem(method))
}

func bal(t *testing.T, addr types.Address, zts types.ZenonTokenStandard) *big.Int {
	t.Helper()
	b, err := net.Balance(addr, zts)
	if err != nil {
		t.Fatalf("balance of %s: %v", addr, err)
	}
	return b
}

func wantBig(t *testing.T, what string, got, want *big.Int) {
	t.Helper()
	if got.Cmp(want) != 0 {
		t.Errorf("%s: got %s, want %s", what, got, want)
	}
}

// call sends a contract call and waits for the contract's receive.
func call(kp *wallet.KeyPair, to types.Address, data []byte, zts types.ZenonTokenStandard, amount *big.Int) (*Result, error) {
	return net.CallContract(kp, to, data, zts, amount, wait)
}

// callOK, callFail and callReject send a contract call and require one of
// the three outcomes: executed, refused by the contract, refused at publish.
func callOK(t *testing.T, kp *wallet.KeyPair, to types.Address, data []byte, zts types.ZenonTokenStandard, amount *big.Int) *Result {
	t.Helper()
	res, err := call(kp, to, data, zts, amount)
	return mustOK(t, res, err)
}

// probe is a read-only RPC call whose answer a failed call must not change.
// The answer must be a successful read. A probe made with watchMissing
// instead expects the read to fail with one given error, before and after:
// two failed reads are otherwise no evidence that state is unchanged.
type probe struct {
	method  string
	args    []interface{}
	missing string
	null    bool
	// check, if set, says what is wrong with an answer as the state the case
	// expects, or "": it runs on each reading, before and after the call.
	check func(raw json.RawMessage) string
}

func watch(method string, args ...interface{}) probe { return probe{method: method, args: args} }

// watchWith: like watch, and each reading must also pass check.
func watchWith(check func(json.RawMessage) string, method string, args ...interface{}) probe {
	return probe{method: method, args: args, check: check}
}

// watchMissing: the read must fail with this error, before and after.
func watchMissing(wantErr, method string, args ...interface{}) probe {
	return probe{method: method, args: args, missing: wantErr}
}

// watchNull: the read must succeed with the answer null, before and after,
// for a getter that reports "there is none" that way.
func watchNull(method string, args ...interface{}) probe {
	return probe{method: method, args: args, null: true}
}

// snapshot is the answer of each probe as JSON.
func snapshot(t *testing.T, probes []probe) []string {
	t.Helper()
	out := make([]string, len(probes))
	for i, p := range probes {
		var raw json.RawMessage
		err := net.Call(&raw, p.method, p.args...)
		switch {
		case p.missing != "" && err != nil && err.Error() == p.missing:
			out[i] = "missing: " + p.missing
		// A result of null reaches the caller as an untouched, empty value:
		// the client stores nothing for it, and answers a missing result
		// member with an error instead. So no error and nothing stored is null.
		case p.null && err == nil && (len(raw) == 0 || strings.TrimSpace(string(raw)) == "null"):
			out[i] = "null"
		case p.null:
			t.Fatalf("probe %s%v: want the answer null, got %.200s (error %v)", p.method, p.args, raw, err)
		case p.missing != "":
			t.Fatalf("probe %s%v: want the error %q, got %v with answer %.200s", p.method, p.args, p.missing, err, raw)
		case err != nil || !stateShaped(raw):
			// State is an object, a list or an amount. null, true and false are
			// not state: they are what a getter that lost its entry answers.
			t.Fatalf("probe %s%v did not read state (error %v, answer %.200s); a probe must succeed, or be made with watchMissing or watchNull", p.method, p.args, err, raw)
		case stateOf(p, raw) != "":
			t.Fatalf("probe %s%v: %s (answer %.200s)", p.method, p.args, stateOf(p, raw), raw)
		case p.check != nil && p.check(raw) != "":
			t.Fatalf("probe %s%v: %s (answer %.200s)", p.method, p.args, p.check(raw), raw)
		default:
			out[i] = string(raw)
		}
	}
	return out
}

// stateShaped: what a state getter answers is an object, a list, or an
// amount as a number or a string of digits. Anything else, null and the
// booleans included, is not a reading of state.
var amountShaped = regexp.MustCompile(`^(-?[0-9]+|"[0-9]+")$`)

func stateShaped(raw json.RawMessage) bool {
	t := strings.TrimSpace(string(raw))
	if len(t) == 0 {
		return false
	}
	switch t[0] {
	case '{', '[':
		return json.Valid(raw)
	}
	return amountShaped.MatchString(t)
}

// stateSpec says what a getter that a probe may watch answers: each member
// with its kind; members that must equal an argument of the call (member ->
// argument index); and, for a page, the arguments that give its index and
// size. A getter with no members answers an amount. A getter that is not
// listed cannot be watched: an answer is only evidence of unchanged state if
// it is recognisably the getter's own answer about what was asked, not {}, a
// stray number, or another object's state.
type stateSpec struct {
	fields    map[string]string // member -> hash, address, zts, amount, uint, bool, string, list
	keys      map[string]int
	pageIndex int // argument index of the page index, or -1
	pageSize  int
	// rows: for a page, each entry's members and kinds; rowKeys: entry
	// members that must equal an argument (the account asked about); rowID:
	// the member that must be unique within the page.
	rows    map[string]string
	rowKeys map[string]int
	rowID   string
	rowIDs  []string // members that together must be unique within the page
}

// pairFields are the members of a bridge token pair (TokenPairMarshall).
var pairFields = map[string]string{"tokenStandard": "zts", "tokenAddress": "string", "bridgeable": "bool", "redeemable": "bool", "owned": "bool",
	"minAmount": "digits", "feePercentage": "uint", "redeemDelay": "uint", "metadata": "string"}

// networkFields are the members of a bridge network (NetworkInfo).
var networkFields = map[string]string{"networkClass": "uint", "chainId": "uint", "name": "string", "contractAddress": "string", "metadata": "string", "tokenPairs": "pairs"}

var stateSpecs = map[string]stateSpec{
	"embedded.accelerator.getVoteBreakdown": {fields: map[string]string{"id": "hash", "total": "uint", "yes": "uint", "no": "uint"}, keys: map[string]int{"id": 0}, pageIndex: -1},
	"embedded.bridge.getAllNetworks":        {fields: map[string]string{"count": "uint", "list": "list"}, pageIndex: 0, pageSize: 1, rows: networkFields, rowIDs: []string{"networkClass", "chainId"}},
	"embedded.bridge.getBridgeInfo": {fields: map[string]string{"administrator": "address", "compressedTssECDSAPubKey": "string", "decompressedTssECDSAPubKey": "string",
		"allowKeyGen": "bool", "halted": "bool", "unhaltedAt": "uint", "unhaltDurationInMomentums": "uint", "tssNonce": "uint", "metadata": "string"}, pageIndex: -1},
	"embedded.bridge.getNetworkInfo":      {fields: networkFields, keys: map[string]int{"networkClass": 0, "chainId": 1}, pageIndex: -1},
	"embedded.bridge.getOrchestratorInfo": {fields: map[string]string{"windowSize": "uint", "keyGenThreshold": "uint", "confirmationsToFinality": "uint", "estimatedMomentumTime": "uint", "allowKeyGenHeight": "uint"}, pageIndex: -1},
	"embedded.htlc.getById": {fields: map[string]string{"id": "hash", "timeLocked": "address", "hashLocked": "address", "tokenStandard": "zts", "amount": "amount",
		"expirationTime": "uint", "hashType": "uint", "keyMaxSize": "uint", "hashLock": "string"}, keys: map[string]int{"id": 0}, pageIndex: -1},
	"embedded.pillar.getDelegatedPillar": {fields: map[string]string{"name": "string", "status": "uint", "weight": "amount"}, pageIndex: -1},
	"embedded.pillar.getDepositedQsr":    {pageIndex: -1},
	"embedded.plasma.getEntriesByAddress": {fields: map[string]string{"qsrAmount": "amount", "count": "uint", "list": "list"}, pageIndex: 1, pageSize: 2,
		rows: map[string]string{"qsrAmount": "amount", "beneficiary": "address", "expirationHeight": "uint", "id": "hash", "isRevocable": "bool"}, rowID: "id"},
	"embedded.ptlc.getById": {fields: map[string]string{"id": "hash", "timeLocked": "address", "tokenStandard": "zts", "amount": "amount", "expirationTime": "uint",
		"pointType": "uint", "pointLock": "string"}, keys: map[string]int{"id": 0}, pageIndex: -1},
	"embedded.ptlc.getReclaimLockById":  {fields: map[string]string{"id": "hash", "pointType": "uint", "pointLock": "string"}, keys: map[string]int{"id": 0}, pageIndex: -1},
	"embedded.sentinel.getDepositedQsr": {pageIndex: -1},
	"embedded.spork.getAll": {fields: map[string]string{"count": "uint", "list": "list"}, pageIndex: 0, pageSize: 1,
		rows: map[string]string{"id": "hash", "name": "string", "description": "string", "activated": "bool", "enforcementHeight": "uint"}, rowID: "id"},
	"embedded.stake.getEntriesByAddress": {fields: map[string]string{"totalAmount": "amount", "totalWeightedAmount": "amount", "count": "uint", "list": "list"}, pageIndex: 1, pageSize: 2,
		rows:    map[string]string{"amount": "amount", "weightedAmount": "amount", "startTimestamp": "uint", "expirationTimestamp": "uint", "address": "address", "id": "hash"},
		rowKeys: map[string]int{"address": 0}, rowID: "id"},
	"embedded.token.getByZts": {fields: map[string]string{"name": "string", "symbol": "string", "domain": "string", "totalSupply": "amount", "decimals": "uint",
		"owner": "address", "tokenStandard": "zts", "maxSupply": "amount"}, keys: map[string]int{"tokenStandard": 0}, pageIndex: -1},
}

var uintShaped = regexp.MustCompile(`^[0-9]+$`)

// kindOK reports whether a member's raw JSON is of the kind.
func kindOK(kind string, raw json.RawMessage) bool {
	var str string
	asString := json.Unmarshal(raw, &str) == nil
	switch kind {
	case "uint":
		return uintShaped.MatchString(strings.TrimSpace(string(raw)))
	case "amount":
		return amountShaped.MatchString(strings.TrimSpace(string(raw)))
	case "digits":
		return asString && uintShaped.MatchString(str)
	case "bool":
		t := strings.TrimSpace(string(raw))
		return t == "true" || t == "false"
	case "string":
		return asString && strings.HasPrefix(strings.TrimSpace(string(raw)), `"`)
	case "pairs":
		var l []json.RawMessage
		if json.Unmarshal(raw, &l) != nil || !strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
			return false
		}
		for _, e := range l {
			if membersOf(e, pairFields) != "" {
				return false
			}
		}
		return true
	case "list":
		var l []json.RawMessage
		return json.Unmarshal(raw, &l) == nil && strings.HasPrefix(strings.TrimSpace(string(raw)), "[")
	case "hash":
		_, err := types.HexToHash(str)
		return asString && err == nil
	case "address":
		_, err := types.ParseAddress(str)
		return asString && err == nil
	case "zts":
		_, err := types.ParseZTS(str)
		return asString && err == nil
	}
	return false
}

// membersOf says what is wrong with raw as an object with exactly these
// members of these kinds, or "".
func membersOf(raw json.RawMessage, fields map[string]string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || !strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		return fmt.Sprintf("want an object, got %.100s", raw)
	}
	for f, kind := range fields {
		v, ok := m[f]
		if !ok || !kindOK(kind, v) {
			return fmt.Sprintf("member %q missing or not a %s: %.100s", f, kind, v)
		}
	}
	return ""
}

// stateOf says what is wrong with a probe's answer as the getter's own
// answer about what was asked, or "" if nothing is.
func stateOf(p probe, raw json.RawMessage) string {
	spec, known := stateSpecs[p.method]
	if !known {
		return "this getter has no entry in stateSpecs, so its answer cannot be recognised"
	}
	if spec.fields == nil {
		if !amountShaped.MatchString(strings.TrimSpace(string(raw))) {
			return "want an amount"
		}
		return ""
	}
	if why := membersOf(raw, spec.fields); why != "" {
		return why
	}
	var m map[string]json.RawMessage
	json.Unmarshal(raw, &m)
	for f, arg := range spec.keys {
		if arg >= len(p.args) {
			return fmt.Sprintf("the probe has no argument %d to hold %q against", arg, f)
		}
		want, err := json.Marshal(p.args[arg])
		if err != nil {
			return err.Error()
		}
		var got, asked interface{}
		json.Unmarshal(m[f], &got)
		json.Unmarshal(want, &asked)
		if fmt.Sprint(got) != fmt.Sprint(asked) {
			return fmt.Sprintf("asked about %s %v, the answer is about %v", f, asked, got)
		}
	}
	if spec.pageIndex >= 0 {
		if spec.pageSize >= len(p.args) {
			return "the probe has no page index and size"
		}
		var index, size, count int
		var list []json.RawMessage
		fmt.Sscan(fmt.Sprint(p.args[spec.pageIndex]), &index)
		fmt.Sscan(fmt.Sprint(p.args[spec.pageSize]), &size)
		if err := json.Unmarshal(m["count"], &count); err != nil || count < 0 {
			return fmt.Sprintf("count %s is not a count", m["count"])
		}
		json.Unmarshal(m["list"], &list)
		want := count - index*size
		if want > size {
			want = size
		}
		if want < 0 {
			want = 0
		}
		if len(list) != want {
			return fmt.Sprintf("page %d of size %d with count %d must hold %d entries, it holds %d", index, size, count, want, len(list))
		}
		ids := map[string]bool{}
		for _, e := range list {
			if spec.rows == nil {
				return "this getter's page entries have no entry in stateSpecs"
			}
			if why := membersOf(e, spec.rows); why != "" {
				return "page entry: " + why
			}
			var o map[string]json.RawMessage
			json.Unmarshal(e, &o)
			for f, arg := range spec.rowKeys {
				want, _ := json.Marshal(p.args[arg])
				var got, asked interface{}
				json.Unmarshal(o[f], &got)
				json.Unmarshal(want, &asked)
				if fmt.Sprint(got) != fmt.Sprint(asked) {
					return fmt.Sprintf("a page entry has %s %v, the probe asked about %v", f, got, asked)
				}
			}
			if len(spec.rowIDs) != 0 {
				key := ""
				for _, f := range spec.rowIDs {
					key += string(o[f]) + "/"
				}
				if ids[key] {
					return fmt.Sprintf("page entries share %v %s", spec.rowIDs, key)
				}
				ids[key] = true
			}
			if spec.rowID != "" {
				if ids[string(o[spec.rowID])] {
					return fmt.Sprintf("page entries share %s %s", spec.rowID, o[spec.rowID])
				}
				ids[string(o[spec.rowID])] = true
			}
		}
	}
	return ""
}

// callFail sends a call that the contract must refuse while executing, and
// checks what a refusal has to leave behind: a failed receive paired with
// the send (mustFail), the caller's ZNN and QSR as they were when the call
// carried no amount, and every watched getter answering exactly as before.
// The reason for the refusal is not on chain; that part belongs to the VM
// unit tests.
func callFail(t *testing.T, kp *wallet.KeyPair, to types.Address, data []byte, zts types.ZenonTokenStandard, amount *big.Int, probes ...probe) *Result {
	t.Helper()
	before := snapshot(t, probes)
	znnBefore, qsrBefore := bal(t, kp.Address, znn), bal(t, kp.Address, qsr)
	res, err := call(kp, to, data, zts, amount)
	r := mustFail(t, res, err)
	if amount == nil || amount.Sign() == 0 {
		wantBig(t, "ZNN of the caller after a refused call", bal(t, kp.Address, znn), znnBefore)
		wantBig(t, "QSR of the caller after a refused call", bal(t, kp.Address, qsr), qsrBefore)
	}
	for i, after := range snapshot(t, probes) {
		if after != before[i] {
			t.Errorf("a refused call changed %s%v:\n before: %.400s\n after:  %.400s", probes[i].method, probes[i].args, before[i], after)
		}
	}
	return r
}

func callReject(t *testing.T, want string, kp *wallet.KeyPair, to types.Address, data []byte, zts types.ZenonTokenStandard, amount *big.Int) {
	t.Helper()
	_, err := net.Send(kp, &nom.AccountBlock{BlockType: nom.BlockTypeUserSend, ToAddress: to, Data: data, TokenStandard: zts, Amount: amount}, &Mutator{})
	mustReject(t, err, want)
}

// mustOK requires that the call was accepted and the contract executed it.
func mustOK(t *testing.T, res *Result, err error) *Result {
	t.Helper()
	if err != nil {
		t.Fatalf("call refused: %v", err)
	}
	if !res.OK() {
		t.Fatalf("contract returned status %d for send %s, want success", res.Status, res.Send.Hash)
	}
	return res
}

// mustReject requires that the node itself refused the block when it was
// published, with a message containing want. An error from preparing the
// block in the driver does not count, and neither does an empty expectation.
func mustReject(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		t.Fatalf("the case does not say which refusal it expects")
	}
	if err == nil {
		t.Fatalf("block was accepted, want refusal containing %q", want)
	}
	var stage *StageError
	if !errors.As(err, &stage) || stage.Stage != "publish" {
		t.Fatalf("the block never reached the node (%v), want the node to refuse it with %q", err, want)
	}
	if !strings.Contains(stage.Err.Error(), want) {
		t.Fatalf("refused with %q, want a message containing %q", stage.Err.Error(), want)
	}
}

// mustFail requires that the block was accepted and the contract then
// refused it: a receive block with the fail status, and the amount that came
// with the call on its way back to the sender as the one descendant block.
// A refused call that carried nothing must produce no descendant at all.
func mustFail(t *testing.T, res *Result, err error) *Result {
	t.Helper()
	if err != nil {
		t.Fatalf("call refused at publish (%v), want a failed contract receive", err)
	}
	if res.Status != StatusFail {
		t.Fatalf("contract returned status %d for send %s, want fail (%d)", res.Status, res.Send.Hash, StatusFail)
	}
	d := res.Receive.DescendantBlocks
	if res.Send.Amount == nil || res.Send.Amount.Sign() == 0 {
		if len(d) != 0 {
			t.Fatalf("a refused call without an amount produced %d descendant blocks, want none (send %s)", len(d), res.Send.Hash)
		}
		return res
	}
	if len(d) != 1 {
		t.Fatalf("a refused call with an amount produced %d descendant blocks, want exactly the refund (send %s)", len(d), res.Send.Hash)
	}
	if d[0].BlockType != nom.BlockTypeContractSend || d[0].Address != res.Send.ToAddress || d[0].ToAddress != res.Send.Address ||
		d[0].TokenStandard != res.Send.TokenStandard || d[0].Amount.Cmp(res.Send.Amount) != 0 {
		t.Fatalf("refund block: type %d from %s to %s, %s of %s; want a contract send from %s to %s of %s %s", d[0].BlockType, d[0].Address, d[0].ToAddress,
			d[0].Amount, d[0].TokenStandard, res.Send.ToAddress, res.Send.Address, res.Send.Amount, res.Send.TokenStandard)
	}
	// The refund is not only listed under the receive: it is a block of its
	// own on every endpoint, with the same content, for the caller to receive.
	for i, url := range net.URLs {
		blk, err := net.Block(i, d[0].Hash)
		if err != nil || blk == nil {
			t.Fatalf("%s does not have the refund block %s of send %s (error: %v)", url, d[0].Hash, res.Send.Hash, err)
		}
		if blk.Hash != d[0].Hash || blk.BlockType != nom.BlockTypeContractSend || blk.Address != res.Send.ToAddress || blk.ToAddress != res.Send.Address ||
			blk.TokenStandard != res.Send.TokenStandard || blk.Amount == nil || blk.Amount.Cmp(res.Send.Amount) != 0 {
			t.Fatalf("%s: refund block %s reads back as block %s, type %d, from %s to %s, %s of %s; want a contract send from %s to %s of %s %s", url, d[0].Hash,
				blk.Hash, blk.BlockType, blk.Address, blk.ToAddress, blk.Amount, blk.TokenStandard, res.Send.ToAddress, res.Send.Address, res.Send.Amount, res.Send.TokenStandard)
		}
	}
	return res
}

// count reads a count field of a decoded reply; a missing or non-numeric
// field fails the case instead of comparing unequal to something.
func count(t *testing.T, reply obj, field string) int {
	t.Helper()
	f, ok := reply[field].(float64)
	if !ok {
		t.Fatalf("reply has no numeric %q: %v", field, reply)
	}
	return int(f)
}

// transfer sends tokens to a user address and returns the published block.
func transfer(t *testing.T, from *wallet.KeyPair, to types.Address, zts types.ZenonTokenStandard, amount *big.Int, data []byte) *nom.AccountBlock {
	t.Helper()
	b, err := net.Send(from, &nom.AccountBlock{BlockType: nom.BlockTypeUserSend, ToAddress: to, TokenStandard: zts, Amount: amount, Data: data}, nil)
	if err != nil {
		t.Fatalf("send from %s: %v", from.Address, err)
	}
	return b
}

// tryTransfer publishes a send built with a mutator and returns the error.
func tryTransfer(from *wallet.KeyPair, to types.Address, zts types.ZenonTokenStandard, amount *big.Int, mut *Mutator) error {
	_, err := net.Send(from, &nom.AccountBlock{BlockType: nom.BlockTypeUserSend, ToAddress: to, TokenStandard: zts, Amount: amount}, mut)
	return err
}

// receiveAll receives at least want pending sends and fails the case
// otherwise.
func receiveAll(t *testing.T, kp *wallet.KeyPair, want int) {
	t.Helper()
	if _, err := net.ReceiveAll(kp, want, 2*wait); err != nil {
		t.Fatalf("receive for %s: %v", kp.Address, err)
	}
}

// newAccount returns a fresh account of this run. The funder sends it the
// given whole-coin amounts and fuses fuseQsr QSR for it; with fuseQsr 0 the
// account has no plasma and pays for its blocks with proof of work.
func newAccount(t *testing.T, slot uint32, znnCoins, qsrCoins, fuseQsr int64) *wallet.KeyPair {
	t.Helper()
	kp := net.Key(runBase + slot)
	funder := net.Key(IdxFunder)
	pending := 0
	if fuseQsr > 0 {
		callOK(t, funder, types.PlasmaContract, definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, kp.Address), qsr, coins(fuseQsr))
	}
	if znnCoins > 0 {
		transfer(t, funder, kp.Address, znn, coins(znnCoins), nil)
		pending++
	}
	if qsrCoins > 0 {
		transfer(t, funder, kp.Address, qsr, coins(qsrCoins), nil)
		pending++
	}
	if pending > 0 {
		receiveAll(t, kp, pending)
	}
	return kp
}

// refusedGone says what is wrong with the refused blocks at the end of a
// run, or "" if every one is absent from every node.
func refusedGone() string {
	committed := func() string {
		for _, h := range net.Refused() {
			for i, url := range net.URLs {
				if blk, err := net.Block(i, h); err != nil || blk != nil {
					return fmt.Sprintf("%s knows refused block %s at the end of the run (error: %v)", url, h, err)
				}
			}
			if have, err := net.KnownOnContainers(h); err != nil || len(have) != 0 {
				return fmt.Sprintf("refused block %s at the end of the run: known on %v (error: %v)", h, have, err)
			}
		}
		return ""
	}
	for try := 0; try < 5; try++ {
		if why := committed(); why != "" {
			return why
		}
		stable := true
		accounts := map[types.Address]bool{}
		for _, h := range net.Refused() {
			accounts[net.RefusedBy(h)] = true
		}
		for addr := range accounts {
			// Every hash returned is inspected, even from a reading that
			// failed or is not a whole snapshot.
			pools, ok, err := net.Unconfirmed(addr)
			stable = stable && ok
			for node, hashes := range pools {
				for _, x := range hashes {
					for _, h := range net.Refused() {
						if x == h {
							return fmt.Sprintf("%s holds refused block %s unconfirmed at the end of the run", node, h)
						}
					}
				}
			}
			if err != nil {
				return err.Error()
			}
		}
		if why := committed(); why != "" {
			return why
		}
		if stable {
			return ""
		}
		time.Sleep(2 * time.Second)
	}
	return "a refused block's account holds more unconfirmed blocks than one RPC page; whether a node still holds a refused block is not established"
}

// openRunLock opens the run lock file read-only, creating it if it is
// absent. An existing file is opened without O_CREATE: with Linux's
// fs.protected_regular, O_CREATE on a file another user owns in sticky /tmp
// is refused even when the file could be read. A new file is created with
// O_EXCL (a run that loses that race opens the winner's file), given mode
// 0644 whatever the umask, and must be a regular file.
func openRunLock(path string) (*os.File, error) {
	for attempt := 0; attempt < 3; attempt++ {
		f, err := os.Open(path)
		if errors.Is(err, fs.ErrNotExist) {
			f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDONLY, 0o644)
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			if err == nil {
				if err = f.Chmod(0o644); err != nil {
					f.Close()
					return nil, err
				}
			}
		}
		if err != nil {
			return nil, err
		}
		if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
			f.Close()
			return nil, fmt.Errorf("%s is not a regular file (%v)", path, err)
		}
		return f, nil
	}
	return nil, fmt.Errorf("%s: created and removed by another run repeatedly", path)
}
