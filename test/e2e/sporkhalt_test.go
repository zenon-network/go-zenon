//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSporkUnknownBinary covers TEST-PLAN.md S-4 and X-4 on the staged
// genesis (edge class E7): a node whose binary lacks EmbeddedRefundSpork in
// types.ImplementedSporksMap cannot pass the spork's enforcement height,
// while every node that has the spork continues; and a node restarted with
// a binary that has the spork continues on its existing data directory
// without a resync.
//
// What "cannot pass" means on this tree differs from the released v0.0.10
// behavior and the TEST-PLAN wording ("exits with status 2"), and the case
// pins the difference:
//
//   - live node (in sync or syncing): the momentum pool refuses the
//     enforcement-height momentum, rolls it back and the node stays
//     running one below the height (chain/momentum_pool.go). Released
//     v0.0.10 called os.Exit(2) on the same condition (chain/
//     momentum_pool.go at c3e0e88), measured in an upgrade-day rehearsal
//     of the official binary on a devnet.
//   - restart with a data directory whose frontier is already at or past
//     an enforced spork the binary lacks: chain.Init refuses to start the
//     node, the start error reaches cli.go and the process exits with
//     status 1. On v0.0.10 the same restart exited 2 from chain.Init.
//
// The container state is a property of the devnet process, not of the RPC
// surface, so this case observes it through the docker CLI. It needs:
//
//   - the staged genesis (embedded-refund enforced at 70; ptlc at 60 and
//     epoch-reward-terms at 80 distinguish it from stock/all-on), and
//
//   - a sixth container "znnd-devnet-oldnode" running a binary with the
//     EmbeddedRefundSpork entry removed from ImplementedSporksMap (built
//     from the same commit otherwise), started with restart "no" so its
//     state stays inspectable. Everything needed is in the repo:
//
//     OLDNODE=1 test/devnet/build-image.sh        # the noref image
//     docker compose -f docker-compose.yml -f test/devnet/compose.variant.yml \
//     -f test/devnet/compose.oldnode.yml up -d oldnode
//
//     The overlay mounts test/devnet/oldnode/syncnode/config.json over the
//     syncnode role config: it sets "P2PBackend": "libp2p", because a
//     stock-config late joiner cannot sync past the libp2p swap spork
//     (zenon-network/go-zenon issue #105) and the oldnode always joins
//     after that height (20 on staged).
//
// Nothing here touches the five standard nodes: the case is safe to run
// alongside the normal groups on a staged devnet. Set E2E_OLDNODE=1 to
// enable; E2E_DEVNET_GENESIS must point at the staged genesis file (the
// same DEVNET_GENESIS test/devnet/up.sh exports).
func TestSporkUnknownBinary(t *testing.T) {
	if os.Getenv("E2E_OLDNODE") != "1" {
		t.Skip("needs the oldnode container and docker access; set E2E_OLDNODE=1")
	}
	if os.Getenv("E2E_DESTRUCTIVE") == "1" {
		t.Skip("skipped in a destructive run")
	}
	if fast {
		t.Skip("the staged variant is a stock-constants genesis")
	}
	enf, ok := sporkHeights["embedded-refund"]
	if !ok || enf == 0 || sporkHeights["ptlc"] == 0 || sporkHeights["epoch-reward-terms"] == 0 {
		t.Skip("needs the staged genesis: embedded-refund, ptlc and epoch-reward-terms at staggered heights")
	}
	const oldnode = "znnd-devnet-oldnode"
	network := envOr("E2E_OLDNET", "znnd-devnet")
	image := envOr("E2E_DEVNET_IMAGE", "go-zenon-devnet:latest")
	volume := envOr("E2E_OLDNODE_VOLUME", "znnd-oldnode-data")
	rpc := envOr("E2E_OLDNODE_RPC", "http://localhost:36996")
	genesis := os.Getenv("E2E_DEVNET_GENESIS")
	if genesis == "" {
		t.Fatal("E2E_DEVNET_GENESIS must point at the staged genesis file (the DEVNET_GENESIS test/devnet/up.sh exports)")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	oldCfg := filepath.Join(root, "test", "devnet", "oldnode", "syncnode", "config.json")
	docker := func(args ...string) (string, error) {
		out, err := exec.Command("docker", args...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	state := func(name string) (status string, exitCode int, err error) {
		out, err := docker("inspect", "-f", "{{.State.Status}} {{.State.ExitCode}}", name)
		if err != nil {
			return "", 0, fmt.Errorf("docker inspect %s: %v (%s)", name, err, out)
		}
		f := strings.Fields(out)
		if len(f) != 2 {
			return "", 0, fmt.Errorf("unexpected inspect output %q", out)
		}
		if _, err := fmt.Sscanf(f[1], "%d", &exitCode); err != nil {
			return "", 0, fmt.Errorf("exit code in %q: %v", out, err)
		}
		return f[0], exitCode, nil
	}
	frontierAt := func() (uint64, error) {
		body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ledger.getFrontierMomentum","params":[]}`)
		resp, err := http.Post(rpc, "application/json", body)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		var m struct {
			Result struct {
				Height uint64 `json:"height"`
			} `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
			return 0, err
		}
		return m.Result.Height, nil
	}
	waitFrontier := func(atLeast uint64, what string) uint64 {
		t.Helper()
		deadline := time.Now().Add(wait)
		for {
			got, err := frontierAt()
			if err == nil && got >= atLeast {
				return got
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: frontier %d (err %v), want >= %d within %v", what, got, err, atLeast, wait)
			}
			time.Sleep(5 * time.Second)
		}
	}
	// run starts a container on the oldnode data volume with the given
	// image and waits for its RPC to answer. The staged genesis and the
	// libp2p syncnode config override are mounted exactly as the oldnode
	// compose service mounts them (test/devnet/compose.oldnode.yml), so a
	// first boot seeds the same chain as the five standard nodes.
	run := func(name, img string) uint64 {
		t.Helper()
		out, err := docker("run", "-d", "--name", name, "--network", network,
			"-v", volume+":/root/.znn",
			"-v", genesis+":/devnet/genesis.json:ro",
			"-v", oldCfg+":/devnet/syncnode/config.json:ro",
			"-e", "ZNND_ROLE=syncnode",
			"-p", "36996:35997", img)
		if err != nil {
			t.Fatalf("start %s (%s): %v (%s)", name, img, err, out)
		}
		return waitFrontier(1, "boot of "+name)
	}
	logsContain := func(name, needle string) bool {
		out, _ := docker("logs", name)
		return strings.Contains(out, needle)
	}

	// volumeAt records how far the oldnode data directory had synced
	// before the recovery swap; S-4b asserts the recovered node continues
	// from at least here instead of resyncing from genesis.
	var volumeAt uint64

	t.Run("S-4_old_node_cannot_pass_the_enforcement_height", func(t *testing.T) {
		cover(t, "E7", "spork", "EmbeddedRefundSpork")
		// The old node synced from genesis and refused the
		// enforcement-height momentum; it must sit one below the
		// enforcement height, still running. (On released v0.0.10 the
		// same condition exited 2; this tree rolls back and stays up —
		// see the group comment.)
		waitFrontier(enf-1, "old node syncing to one below the enforcement height")
		time.Sleep(30 * time.Second) // three producer slots: it must not advance
		got, err := frontierAt()
		if err != nil {
			t.Fatalf("old node frontier: %v", err)
		}
		if got != enf-1 {
			t.Errorf("old node is at %d, want it stopped at %d (one below the enforcement height)", got, enf-1)
		}
		status, _, err := state(oldnode)
		if err != nil || status != "running" {
			t.Errorf("old node state %q (%v), want running: this tree refuses the insert and stays up", status, err)
		}
		if !logsContain(oldnode, "Detected an unimplemented spork") {
			t.Error("old node logs do not carry the designed unimplemented-spork message")
		}
	})

	t.Run("X-4_nodes_with_the_spork_continued_past_it", func(t *testing.T) {
		// The five devnet nodes know the spork: the chain must have
		// produced well past the enforcement height while the old node
		// stopped. This is the designed split that makes the upgrade
		// deadline real.
		if err := net.WaitHeight(enf+5, wait); err != nil {
			t.Fatal(err)
		}
		f, err := net.Frontier()
		if err != nil {
			t.Fatal(err)
		}
		if f.Height <= enf {
			t.Errorf("network frontier is %d, want it continued past %d", f.Height, enf)
		}
	})

	t.Run("S-4c_binary_without_the_spork_cannot_start_on_a_past_enforcement_db", func(t *testing.T) {
		// Upgrade-day edge: a data directory that already passed the
		// enforcement height cannot boot a binary that lacks the spork.
		// Get the volume past the height with the stock image, then try
		// the old binary and record how it refuses. The height the volume
		// reached is recorded: S-4b asserts the recovered node continues
		// from at least here instead of resyncing from genesis.
		if out, err := docker("rm", "-f", oldnode); err != nil {
			t.Fatalf("remove the live old node: %v (%s)", err, out)
		}
		run("oldnode-sync-past", image)
		volumeAt = waitFrontier(enf+3, "stock binary syncing the oldnode volume past the enforcement height")
		if out, err := docker("rm", "-f", "oldnode-sync-past"); err != nil {
			t.Fatalf("remove the sync-past container: %v (%s)", err, out)
		}
		// The old binary on the same volume: chain.Init must refuse to
		// start. This tree exits 1 (cli.go on the start error); released
		// v0.0.10 exited 2 from chain.Init. Assert the designed refusal,
		// not a specific code, and log the code for the record. Started
		// detached and polled so a surprise start cannot hang the test.
		out, err := docker("run", "-d", "--name", oldnode, "--network", network,
			"-v", volume+":/root/.znn",
			"-v", genesis+":/devnet/genesis.json:ro",
			"-v", oldCfg+":/devnet/syncnode/config.json:ro",
			"-e", "ZNND_ROLE=syncnode",
			"-p", "36996:35997", envOr("E2E_OLDNODE_IMAGE", "go-zenon-devnet-noref:latest"))
		if err != nil {
			t.Fatalf("start the old binary on the past-enforcement volume: %v (%s)", err, out)
		}
		deadline := time.Now().Add(3 * time.Minute)
		var status string
		var code int
		for {
			status, code, err = state(oldnode)
			if err == nil && status == "exited" {
				break
			}
			if time.Now().After(deadline) {
				docker("rm", "-f", oldnode)
				t.Fatal("old binary did not exit within 3 min on a past-enforcement database; it apparently started — want a start refusal")
			}
			time.Sleep(2 * time.Second)
		}
		t.Logf("old binary on a past-enforcement db exited with code %d (dev exits 1 via cli.go; v0.0.10 exited 2 from chain.Init)", code)
		if !logsContain(oldnode, "unimplemented spork") {
			t.Error("old binary logs do not name the unimplemented spork")
		}
		if out, err := docker("rm", "-f", oldnode); err != nil {
			t.Fatalf("remove the refused container: %v (%s)", err, out)
		}
		t.Logf("oldnode volume recorded at height %d before the recovery swap", volumeAt)
	})

	t.Run("S-4b_recovery_binary_continues_without_resync", func(t *testing.T) {
		// Recovery per the upgrade runbook: stop (done above), swap to a
		// binary that implements the spork, start on the SAME data
		// directory. The chain continues without a resync: the first
		// frontier after boot must already be at or beyond the height the
		// volume had reached before the swap (a resync from genesis would
		// boot far below it), and the node then reaches the live frontier.
		first := run(oldnode, image)
		t.Cleanup(func() { docker("rm", "-f", oldnode) })
		if first < volumeAt {
			t.Errorf("recovered node first frontier is %d, below the recorded volume height %d — it resynced instead of continuing on the existing data", first, volumeAt)
		}
		want, err := net.Frontier()
		if err != nil {
			t.Fatal(err)
		}
		got := waitFrontier(want.Height, "recovered node catching up on its existing data")
		t.Logf("recovered node reached height %d (frontier at start %d, first boot frontier %d) on its existing data", got, want.Height, first)
	})
}
