package verifier

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
)

// stubMomentumStore satisfies store.Momentum by embedding a nil interface and
// overriding only the methods content() exercises.
type stubMomentumStore struct {
	store.Momentum
}

func (*stubMomentumStore) GetFrontierAccountBlock(types.Address) (*nom.AccountBlock, error) {
	return nil, nil
}

// A momentum content header with no matching entry in the prefetched
// account-blocks must be rejected with an error, not dereference a nil block.
func TestRawMomentumVerifier_Content_MissingAccountBlock_ReturnsError(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	headerHash := types.NewHash([]byte("header"))
	blockHash := types.NewHash([]byte("different-block"))

	momentum := &nom.Momentum{
		Content: nom.MomentumContent{
			{
				Address: address,
				HashHeight: types.HashHeight{
					Hash:   headerHash,
					Height: 1,
				},
			},
		},
	}
	accountBlocks := []*nom.AccountBlock{
		{
			Address: address,
			Hash:    blockHash,
			Height:  1,
		},
	}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: accountBlocks,
		momentumStore: &stubMomentumStore{},
	}

	err := rmv.content(false)
	if err == nil {
		t.Fatal("expected an error for a content header missing its account block, got nil")
	}
}

// A momentum content header must not be satisfied by more than one prefetched
// account-block, or dynamic-plasma price accounting could count it more than once.
func TestRawMomentumVerifier_Content_DuplicateAccountBlock_ReturnsError(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	blockHash := types.NewHash([]byte("block"))

	momentum := &nom.Momentum{
		Content: nom.MomentumContent{
			{
				Address: address,
				HashHeight: types.HashHeight{
					Hash:   blockHash,
					Height: 1,
				},
			},
		},
	}
	block := &nom.AccountBlock{
		Address: address,
		Hash:    blockHash,
		Height:  1,
	}
	accountBlocks := []*nom.AccountBlock{block, block}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: accountBlocks,
		momentumStore: &stubMomentumStore{},
	}

	err := rmv.content(true)
	if err == nil {
		t.Fatal("expected an error for a duplicated prefetched account-block, got nil")
	}
}
