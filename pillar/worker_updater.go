package pillar

import (
	"bytes"
	"math/big"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

func canPerformEmbeddedUpdate(momentumStore store.Momentum, pool chain.AccountPool, contract types.Address) error {
	store := pool.GetFrontierAccountStore(contract)
	context := vm_context.NewAccountContext(momentumStore, store, nil, nil)
	return implementation.CanPerformUpdate(context)
}

func (w *worker) updateContracts(momentumStore store.Momentum) error {
	pricing, err := newUpdatePricing(momentumStore)
	if err != nil {
		return err
	}
	if pricing != nil {
		if err := w.replaceUnderpricedUpdate(momentumStore, pricing); err != nil {
			return err
		}
	}
	for _, address := range types.EmbeddedWUpdate {
		if err := canPerformEmbeddedUpdate(momentumStore, w.chain, address); err == nil {
			if pricing != nil && w.unconfirmedUpdate(address) != nil {
				continue
			}
			w.log.Info("producing block to update embedded-contract", "contract-address", address)
			template := newUpdateTemplate(w.coinbase.Address, address)
			if pricing != nil {
				if err := w.priceUpdate(momentumStore, pricing.fusionPrice, template, nil); err != nil {
					return err
				}
			}
			if err := w.produceUpdate(template); err != nil {
				return err
			}
		} else if err == constants.ErrUpdateTooRecent || err == constants.ErrContractMethodNotFound {
		} else {
			return err
		}
	}
	return nil
}

const updateFusionPriceHeadroom = 2

type updatePricing struct {
	plasma      dp.DynamicPlasma
	fusionPrice uint64
}

func newUpdatePricing(momentumStore store.Momentum) (*updatePricing, error) {
	active, err := momentumStore.IsSporkActive(types.DynamicPlasmaSpork)
	if err != nil || !active {
		return nil, err
	}
	frontier, err := momentumStore.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	if frontier.Version < nom.DynamicPlasmaMomentumVersion {
		return nil, nil
	}
	config, err := momentumStore.GetPlasmaVariables()
	if err != nil {
		return nil, err
	}
	return &updatePricing{
		plasma:      dp.NewDynamicPlasma(frontier, config),
		fusionPrice: frontier.NextFusionPrice,
	}, nil
}

func updateFusedPlasma(basePlasma, fusionPrice, availablePlasma uint64) (uint64, error) {
	floor := new(big.Int).Mul(new(big.Int).SetUint64(basePlasma), new(big.Int).SetUint64(fusionPrice))
	floor.Add(floor, new(big.Int).SetUint64(dp.PriceScaleFactor-1))
	floor.Div(floor, new(big.Int).SetUint64(dp.PriceScaleFactor))
	available := new(big.Int).SetUint64(availablePlasma)
	withHeadroom := new(big.Int).Mul(floor, big.NewInt(updateFusionPriceHeadroom))
	if withHeadroom.Cmp(available) <= 0 {
		return withHeadroom.Uint64(), nil
	}
	if floor.Cmp(available) <= 0 {
		return floor.Uint64(), nil
	}
	return 0, constants.ErrNotEnoughPlasma
}

func newUpdateTemplate(producer, contract types.Address) *nom.AccountBlock {
	return &nom.AccountBlock{
		BlockType: nom.BlockTypeUserSend,
		Address:   producer,
		ToAddress: contract,
		Data:      definition.ABICommon.PackMethodPanic(definition.UpdateMethodName),
	}
}

func isUpdateBlock(block *nom.AccountBlock) bool {
	return types.IsEmbeddedAddress(block.ToAddress) && bytes.Equal(block.Data, definition.ABICommon.PackMethodPanic(definition.UpdateMethodName))
}

func (w *worker) unconfirmedUpdate(contract types.Address) *nom.AccountBlock {
	for _, block := range w.chain.GetUncommittedAccountBlocksByAddress(w.coinbase.Address) {
		if block.ToAddress == contract && isUpdateBlock(block) {
			return block
		}
	}
	return nil
}

func (w *worker) lowestUnderpricedUpdate(plasma dp.DynamicPlasma) *nom.AccountBlock {
	for _, block := range w.chain.GetUncommittedAccountBlocksByAddress(w.coinbase.Address) {
		if isUpdateBlock(block) && !plasma.ValidPrice(block) {
			return block
		}
	}
	return nil
}

func (w *worker) priceUpdate(momentumStore store.Momentum, fusionPrice uint64, template, replaced *nom.AccountBlock) error {
	var account store.Account
	if replaced == nil {
		account = w.chain.GetFrontierAccountStore(w.coinbase.Address)
	} else {
		// The pending list can change between lowestUnderpricedUpdate and
		// here (a rollback, or the stale block confirming), and the pool then
		// has no store at the predecessor. Give up the round; the next one
		// looks again.
		account = w.chain.GetAccountStore(w.coinbase.Address, replaced.Previous())
		if account == nil {
			return ErrUpdatePredecessorMissing
		}
		template.Height, template.PreviousHash = replaced.Height, replaced.PreviousHash
	}
	template.MomentumAcknowledged = momentumStore.Identifier()
	cache := w.chain.GetCacheStore(template.MomentumAcknowledged)
	basePlasma, err := vm.GetBasePlasmaForAccountBlock(vm_context.NewAccountContext(nil, account, cache, nil), template)
	if err != nil {
		return err
	}
	available, err := vm.AvailablePlasmaV2(cache, account)
	if err != nil {
		return err
	}
	template.FusedPlasma, err = updateFusedPlasma(basePlasma, fusionPrice, available)
	return err
}

func (w *worker) produceUpdate(template *nom.AccountBlock) error {
	block, err := w.supervisor.GenerateFromTemplate(template, w.coinbase.Signer)
	if err != nil {
		return err
	}
	if err := w.broadcaster.CreateAccountBlock(block); err != nil {
		w.log.Error("unable to insert update block for embedded-contract", "contract-address", template.ToAddress, "reason", err)
	}
	return nil
}

func (w *worker) replaceUnderpricedUpdate(momentumStore store.Momentum, pricing *updatePricing) error {
	replaced := w.lowestUnderpricedUpdate(pricing.plasma)
	if replaced == nil {
		return nil
	}
	w.log.Info("replacing underpriced block to update embedded-contract", "contract-address", replaced.ToAddress, "height", replaced.Height, "fused-plasma", replaced.FusedPlasma)
	template := newUpdateTemplate(w.coinbase.Address, replaced.ToAddress)
	if err := w.priceUpdate(momentumStore, pricing.fusionPrice, template, replaced); err != nil {
		return err
	}
	return w.produceUpdate(template)
}
