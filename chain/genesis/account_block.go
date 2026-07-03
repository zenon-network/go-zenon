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

// mockStable backs the throwaway account pool used during genesis
// construction: every account starts from an empty in-memory
// database instead of momentum-confirmed state.
type mockStable struct {
}

func (m *mockStable) GetStableAccountDB(address types.Address) db.DB {
	return db.NewMemDB()
}

// newGenesisAccountBlocks fabricates all genesis account blocks into
// a fresh in-memory account pool: one block per seeded embedded
// contract (pillar, token, plasma, swap and — only when SporkConfig
// is set — spork), then one balance-only block for every remaining
// address in GenesisBlocks. The momentum VM later copies the pool's
// content into the genesis momentum (see newGenesisMomentum).
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

// wrap finalizes one genesis account block from the contract storage
// already written into context: it applies the address's BalanceList
// from GenesisBlocks (if any), then packs the accumulated changes
// into an unsigned height-1 block of nom.BlockTypeGenesisReceive that
// records the patch's hash in ChangesHash. Account-block hashes do
// not cover ChangesHash; it is the enclosing genesis momentum that
// commits to the full state patch.
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

// newContext opens an in-memory account VM context for address and
// returns it together with the contract-storage view the genesis*
// helpers write the definition entries into.
func newContext(address types.Address) (vm_context.AccountVmContext, db.DB) {
	context := vm_context.NewGenesisAccountContext(address)
	contextStorage := context.Storage()
	return context, contextStorage
}

// genesisPillarContractConfig builds the pillar contract's genesis
// block: each pillar is stored both as a PillarInfo and as a
// ProducingPillar entry keyed by its block-producing address, plus
// the configured delegations and legacy-pillar entries.
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

// genesisTokenContractConfig builds the token contract's genesis
// block holding the initial ZTS token definitions.
func genesisTokenContractConfig(cfg *GenesisConfig) *nom.AccountBlockTransaction {
	config := cfg.TokenConfig
	context, contextStorage := newContext(types.TokenContract)

	for _, token := range config.Tokens {
		common.DealWithErr(token.Save(contextStorage))
	}

	return wrap(cfg, context)
}

// genesisPlasmaContractConfig builds the plasma contract's genesis
// block: every fusion entry is stored individually and the totals
// are aggregated into one FusedAmount per beneficiary.
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

// genesisSwapContractConfig builds the swap contract's genesis block
// holding the legacy balances redeemable per key-id hash.
func genesisSwapContractConfig(cfg *GenesisConfig) *nom.AccountBlockTransaction {
	config := cfg.SwapConfig
	context, contextStorage := newContext(types.SwapContract)

	for _, entry := range config.Entries {
		common.DealWithErr(entry.Save(contextStorage))
	}

	return wrap(cfg, context)
}

// genesisSporkContractConfig builds the spork contract's genesis
// block holding the sporks pre-activated at genesis; only called
// when SporkConfig is present.
func genesisSporkContractConfig(cfg *GenesisConfig) *nom.AccountBlockTransaction {
	config := cfg.SporkConfig
	context, contextStorage := newContext(types.SporkContract)

	for _, entry := range config.Sporks {
		entry.Save(contextStorage)
	}

	return wrap(cfg, context)
}

// genesisBalanceBlocksConfig builds a balance-only genesis block for
// every GenesisBlocks address not in alreadySet — the contract
// addresses whose blocks already carry their balances via wrap.
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
