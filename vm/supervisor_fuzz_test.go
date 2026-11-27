package vm

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"math/big"
	"sort"
	"testing"

	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/abi"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

type fuzzEmbeddedTarget struct {
	address   types.Address
	selectors [][]byte
}

func buildFuzzEmbeddedTargets() []fuzzEmbeddedTarget {
	specs := []struct {
		addr types.Address
		abi  abi.ABIContract
	}{
		{types.PlasmaContract, definition.ABIPlasma},
		{types.PillarContract, definition.ABIPillars},
		{types.TokenContract, definition.ABIToken},
		{types.SentinelContract, definition.ABISentinel},
		{types.SwapContract, definition.ABISwap},
		{types.StakeContract, definition.ABIStake},
		{types.SporkContract, definition.ABISpork},
		{types.LiquidityContract, definition.ABILiquidity},
		{types.AcceleratorContract, definition.ABIAccelerator},
		{types.BridgeContract, definition.ABIBridge},
		{types.HtlcContract, definition.ABIHtlc},
		{types.PtlcContract, definition.ABIPtlc},
	}

	targets := make([]fuzzEmbeddedTarget, 0, len(specs))
	for _, spec := range specs {
		names := make([]string, 0, len(spec.abi.Methods))
		for name := range spec.abi.Methods {
			names = append(names, name)
		}
		sort.Strings(names)
		selectors := make([][]byte, 0, len(names))
		for _, name := range names {
			m := spec.abi.Methods[name]
			selectors = append(selectors, append([]byte(nil), m.Id()...))
		}
		targets = append(targets, fuzzEmbeddedTarget{
			address:   spec.addr,
			selectors: selectors,
		})
	}
	return targets
}

type fuzzSporkCache struct {
	reverifyCacheStore
	accelerator bool
	bridgeLiq   bool
	htlc        bool
	ptlc        bool
}

func (c *fuzzSporkCache) IsSporkActive(spork *types.ImplementedSpork) (bool, error) {
	switch spork {
	case types.DynamicPlasmaSpork:
		return c.dynamicPlasma, nil
	case types.AcceleratorSpork:
		return c.accelerator, nil
	case types.BridgeAndLiquiditySpork:
		return c.bridgeLiq, nil
	case types.HtlcSpork:
		return c.htlc, nil
	case types.PtlcSpork:
		return c.ptlc, nil
	default:
		return false, nil
	}
}

// FuzzApplyBlockEmbeddedRePackRejected feeds arbitrary call data across all
// embedded contracts and spork combinations into Supervisor.ApplyBlock (and
// ApplyBlockInMomentum) with the real verifier. It asserts that:
//   - ApplyBlock never accepts an externally supplied block whose Data was
//     mutated in place by ValidateSendBlock (ComputeHash() must equal the
//     originally signed Hash and Data must be unchanged).
//   - ApplyBlock never returns ErrVmRunPanic on arbitrary call payloads.
//   - Any rejected block returns a nil transaction.
func FuzzApplyBlockEmbeddedRePackRejected(f *testing.F) {
	targets := buildFuzzEmbeddedTargets()

	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 7
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	userAddr := types.PubKeyToAddress(pub)

	// Initialize types.SporkAddress (normally set during genesis loading) to the
	// fuzz caller so spork-gated methods (plasma.SetVariables, spork.CreateSpork,
	// spork.ActivateSpork) pass authorization and exercise their ABI re-pack paths.
	prevSporkAddress := types.SporkAddress
	sporkAddr := userAddr
	types.SporkAddress = &sporkAddr
	f.Cleanup(func() {
		types.SporkAddress = prevSporkAddress
	})

	frontier := &nom.Momentum{
		Hash:   types.HexToHashPanic("01aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb900"),
		Height: 1,
	}

	// Seed interesting canonical, trailing-byte, and non-canonical-padding cases
	// across multiple contracts and both pre- and post-DynamicPlasma states.
	fuseCanon := definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, userAddr)
	fuseTrail := append(append([]byte(nil), fuseCanon...), 0x00)
	fusePad := append([]byte(nil), fuseCanon...)
	fusePad[4] = 0x01

	stakeCanon := definition.ABIStake.PackMethodPanic(definition.StakeMethodName, int64(constants.StakeTimeMinSec))
	stakeTrail := append(append([]byte(nil), stakeCanon...), 0x00)

	depositCanon := definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName)
	depositTrail := append(append([]byte(nil), depositCanon...), 0x00)

	donateCanon := definition.ABIAccelerator.PackMethodPanic(definition.DonateMethodName)
	donateTrail := append(append([]byte(nil), donateCanon...), 0x00)

	// (contractIdx, methodIdx=255 means use rawPayload verbatim, sporkFlags, tokenChoice, amountUnits, rawPayload)
	f.Add(uint8(0), uint8(255), uint8(0x00), uint8(1), uint64(constants.FuseMinAmount.Uint64()), fuseCanon)
	f.Add(uint8(0), uint8(255), uint8(0x00), uint8(1), uint64(constants.FuseMinAmount.Uint64()), fuseTrail)
	f.Add(uint8(0), uint8(255), uint8(0x01), uint8(1), uint64(constants.FuseMinAmount.Uint64()), fusePad)
	f.Add(uint8(0), uint8(255), uint8(0x1f), uint8(1), uint64(constants.FuseMinAmount.Uint64()), fuseTrail)
	f.Add(uint8(0), uint8(255), uint8(0x20), uint8(1), uint64(constants.FuseMinAmount.Uint64()), fuseTrail)
	f.Add(uint8(5), uint8(255), uint8(0x00), uint8(0), uint64(constants.StakeMinAmount.Uint64()), stakeCanon)
	f.Add(uint8(5), uint8(255), uint8(0x00), uint8(0), uint64(constants.StakeMinAmount.Uint64()), stakeTrail)
	f.Add(uint8(1), uint8(255), uint8(0x00), uint8(1), uint64(1), depositCanon)
	f.Add(uint8(1), uint8(255), uint8(0x00), uint8(1), uint64(1), depositTrail)
	f.Add(uint8(8), uint8(255), uint8(0x02), uint8(0), uint64(1), donateCanon)
	f.Add(uint8(8), uint8(255), uint8(0x02), uint8(0), uint64(1), donateTrail)
	// Also seed selector-prefixed mode (methodIdx < 254).
	f.Add(uint8(0), uint8(0), uint8(0x00), uint8(1), uint64(constants.FuseMinAmount.Uint64()), fuseCanon[4:])
	f.Add(uint8(9), uint8(0), uint8(0x1f), uint8(0), uint64(0), []byte{0x01})
	f.Add(uint8(10), uint8(0), uint8(0x1f), uint8(0), uint64(1), []byte{0x00})
	f.Add(uint8(11), uint8(0), uint8(0x1f), uint8(0), uint64(1), []byte{0x00})

	generousBalance := new(big.Int).Mul(big.NewInt(1_000_000), big.NewInt(constants.Decimals))
	fusedQsr := new(big.Int).Mul(big.NewInt(5000), big.NewInt(constants.Decimals))

	f.Fuzz(func(t *testing.T, contractIdx, methodIdx, sporkFlags, tokenChoice uint8, amountUnits uint64, rawPayload []byte) {
		target := targets[int(contractIdx)%len(targets)]

		// Cap payload length so fuzz iterations stay bounded while still
		// exercising values above MaxDataLength.
		const maxFuzzPayload = constants.MaxDataLength + 32
		if len(rawPayload) > maxFuzzPayload {
			rawPayload = rawPayload[:maxFuzzPayload]
		}

		var data []byte
		if methodIdx < 254 && len(target.selectors) > 0 {
			sel := target.selectors[int(methodIdx)%len(target.selectors)]
			data = make([]byte, len(sel)+len(rawPayload))
			copy(data, sel)
			copy(data[len(sel):], rawPayload)
		} else {
			data = append([]byte(nil), rawPayload...)
		}

		var zts types.ZenonTokenStandard
		var amount *big.Int
		switch tokenChoice % 3 {
		case 0:
			zts = types.ZnnTokenStandard
			amount = new(big.Int).SetUint64(amountUnits)
		case 1:
			zts = types.QsrTokenStandard
			amount = new(big.Int).SetUint64(amountUnits)
		default:
			zts = types.ZeroTokenStandard
			amount = big.NewInt(0)
		}

		acctStore := account.NewAccountStore(userAddr, db.NewMemDB())
		if err := acctStore.SetBalance(types.ZnnTokenStandard, generousBalance); err != nil {
			t.Fatalf("set ZNN balance: %v", err)
		}
		if err := acctStore.SetBalance(types.QsrTokenStandard, generousBalance); err != nil {
			t.Fatalf("set QSR balance: %v", err)
		}

		cache := &fuzzSporkCache{
			reverifyCacheStore: reverifyCacheStore{
				dynamicPlasma: sporkFlags&0x01 != 0,
				fusedQsr:      fusedQsr,
			},
			accelerator: sporkFlags&0x02 != 0,
			bridgeLiq:   sporkFlags&0x04 != 0,
			htlc:        sporkFlags&0x08 != 0,
			ptlc:        sporkFlags&0x10 != 0,
		}
		chainStub := &stubChainForGenerate{
			momentumStore: &reverifyMomentumStore{frontier: frontier},
			accountStore:  acctStore,
			cacheStore:    cache,
		}
		s := NewSupervisor(chainStub, nil)

		fusedPlasma := uint64(constants.MaxPlasmaForAccountBlock)
		if fusedPlasma > constants.MaxFusionPlasmaForAccount {
			fusedPlasma = constants.MaxFusionPlasmaForAccount
		}

		b := &nom.AccountBlock{
			Version:              1,
			ChainIdentifier:      1,
			BlockType:            nom.BlockTypeUserSend,
			Address:              userAddr,
			ToAddress:            target.address,
			Amount:               amount,
			TokenStandard:        zts,
			MomentumAcknowledged: frontier.Identifier(),
			Height:               1,
			Data:                 data,
			FusedPlasma:          fusedPlasma,
		}
		b.Hash = b.ComputeHash()
		b.PublicKey = append([]byte(nil), pub...)
		b.Signature = ed25519.Sign(priv, b.Hash.Bytes())

		origData := append([]byte(nil), b.Data...)
		origHash := b.Hash

		var tx *nom.AccountBlockTransaction
		var err error
		if sporkFlags&0x20 != 0 {
			tx, err = s.ApplyBlockInMomentum(b, 10)
		} else {
			tx, err = s.ApplyBlock(b)
		}

		if errors.Is(err, constants.ErrVmRunPanic) {
			t.Fatalf("unexpected VM panic for contract=%v sporkFlags=0x%02x dataLen=%d: %v", target.address, sporkFlags, len(origData), err)
		}
		if err != nil {
			if tx != nil {
				t.Fatalf("non-nil transaction returned alongside error %v", err)
			}
			return
		}
		if tx == nil || tx.Block == nil {
			t.Fatalf("nil transaction returned with nil error")
		}
		if !bytes.Equal(tx.Block.Data, origData) {
			t.Fatalf("ApplyBlock accepted a block whose Data was mutated in place (contract=%v, sporkFlags=0x%02x, origLen=%d, newLen=%d)",
				target.address, sporkFlags, len(origData), len(tx.Block.Data))
		}
		if tx.Block.Hash != origHash || tx.Block.ComputeHash() != origHash {
			t.Fatalf("ApplyBlock accepted a block with mismatched hash (orig=%v, block.Hash=%v, computed=%v)",
				origHash, tx.Block.Hash, tx.Block.ComputeHash())
		}
	})
}
