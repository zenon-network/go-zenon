package verifier

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
)

// stubSporkMomentumStore overrides IsSporkActive so tests can toggle the
// Dynamic Plasma spork without a full chain.
type stubSporkMomentumStore struct {
	store.Momentum
	dynamicPlasmaActive bool
}

func (s *stubSporkMomentumStore) IsSporkActive(spork *types.ImplementedSpork) (bool, error) {
	if spork == types.DynamicPlasmaSpork {
		return s.dynamicPlasmaActive, nil
	}
	return false, nil
}

// A user receive block with non-empty Data must be rejected once the Dynamic
// Plasma spork is enforced at the block's acknowledged momentum.
func TestAccountBlockVerifier_ReceiveData_DynamicPlasmaEnforced_RejectsData(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")

	block := &nom.AccountBlock{
		Version:       1,
		BlockType:     nom.BlockTypeUserReceive,
		Address:       address,
		Height:        2,
		Data:          []byte("trailing"),
		FromBlockHash: types.NewHash([]byte("send")),
	}

	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  nil,
		momentumStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
		frontierStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
	}

	err := abv.receiveData()
	if err != ErrABDataMustBeEmpty {
		t.Fatalf("expected ErrABDataMustBeEmpty, got %v", err)
	}
}

// A user receive block with non-empty Data is accepted before the Dynamic
// Plasma spork is enforced.
func TestAccountBlockVerifier_ReceiveData_DynamicPlasmaNotEnforced_AllowsData(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")

	block := &nom.AccountBlock{
		Version:       1,
		BlockType:     nom.BlockTypeUserReceive,
		Address:       address,
		Height:        2,
		Data:          []byte("some data"),
		FromBlockHash: types.NewHash([]byte("send")),
	}

	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  nil,
		momentumStore: &stubSporkMomentumStore{dynamicPlasmaActive: false},
		frontierStore: &stubSporkMomentumStore{dynamicPlasmaActive: false},
	}

	if err := abv.receiveData(); err != nil {
		t.Fatalf("expected nil error before spork, got %v", err)
	}
}

// A user receive block with empty Data is accepted regardless of spork state.
func TestAccountBlockVerifier_ReceiveData_EmptyData_AllwaysAllowed(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")

	block := &nom.AccountBlock{
		Version:       1,
		BlockType:     nom.BlockTypeUserReceive,
		Address:       address,
		Height:        2,
		Data:          nil,
		FromBlockHash: types.NewHash([]byte("send")),
	}

	// With spork enforced
	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  nil,
		momentumStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
		frontierStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
	}
	if err := abv.receiveData(); err != nil {
		t.Fatalf("expected nil error for empty data with spork enforced, got %v", err)
	}

	// Without spork
	abv.momentumStore = &stubSporkMomentumStore{dynamicPlasmaActive: false}
	abv.frontierStore = &stubSporkMomentumStore{dynamicPlasmaActive: false}
	if err := abv.receiveData(); err != nil {
		t.Fatalf("expected nil error for empty data without spork, got %v", err)
	}
}

// An embedded (contract) receive block with Data is not affected by this
// rule — embedded blocks have their own data semantics.
func TestAccountBlockVerifier_ReceiveData_EmbeddedAddress_SkipsCheck(t *testing.T) {
	block := &nom.AccountBlock{
		Version:       1,
		BlockType:     nom.BlockTypeContractReceive,
		Address:       types.PlasmaContract,
		Height:        2,
		Data:          []byte("contract data"),
		FromBlockHash: types.NewHash([]byte("send")),
	}

	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  nil,
		momentumStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
		frontierStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
	}

	if err := abv.receiveData(); err != nil {
		t.Fatalf("expected nil error for embedded receive block, got %v", err)
	}
}

// A send block is not affected by the receive-data rule.
func TestAccountBlockVerifier_ReceiveData_SendBlock_SkipsCheck(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")

	block := &nom.AccountBlock{
		Version:   1,
		BlockType: nom.BlockTypeUserSend,
		Address:   address,
		Height:    2,
		Data:      []byte("send data"),
	}

	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  nil,
		momentumStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
		frontierStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
	}

	if err := abv.receiveData(); err != nil {
		t.Fatalf("expected nil error for send block, got %v", err)
	}
}

// When momentumStore is nil (user blocks), receiveData falls back to
// chain.GetMomentumStore(block.MomentumAcknowledged). With a nil chain the
// check must not panic; it should return ErrABMAMissing.
func TestAccountBlockVerifier_ReceiveData_NilChain_ReturnsMAMissing(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")

	block := &nom.AccountBlock{
		Version:       1,
		BlockType:     nom.BlockTypeUserReceive,
		Address:       address,
		Height:        2,
		Data:          []byte("data"),
		FromBlockHash: types.NewHash([]byte("send")),
	}

	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  nil,
		momentumStore: nil, // simulates user block
		frontierStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
		chain:         nil, // no chain available
	}

	err := abv.receiveData()
	if err != ErrABMAMissing {
		t.Fatalf("expected ErrABMAMissing with nil chain, got %v", err)
	}
}
