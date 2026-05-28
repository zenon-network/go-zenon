//go:build testnet

package ptlc_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/rpc/api"
	rpcclient "github.com/zenon-network/go-zenon/rpc/server"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

const (
	defaultWriteRPCURL  = "http://localhost:35997"
	defaultPillarRPCURL = "http://localhost:35991"
)

type relayObservation struct {
	seenBlock       bool
	seenBlockAfter  time.Duration
	seenPaired      bool
	seenPairedAfter time.Duration
	confirmations   uint64
	contractStatus  string
	frontierHeight  uint64
	last            string
}

func TestRpcRelayDiagnosticViaRPC(t *testing.T) {
	if os.Getenv("PTLC_TESTNET_RPC_DIAGNOSTIC") != "1" {
		t.Skip("set PTLC_TESTNET_RPC_DIAGNOSTIC=1 to run the RPC relay diagnostic")
	}

	writeURL := envOr("PTLC_TESTNET_WRITE_RPC", defaultWriteRPCURL)
	pillarURL := envOr("PTLC_TESTNET_PILLAR_RPC", defaultPillarRPCURL)

	writer := newHarnessForURL(t, writeURL)
	pillar := newHarnessForURL(t, pillarURL)

	logEndpointHealth(t, "write-rpc", writer)
	logEndpointHealth(t, "pillar", pillar)

	locker := writer.keys[1]
	recipient := writer.keys[3]
	createData := definition.ABIPtlc.PackMethodPanic(
		definition.CreatePtlcMethodName,
		writer.currentTimestamp()+600,
		definition.PointTypeED25519,
		recipient.Public,
	)

	hash := writer.mustPublishSend(locker, types.PtlcContract, oneZNN(1), types.ZnnTokenStandard, createData)
	t.Logf("published PTLC create through %s: %s", writeURL, hash)

	observations := observeRelay(t, hash, map[string]*harness{
		"write-rpc": writer,
		"pillar":    pillar,
	}, 2*time.Minute)

	for name, observation := range observations {
		t.Logf("%s observation: %s", name, observation)
	}

	pillarObservation := observations["pillar"]
	writeObservation := observations["write-rpc"]
	if !pillarObservation.seenBlock {
		t.Fatalf("RPC-published block did not reach pillar within diagnostic window")
	}
	if !pillarObservation.seenPaired {
		t.Fatalf("RPC-published block reached pillar but pillar did not see contract receive")
	}
	if !writeObservation.seenBlock {
		t.Fatalf("RPC-published block reached pillar but was not visible from write RPC")
	}
	if !writeObservation.seenPaired {
		t.Fatalf("RPC-published block was visible from write RPC but contract receive was not visible there")
	}
}

func newHarnessForURL(t *testing.T, rpcURL string) *harness {
	t.Helper()

	var client *rpcclient.Client
	deadline := time.Now().Add(90 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		c, err := rpcclient.DialContext(ctx, rpcURL)
		cancel()
		if err == nil {
			client = c
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial %s: %v", rpcURL, err)
		}
		time.Sleep(1 * time.Second)
	}
	t.Cleanup(client.Close)

	h := &harness{
		t:      t,
		client: client,
		keys:   deriveDevKeys(t),
	}
	h.waitForReady()
	return h
}

func logEndpointHealth(t *testing.T, name string, h *harness) {
	t.Helper()

	momentum, err := h.frontierMomentum()
	if err != nil {
		t.Logf("%s frontier error: %v", name, err)
	} else {
		t.Logf("%s frontier: height=%d hash=%s timestamp=%d", name, momentum.Height, momentum.Hash, momentum.TimestampUnix)
	}

	var network api.NetworkInfoResponse
	if err := h.call(&network, "stats.networkInfo"); err != nil {
		t.Logf("%s networkInfo error: %v", name, err)
	} else {
		peers := make([]string, 0, len(network.Peers))
		for _, peer := range network.Peers {
			peers = append(peers, fmt.Sprintf("%s@%s", peer.Name, peer.IP))
		}
		t.Logf("%s networkInfo: peers=%d [%s]", name, network.NumPeers, strings.Join(peers, ", "))
	}

	var syncInfo protocol.SyncInfo
	if err := h.call(&syncInfo, "stats.syncInfo"); err != nil {
		t.Logf("%s syncInfo error: %v", name, err)
	} else {
		t.Logf("%s syncInfo: state=%d current=%d target=%d", name, syncInfo.State, syncInfo.CurrentHeight, syncInfo.TargetHeight)
	}
}

func observeRelay(t *testing.T, hash types.Hash, endpoints map[string]*harness, timeout time.Duration) map[string]*relayObservation {
	t.Helper()

	start := time.Now()
	deadline := start.Add(timeout)
	observations := make(map[string]*relayObservation, len(endpoints))
	for name := range endpoints {
		observations[name] = &relayObservation{}
	}

	for {
		allPaired := true
		for name, h := range endpoints {
			observation := observations[name]
			if momentum, err := h.frontierMomentum(); err == nil {
				observation.frontierHeight = momentum.Height
			}

			var block *api.AccountBlock
			err := h.call(&block, "ledger.getAccountBlockByHash", hash)
			if err != nil {
				observation.last = err.Error()
				allPaired = false
				continue
			}
			if block == nil {
				observation.last = "block not visible"
				allPaired = false
				continue
			}

			elapsed := time.Since(start).Round(time.Second)
			if !observation.seenBlock {
				observation.seenBlock = true
				observation.seenBlockAfter = elapsed
			}
			if block.ConfirmationDetail != nil {
				observation.confirmations = block.ConfirmationDetail.NumConfirmations
			}
			if block.PairedAccountBlock == nil {
				observation.last = "block visible without paired contract receive"
				allPaired = false
				continue
			}

			if !observation.seenPaired {
				observation.seenPaired = true
				observation.seenPairedAfter = elapsed
			}
			observation.last = "paired contract receive visible"
			observation.contractStatus = contractStatusString(block.PairedAccountBlock)
		}

		if allPaired {
			return observations
		}
		if time.Now().After(deadline) {
			return observations
		}
		time.Sleep(1 * time.Second)
	}
}

func contractStatusString(block *api.AccountBlock) string {
	if block.BlockType != nom.BlockTypeContractReceive {
		return fmt.Sprintf("unexpected block type %d", block.BlockType)
	}
	if len(block.Data) != 8 {
		return fmt.Sprintf("unexpected status data length %d", len(block.Data))
	}
	status := common.BytesToUint64(block.Data)
	if status == contractSuccess {
		return "success"
	}
	if status == contractFail {
		return "fail"
	}
	return fmt.Sprintf("unknown status %d", status)
}

func (o relayObservation) String() string {
	return fmt.Sprintf(
		"seenBlock=%t after=%s seenPaired=%t pairedAfter=%s confirmations=%d contractStatus=%s frontierHeight=%d last=%q",
		o.seenBlock,
		o.seenBlockAfter,
		o.seenPaired,
		o.seenPairedAfter,
		o.confirmations,
		o.contractStatus,
		o.frontierHeight,
		o.last,
	)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
