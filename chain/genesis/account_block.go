package genesis

import (
	"math/big"
	"sync"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

type mockStable struct {
}

func (m *mockStable) GetStableAccountDB(address types.Address) db.DB {
	return db.NewMemDB()
}

func newGenesisAccountBlocks(cfg *GenesisConfig) chain.AccountPool {
	pool := chain.NewAccountPool(new(mockStable))

	changes := new(sync.Mutex)
	pool.AddAccountBlockTransaction(changes, genesisPillarContractConfig(cfg))
	pool.AddAccountBlockTransaction(changes, genesisTokenContractConfig(cfg))
	pool.AddAccountBlockTransaction(changes, genesisPlasmaContractConfig(cfg))
	pool.AddAccountBlockTransaction(changes, genesisSwapContractConfig(cfg))

	alreadySet := map[types.Address]interface{}{
		types.PillarContract: struct{}{},
		types.TokenContract:  struct{}{},
		types.PlasmaContract: struct{}{},
		types.SwapContract:   struct{}{},
	}

	if cfg.SporkConfig != nil {
		pool.AddAccountBlockTransaction(changes, genesisSporkContractConfig(cfg))
		alreadySet[types.SporkContract] = struct{}{}
	}

	list := genesisBalanceBlocksConfig(cfg, alreadySet)
	for _, el := range list {
		pool.AddAccountBlockTransaction(changes, el)
	}
	return pool
}

func wrap(cfg *GenesisConfig, context vm_context.AccountVmContext) *nom.AccountBlockTransaction {
	address := *context.Address()
	block := &nom.AccountBlock{
		Version:         1,
		ChainIdentifier: cfg.ChainIdentifier,
		BlockType:       nom.BlockTypeGenesisReceive,
		Height:          1,
		Address:         address,
	}

	for _, block := range cfg.GenesisBlocks.Blocks {
		if block.Address != address {
			continue
		}

		for zts, balance := range block.BalanceList {
			common.DealWithErr(context.SetBalance(zts, balance))
		}
	}

	changes, err := context.Changes()
	common.DealWithErr(err)
	block.ChangesHash = db.PatchHash(changes)
	block.Hash = block.ComputeHash()

	return &nom.AccountBlockTransaction{
		Block:   block,
		Changes: changes,
	}
}

func newContext(address types.Address) (vm_context.AccountVmContext, db.DB) {
	context := vm_context.NewGenesisAccountContext(address)
	contextStorage := context.Storage()
	return context, contextStorage
}

func genesisPillarContractConfig(cfg *GenesisConfig) *nom.AccountBlockTransaction {
	config := cfg.PillarConfig
	context, contextStorage := newContext(types.PillarContract)

	for _, pillar := range config.Pillars {
		common.DealWithErr(pillar.Save(contextStorage))
		common.DealWithErr((&definition.ProducingPillar{
			Name:      pillar.Name,
			Producing: &pillar.BlockProducingAddress,
		}).Save(contextStorage))
	}
	for _, delegation := range config.Delegations {
		common.DealWithErr(delegation.Save(contextStorage))
	}
	for _, legacyEntry := range config.LegacyEntries {
		common.DealWithErr(legacyEntry.Save(contextStorage))
	}

	return wrap(cfg, context)
}
func genesisTokenContractConfig(cfg *GenesisConfig) *nom.AccountBlockTransaction {
	config := cfg.TokenConfig
	context, contextStorage := newContext(types.TokenContract)

	for _, token := range config.Tokens {
		common.DealWithErr(token.Save(contextStorage))
	}

	return wrap(cfg, context)
}
func genesisPlasmaContractConfig(cfg *GenesisConfig) *nom.AccountBlockTransaction {
	config := cfg.PlasmaConfig
	context, contextStorage := newContext(types.PlasmaContract)

	fusedAmount := make(map[types.Address]*big.Int)
	for _, entry := range config.Fusions {
		common.DealWithErr(entry.Save(contextStorage))

		amount, ok := fusedAmount[entry.Beneficiary]
		if ok {
			amount.Add(amount, entry.Amount)
		} else {
			fusedAmount[entry.Beneficiary] = new(big.Int).Set(entry.Amount)
		}
	}
	for addr, amount := range fusedAmount {
		common.DealWithErr((&definition.FusedAmount{
			Beneficiary: addr,
			Amount:      amount,
		}).Save(contextStorage))
	}

	return wrap(cfg, context)
}
func genesisSwapContractConfig(cfg *GenesisConfig) *nom.AccountBlockTransaction {
	config := cfg.SwapConfig
	context, contextStorage := newContext(types.SwapContract)

	for _, entry := range config.Entries {
		common.DealWithErr(entry.Save(contextStorage))
	}

	return wrap(cfg, context)
}
func genesisSporkContractConfig(cfg *GenesisConfig) *nom.AccountBlockTransaction {
	config := cfg.SporkConfig
	context, contextStorage := newContext(types.SporkContract)

	for _, entry := range config.Sporks {
		entry.Save(contextStorage)
	}

	return wrap(cfg, context)
}
func genesisBalanceBlocksConfig(cfg *GenesisConfig, alreadySet map[types.Address]interface{}) []*nom.AccountBlockTransaction {
	list := make([]*nom.AccountBlockTransaction, 0, len(cfg.GenesisBlocks.Blocks))
	for _, genesisBlock := range cfg.GenesisBlocks.Blocks {
		if _, ok := alreadySet[genesisBlock.Address]; ok {
			continue
		}

		context, _ := newContext(genesisBlock.Address)
		list = append(list, wrap(cfg, context))
	}

	return list
}
