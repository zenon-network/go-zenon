//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
)

// TestSporkStagedEnforcement covers TEST-PLAN.md S-1, S-2 and S-5 on the
// staged genesis (edge class E7) for the five sporks that arrive while the
// network runs: Libp2pSpork (20), DynamicPlasmaSpork (30), PtlcSpork (60),
// EmbeddedRefundSpork (70) and EpochRewardTermsSpork (80).
//
//   - S-1: before its enforcement height a spork is scheduled but not in
//     force. For Dynamic Plasma this is pinned where consensus sees it:
//     momentums carry Version 1 and zero NextFusionPrice/NextWorkPrice,
//     both of which the momentum verifier requires pre-spork
//     (verifier/momentum.go) and forbids/requires respectively once the
//     spork is in force.
//   - S-2: at the enforcement height every one of the five nodes switched
//     together: all five return the same momentum hash one below, at and
//     one above the height (no fork), every container is still running and
//     none has ever restarted (no node exited). For Dynamic Plasma the
//     version switch lands one momentum late — a producer builds momentum
//     N on its parent N-1's spork state (pillar/worker_momentum.go) — so
//     the momentum at the height is still Version 1 with zero prices and
//     the first Version 2 momentum, one past it, carries both resource
//     prices at or above dp.MinResourcePrice.
//   - S-5: after every staged spork is in force, restarting all five nodes
//     preserves the spork table exactly (ids, activation, enforcement
//     heights) and the chain continues on the same data.
//
// The gated *send* behaviour of EmbeddedRefundSpork (the stalled refund
// queue of issue #271) needs a failing contract-to-contract send whose only
// live trigger is a bridge wrap of a non-burnable Owned token; the bridge
// administrator delays are epoch-scale on this genesis, so that behaviour
// is covered by the unit tests in vm/embedded/tests/embedded_refund_test.go
// and the phase-27 lab rehearsal instead of here. This case covers what the
// staged network itself can show: the boundary mechanics, above.
//
// Run it positioned before the first staged height, from the repo root:
//
//	E2E_STAGED=1 E2E_NODES=pillar,pillar2,pillar3,rpc,observer \
//	E2E_GENESIS="$PWD/test/devnet/genesis/staged.json" E2E_MIN_HEIGHT=12 \
//	go test -tags e2e ./test/e2e/ -run TestSporkStagedEnforcement -timeout 20m
//
// E2E_STAGED=1 is the opt-in: the S-5 case restarts all five containers, so
// an unfiltered run on a staged devnet must not pick the test up by itself.
// The nodes are read through docker exec (E2E_NODES, like
// TestHaltOnUnknownSpork), so no host RPC ports beyond the primary endpoint
// are needed; the containers are controlled through the docker CLI.
// readSporks returns the enforcement height of every activated spork this
// binary implements, and the highest of them, as the node reports it over
// RPC. (The framework reads the same list from the genesis file at start;
// the restart-survival case compares the node's own answer before and
// after.)
type sporkCaller func(result interface{}, method string, args ...interface{}) error

func readSporksFrom(call sporkCaller) (map[string]uint64, uint64, error) {
	var list struct {
		List []struct {
			Id                types.Hash `json:"id"`
			Activated         bool       `json:"activated"`
			EnforcementHeight uint64     `json:"enforcementHeight"`
		} `json:"list"`
	}
	if err := call(&list, "embedded.spork.getAll", 0, 50); err != nil {
		return nil, 0, err
	}
	out, last := map[string]uint64{}, uint64(0)
	for _, s := range list.List {
		name, known := sporkNames[s.Id]
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

func readSporks() (map[string]uint64, uint64, error) {
	return readSporksFrom(net.Call)
}

// containerCaller reads one devnet container's RPC through docker exec, so
// the nodes that publish no host port answer too (net.Containers comes from
// E2E_NODES). Every request is bounded by ctx.
func containerCaller(ctx context.Context, container string) sporkCaller {
	return func(result interface{}, method string, args ...interface{}) error {
		params, err := json.Marshal(args)
		if err != nil {
			return err
		}
		var envelope struct {
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := dockerRPCParams(ctx, container, method, string(params), &envelope); err != nil {
			return err
		}
		// An RPC-level error carries no result: without this check the
		// unmarshal below fails with "unexpected end of JSON input" and the
		// node's actual refusal is lost.
		if envelope.Error != nil {
			return fmt.Errorf("%s on %s: RPC error %d: %s", method, container, envelope.Error.Code, envelope.Error.Message)
		}
		if len(envelope.Result) == 0 {
			return fmt.Errorf("%s on %s: reply has neither result nor error", method, container)
		}
		return json.Unmarshal(envelope.Result, result)
	}
}

func TestSporkStagedEnforcement(t *testing.T) {
	if os.Getenv("E2E_STAGED") != "1" {
		t.Skip("S-5 restarts all five devnet containers; set E2E_STAGED=1 to opt in")
	}
	if os.Getenv("E2E_DESTRUCTIVE") == "1" {
		t.Skip("skipped in a destructive run")
	}
	if fast {
		t.Skip("the fast variant enforces every spork from height 0")
	}
	// The staged genesis: staggered heights that distinguish it from
	// stock/all-on (both put everything new at 0).
	want := map[string]uint64{
		"libp2p": 20, "dynamic-plasma": 30, "ptlc": 60,
		"embedded-refund": 70, "epoch-reward-terms": 80,
	}
	for name, h := range want {
		if sporkHeights[name] != h {
			t.Skipf("needs the staged genesis: spork %q enforced at %d, staged has %d", name, sporkHeights[name], h)
		}
	}
	if len(net.Containers) == 0 {
		t.Fatal("E2E_NODES is not set: the boundary and restart checks read every node through docker exec")
	}
	// S-1 and the Dynamic Plasma format assertions need the run positioned
	// before height 30 (set E2E_MIN_HEIGHT=12); the boundary agreement
	// checks read history and the restart works at any height.
	containers := net.Containers

	docker := func(args ...string) (string, error) {
		out, err := exec.Command("docker", args...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	// noExitYet fails when a container is not running or has ever
	// restarted: across an enforcement height a node that exited (e.g. on
	// an unknown spork, the S-4 case) would show here. The calling subtest's
	// t is passed in so failures and coverage land on the case, not on the
	// outer test.
	noExitYet := func(t *testing.T, what string) {
		t.Helper()
		args := append([]string{"inspect", "-f", "{{.Name}} {{.State.Status}} {{.RestartCount}}"}, containers...)
		out, err := docker(args...)
		if err != nil {
			t.Fatalf("docker inspect: %v (%s)", err, out)
		}
		for _, line := range strings.Split(out, "\n") {
			var name, status string
			var restarts int
			if _, err := fmt.Sscanf(line, "/%s %s %d", &name, &status, &restarts); err != nil || status != "running" || restarts != 0 {
				t.Fatalf("%s: container %q is %s with %d restarts (%q): a node exited", what, name, status, restarts, line)
			}
		}
	}

	// momentum is one momentum at exactly the given height from the primary
	// endpoint. The calling subtest's t is passed in so the lookup's
	// coverage lands on the case, not on the outer test.
	momentum := func(t *testing.T, height uint64) struct {
		Hash            types.Hash `json:"hash"`
		Height          uint64     `json:"height"`
		Version         uint64     `json:"version"`
		NextFusionPrice uint64     `json:"nextFusionPrice"`
		NextWorkPrice   uint64     `json:"nextWorkPrice"`
	} {
		t.Helper()
		var list struct {
			List []struct {
				Hash            types.Hash `json:"hash"`
				Height          uint64     `json:"height"`
				Version         uint64     `json:"version"`
				NextFusionPrice uint64     `json:"nextFusionPrice"`
				NextWorkPrice   uint64     `json:"nextWorkPrice"`
			} `json:"list"`
		}
		get(t, &list, "ledger.getMomentumsByHeight", height, 1)
		if len(list.List) != 1 || list.List[0].Height != height {
			t.Fatalf("momentum at height %d: got %+v, want exactly that height", height, list.List)
		}
		return list.List[0]
	}

	// agree polls until every node answers and returns the single momentum
	// hash they all share at the height; a node on a different branch (or
	// dead) fails the deadline. Every node is read through docker exec
	// (net.Containers), each request bounded by the polling deadline.
	agree := func(t *testing.T, height uint64) types.Hash {
		t.Helper()
		deadline := time.Now().Add(wait)
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		defer cancel()
		var ref types.Hash
		for {
			all := true
			for i, c := range containers {
				var m struct {
					List []struct {
						Hash   types.Hash `json:"hash"`
						Height uint64     `json:"height"`
					} `json:"list"`
				}
				if err := containerCaller(ctx, c)(&m, "ledger.getMomentumsByHeight", height, 1); err != nil || len(m.List) != 1 || m.List[0].Height != height {
					all = false
					break
				}
				if i == 0 {
					ref = m.List[0].Hash
				} else if m.List[0].Hash != ref {
					t.Fatalf("node %s disagrees at height %d: %s vs %s", c, height, m.List[0].Hash, ref)
				}
			}
			if all {
				return ref
			}
			if time.Now().After(deadline) {
				t.Fatalf("not all of %v answered height %d within %v", containers, height, wait)
			}
			time.Sleep(2 * time.Second)
		}
	}

	t.Run("S-1_before_enforcement_gated_rules_off", func(t *testing.T) {
		f, err := net.Frontier()
		if err != nil {
			t.Fatal(err)
		}
		if f.Height >= want["dynamic-plasma"] {
			t.Skipf("run positioned before height %d: frontier is %d (set E2E_MIN_HEIGHT=12)", want["dynamic-plasma"], f.Height)
		}
		// Cover only the sporks whose height has not passed yet; when the
		// driver starts just below the libp2p height, libp2p itself is
		// checked by the S-2 subtest instead.
		var ahead []string
		for name, h := range want {
			if f.Height >= h {
				continue
			}
			ahead = append(ahead, sporkItem(t, name))
			if live[name] {
				t.Fatalf("harness recorded %s as live at frontier %d below its enforcement height %d", name, f.Height, h)
			}
		}
		cover(t, "E7", "spork", ahead...)
		// Dynamic Plasma's consensus-visible gate pre-spork: version 1 and
		// zero resource prices (verifier/momentum.go requires both).
		for _, h := range []uint64{f.Height, f.Height - 1} {
			m := momentum(t, h)
			if m.Version != 1 {
				t.Errorf("momentum %d: version %d, want 1 before Dynamic Plasma", h, m.Version)
			}
			if m.NextFusionPrice != 0 || m.NextWorkPrice != 0 {
				t.Errorf("momentum %d: prices %d/%d, want 0/0 before Dynamic Plasma", h, m.NextFusionPrice, m.NextWorkPrice)
			}
		}
		noExitYet(t, "before any staged enforcement")
	})

	// boundary waits for the chain to pass onePast and then checks that all
	// five nodes share one momentum hash at H-1, H and H+1. The jumps
	// between staged heights are minutes of wall time (momentums are 10 s
	// apart), far beyond the block-confirmation `wait`.
	boundary := func(t *testing.T, name string, H uint64, onePast uint64) {
		t.Helper()
		if err := net.WaitHeight(onePast, 10*time.Minute); err != nil {
			t.Fatal(err)
		}
		for _, h := range []uint64{H - 1, H, H + 1} {
			got := agree(t, h)
			if canonical := momentum(t, h).Hash; got != canonical {
				t.Errorf("node hashes at %d: %s, primary says %s", h, got, canonical)
			}
		}
		noExitYet(t, fmt.Sprintf("across %s enforcement at %d", name, H))
	}

	t.Run("S-2_libp2p_at_enforcement", func(t *testing.T) {
		cover(t, "E7", "spork", "Libp2pSpork")
		// The transport swap leaves the momentum format untouched; the
		// observable gate is that every node stays on one chain through it.
		boundary(t, "libp2p", want["libp2p"], want["libp2p"]+2)
	})

	t.Run("S-2_dynamic_plasma_at_enforcement", func(t *testing.T) {
		cover(t, "E7", "spork", "DynamicPlasmaSpork")
		H := want["dynamic-plasma"]
		boundary(t, "dynamic-plasma", H, H+2)
		// E7 semantic for Dynamic Plasma: momentum H is still produced by
		// the pre-spork rules — a producer builds momentum N on its parent
		// N-1's spork state (pillar/worker_momentum.go), so the version
		// switch lands on H+1, not H. One below and at H: version 1 with
		// zero prices (which the momentum verifier requires pre-spork);
		// H+1 and above: version 2 with both resource prices at the floor.
		for _, h := range []uint64{H - 1, H} {
			m := momentum(t, h)
			if m.Version != 1 || m.NextFusionPrice != 0 || m.NextWorkPrice != 0 {
				t.Errorf("momentum %d: version %d prices %d/%d, want v1 0/0 before the format switch", h, m.Version, m.NextFusionPrice, m.NextWorkPrice)
			}
		}
		for _, h := range []uint64{H + 1, H + 2} {
			m := momentum(t, h)
			if m.Version != 2 {
				t.Errorf("momentum %d: version %d, want 2 from one past Dynamic Plasma", h, m.Version)
			}
			if m.NextFusionPrice < dp.MinResourcePrice || m.NextWorkPrice < dp.MinResourcePrice {
				t.Errorf("momentum %d: prices %d/%d below the floor %d", h, m.NextFusionPrice, m.NextWorkPrice, dp.MinResourcePrice)
			}
		}
	})

	t.Run("S-2_ptlc_and_embedded_refund_at_enforcement", func(t *testing.T) {
		cover(t, "E7", "spork", "PtlcSpork", "EmbeddedRefundSpork")
		boundary(t, "ptlc", want["ptlc"], want["ptlc"]+2)
		boundary(t, "embedded-refund", want["embedded-refund"], want["embedded-refund"]+2)
	})

	t.Run("S-2_epoch_reward_terms_at_enforcement", func(t *testing.T) {
		cover(t, "E7", "spork", "EpochRewardTermsSpork")
		boundary(t, "epoch-reward-terms", want["epoch-reward-terms"], want["epoch-reward-terms"]+2)
	})

	t.Run("S-5_restart_after_activation", func(t *testing.T) {
		cover(t, "E7", "spork", "Libp2pSpork", "DynamicPlasmaSpork", "PtlcSpork", "EmbeddedRefundSpork", "EpochRewardTermsSpork")
		before, _, err := readSporks()
		if err != nil {
			t.Fatal(err)
		}
		f, err := net.Frontier()
		if err != nil {
			t.Fatal(err)
		}
		out, err := docker(append([]string{"restart"}, containers...)...)
		if err != nil {
			t.Fatalf("docker restart: %v (%s)", err, out)
		}
		t.Logf("restarted %v; waiting for the chain to continue", containers)
		if err := net.WaitHeight(f.Height+2, wait); err != nil {
			t.Fatal(err)
		}
		// Every node came back, not only the primary endpoint: all five
		// serve the same new tip (two of three pillars can still produce
		// with one node down, so the primary alone would not notice), and
		// every container is running with no restart-policy restart.
		agree(t, f.Height+2)
		noExitYet(t, "after the S-5 restart")
		// The spork table survived on every node, read through docker exec.
		ctx, cancel := context.WithTimeout(context.Background(), wait)
		defer cancel()
		for _, c := range containers {
			after, _, err := readSporksFrom(containerCaller(ctx, c))
			if err != nil {
				t.Errorf("spork table of %s after the restart: %v", c, err)
				continue
			}
			for name, h := range before {
				if after[name] != h {
					t.Errorf("%s: spork %s enforcement height after restart: %d, want %d", c, name, after[name], h)
				}
			}
			if len(after) != len(before) {
				t.Errorf("%s: spork table changed across the restart: %v -> %v", c, before, after)
			}
		}
		// Same data, no resync: the pre-restart frontier is still
		// canonical and the Dynamic Plasma format persisted (the switch
		// lands on H+1 — see the S-2 dynamic-plasma subtest).
		if m := momentum(t, f.Height); m.Hash != f.Hash {
			t.Errorf("momentum %d changed across the restart: %s, want %s", f.Height, m.Hash, f.Hash)
		}
		if m := momentum(t, want["dynamic-plasma"]+1); m.Version != 2 {
			t.Errorf("momentum %d after restart: version %d, want 2", want["dynamic-plasma"]+1, m.Version)
		}
	})
}
