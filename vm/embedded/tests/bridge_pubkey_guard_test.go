package tests

import (
	"bytes"
	"encoding/base64"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func saveBridgeTestGlobals(t *testing.T) func() {
	t.Helper()

	origSporkId := types.BridgeAndLiquiditySpork.SporkId
	origImplemented := make(map[types.Hash]bool, len(types.ImplementedSporksMap))
	for k, v := range types.ImplementedSporksMap {
		origImplemented[k] = v
	}
	origAdmin := types.Address{}
	origAdmin.SetBytes(constants.InitialBridgeAdministrator.Bytes())
	origAdminDelay := constants.MinAdministratorDelay
	origSoftDelay := constants.MinSoftDelay
	origUnhaltDuration := constants.MinUnhaltDurationInMomentums
	origMinGuardians := constants.MinGuardians

	return func() {
		types.BridgeAndLiquiditySpork.SporkId = origSporkId
		for k := range types.ImplementedSporksMap {
			delete(types.ImplementedSporksMap, k)
		}
		for k, v := range origImplemented {
			types.ImplementedSporksMap[k] = v
		}
		constants.InitialBridgeAdministrator.SetBytes(origAdmin.Bytes())
		constants.MinAdministratorDelay = origAdminDelay
		constants.MinSoftDelay = origSoftDelay
		constants.MinUnhaltDurationInMomentums = origUnhaltDuration
		constants.MinGuardians = origMinGuardians
	}
}

func insertMomentumWithoutAutoReceive(t *testing.T, z mock.MockZenon, supervisor *vm.Supervisor) {
	t.Helper()

	store := z.Chain().GetFrontierMomentumStore()
	previousMomentum, err := store.GetFrontierMomentum()
	common.FailIfErr(t, err)

	ts := previousMomentum.Timestamp.Add(10 * time.Second)
	expectedProducer, err := z.Consensus().GetMomentumProducer(ts)
	common.FailIfErr(t, err)
	if expectedProducer == nil {
		t.Fatalf("nil expected momentum producer")
	}

	var producerKey *wallet.KeyPair
	for _, key := range g.PillarKeys {
		if key.Address == *expectedProducer {
			producerKey = key
			break
		}
	}
	if producerKey == nil {
		t.Fatalf("could not find mock pillar key for producer %v", expectedProducer)
	}

	blocks := z.Chain().GetNewMomentumContent()
	m := &nom.Momentum{
		ChainIdentifier: z.Chain().ChainIdentifier(),
		PreviousHash:    previousMomentum.Hash,
		Height:          previousMomentum.Height + 1,
		TimestampUnix:   uint64(ts.Unix()),
		Content:         nom.NewMomentumContent(blocks),
		Version:         uint64(1),
	}
	m.EnsureCache()

	momentumTx, err := supervisor.GenerateMomentum(&nom.DetailedMomentum{
		Momentum:      m,
		AccountBlocks: blocks,
	}, producerKey.Signer)
	common.FailIfErr(t, err)
	z.Broadcaster().CreateMomentum(momentumTx)
}

func TestBridge_ChangeTssECDSAPubKey_InvalidCompressedKeyAutoReceive(t *testing.T) {
	defer saveBridgeTestGlobals(t)()

	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	activateBridgeStep2(t, z)

	zeroes := make([]byte, constants.CompressedECDSAPubKeyLength)
	maxX := make([]byte, constants.CompressedECDSAPubKeyLength)
	maxX[0] = 0x02
	for i := 1; i < len(maxX); i++ {
		maxX[i] = 0xff
	}

	cases := []struct {
		name   string
		pubKey string
	}{
		{"all-zeroes", base64.StdEncoding.EncodeToString(zeroes)},
		{"max-x", base64.StdEncoding.EncodeToString(maxX)},
	}

	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			heightBefore := z.Chain().GetFrontierAccountStore(types.BridgeContract).Identifier().Height

			sendBlock := z.InsertSendBlock(&nom.AccountBlock{
				Address:   g.User1.Address,
				ToAddress: types.BridgeContract,
				Data: definition.ABIBridge.PackMethodPanic(
					definition.ChangeTssECDSAPubKeyMethodName,
					tc.pubKey,
					"",
					"",
				),
			}, nil, mock.SkipVmChanges)

			// Confirm the send block in a momentum without triggering the pillar
			// contract worker yet, so it sits at the front of the bridge sequencer.
			insertMomentumWithoutAutoReceive(t, z, supervisor)

			// Exercise Supervisor.GenerateAutoReceive directly: must not panic and
			// must return a failed ContractReceive with ErrInvalidCompressedECDSAPubKey.
			exec, err := supervisor.GenerateAutoReceive(sendBlock)
			common.FailIfErr(t, err)
			if exec == nil || exec.Transaction == nil || exec.Transaction.Block == nil {
				t.Fatalf("expected non-nil ContractExecution transaction")
			}
			common.ExpectError(t, exec.ReturnedError, constants.ErrInvalidCompressedECDSAPubKey)
			recvBlock := exec.Transaction.Block
			common.Expect(t, recvBlock.BlockType, uint64(nom.BlockTypeContractReceive))
			common.Expect(t, recvBlock.FromBlockHash, sendBlock.Hash)
			if !bytes.Equal(recvBlock.Data, common.Uint64ToBytes(2)) {
				t.Fatalf("expected failed status data %x, got %x", common.Uint64ToBytes(2), recvBlock.Data)
			}

			// Let the pillar auto-receive and cement the failed receive block.
			z.InsertNewMomentum()
			z.InsertNewMomentum()

			bridgeStore := z.Chain().GetFrontierAccountStore(types.BridgeContract)
			heightAfter := bridgeStore.Identifier().Height
			if heightAfter <= heightBefore {
				t.Fatalf("expected bridge account height to advance past %d, got %d", heightBefore, heightAfter)
			}

			frontierRecv, err := bridgeStore.ByHeight(heightAfter)
			common.FailIfErr(t, err)
			common.Expect(t, frontierRecv.BlockType, uint64(nom.BlockTypeContractReceive))
			common.Expect(t, frontierRecv.FromBlockHash, sendBlock.Hash)
			common.Expect(t, frontierRecv.Hash, recvBlock.Hash)
			if !bytes.Equal(frontierRecv.Data, common.Uint64ToBytes(2)) {
				t.Fatalf("expected cemented failed status data %x, got %x", common.Uint64ToBytes(2), frontierRecv.Data)
			}

			// Sequencer mailbox must be drained so subsequent bridge calls are not stuck.
			momentumStore := z.Chain().GetFrontierMomentumStore()
			if next := bridgeStore.SequencerFront(momentumStore.GetAccountMailbox(types.BridgeContract)); next != nil {
				t.Fatalf("expected empty bridge sequencer mailbox, still queued %v", next)
			}
		})
	}

	// Confirm a subsequent valid bridge call executes cleanly after the rejected sends.
	defer z.CallContract(setOrchestratorInfo(g.User5.Address, 7, 3, 15, 10)).
		Error(t, nil)
	insertMomentums(z, 2)
}
