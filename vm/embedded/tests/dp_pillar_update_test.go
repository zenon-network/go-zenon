package tests

import (
	"fmt"
	"strings"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func setUpdateMinNumMomentums(t *testing.T, momentums uint64) {
	saved := constants.UpdateMinNumMomentums
	constants.UpdateMinNumMomentums = momentums
	t.Cleanup(func() {
		constants.UpdateMinNumMomentums = saved
	})
}

func frontierMomentum(t *testing.T, z mock.MockZenon) *nom.Momentum {
	momentum, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)
	return momentum
}

func frontierDynamicPlasma(t *testing.T, z mock.MockZenon) dp.DynamicPlasma {
	store := z.Chain().GetFrontierMomentumStore()
	config, err := store.GetPlasmaVariables()
	common.FailIfErr(t, err)
	return dp.NewDynamicPlasma(frontierMomentum(t, z), config)
}

func sendUserBlocksAtTwentyTimesMinimumPrice(z mock.MockZenon, count int) {
	for i := 0; i < count; i++ {
		z.InsertSendBlock(&nom.AccountBlock{
			Address:       g.User1.Address,
			ToAddress:     g.User2.Address,
			TokenStandard: types.ZeroTokenStandard,
			Amount:        common.Big0,
			FusedPlasma:   20 * constants.AccountBlockBasePlasma,
		}, nil, mock.SkipVmChanges)
	}
}

func unconfirmedPillarBlocks(z mock.MockZenon) []*nom.AccountBlock {
	var blocks []*nom.AccountBlock
	for _, key := range g.PillarKeys {
		blocks = append(blocks, z.Chain().GetUncommittedAccountBlocksByAddress(key.Address)...)
	}
	return blocks
}

func isUpdateCall(block *nom.AccountBlock) bool {
	return types.IsEmbeddedAddress(block.ToAddress) && string(block.Data) == string(definition.ABICommon.PackMethodPanic(definition.UpdateMethodName))
}

func TestDynamicPlasma_PillarUpdateConfirmsAboveMinimumPrice(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	savePlasmaDefaults(t)

	definition.DefaultFusedPlasmaTarget = 10500

	activateDynamicPlasma(t, z)
	for i := 0; i < 8; i++ {
		sendUserBlocksAtTwentyTimesMinimumPrice(z, 2)
		z.InsertNewMomentum()
	}
	setUpdateMinNumMomentums(t, frontierMomentum(t, z).Height)

	sendUserBlocksAtTwentyTimesMinimumPrice(z, 2)
	z.InsertNewMomentum()

	updates := unconfirmedPillarBlocks(z)
	if len(updates) == 0 {
		t.Fatalf("no pillar sent update blocks")
	}
	producer := updates[0].Address
	for _, update := range updates {
		if update.Address != producer || !isUpdateCall(update) {
			t.Fatalf("unexpected unconfirmed pillar block %v to %v", update.Header(), update.ToAddress)
		}
	}
	later := z.InsertSendBlock(&nom.AccountBlock{
		Address:       producer,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZeroTokenStandard,
		Amount:        common.Big0,
		FusedPlasma:   20 * constants.AccountBlockBasePlasma,
	}, nil, mock.SkipVmChanges)

	sendUserBlocksAtTwentyTimesMinimumPrice(z, 2)
	z.InsertNewMomentum()
	sendUserBlocksAtTwentyTimesMinimumPrice(z, 2)
	z.InsertNewMomentum()

	if price := frontierMomentum(t, z).NextFusionPrice; price <= 2*dp.MinResourcePrice {
		t.Fatalf("fusion price %v is not above twice the minimum", price)
	}
	store := z.Chain().GetFrontierMomentumStore()
	for _, block := range append(updates, later) {
		confirmation, err := store.GetBlockConfirmationHeight(block.Hash)
		common.FailIfErr(t, err)
		if confirmation == 0 {
			t.Fatalf("block %v to %v paying %v for base %v is not confirmed", block.Header(), block.ToAddress, block.FusedPlasma, block.BasePlasma)
		}
	}
}

func TestDynamicPlasma_PillarUpdateReplacedAtSameHeightWhenUnderpriced(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	savePlasmaDefaults(t)

	definition.DefaultMaxBasePlasmaInMomentum = 5 * constants.AccountBlockBasePlasma
	definition.DefaultFusedPlasmaTarget = 10500

	activateDynamicPlasma(t, z)
	setUpdateMinNumMomentums(t, frontierMomentum(t, z).Height)

	sendUserBlocksAtTwentyTimesMinimumPrice(z, 5)
	z.InsertNewMomentum()

	sent := unconfirmedPillarBlocks(z)
	if len(sent) == 0 {
		t.Fatalf("no pillar sent update blocks")
	}
	producer := sent[0].Address
	sentByHeight := make(map[uint64]*nom.AccountBlock)
	for _, update := range sent {
		if update.Address != producer || !isUpdateCall(update) {
			t.Fatalf("unexpected unconfirmed pillar block %v to %v", update.Header(), update.ToAddress)
		}
		sentByHeight[update.Height] = update
	}

	for i := 0; i < 12; i++ {
		sendUserBlocksAtTwentyTimesMinimumPrice(z, 5)
		z.InsertNewMomentum()
	}

	plasma := frontierDynamicPlasma(t, z)
	if plasma.ValidPrice(sent[0]) {
		t.Fatalf("the first update block still pays the floor; the price did not move enough")
	}
	pending := z.Chain().GetUncommittedAccountBlocksByAddress(producer)
	if len(pending) != len(sent) {
		t.Fatalf("producer has %v unconfirmed blocks, sent %v", len(pending), len(sent))
	}
	contracts := make(map[types.Address]bool)
	for _, block := range pending {
		original, ok := sentByHeight[block.Height]
		if !ok {
			t.Fatalf("block %v is at a new height", block.Header())
		}
		if !isUpdateCall(block) || block.ToAddress != original.ToAddress || contracts[block.ToAddress] {
			t.Fatalf("block %v to %v does not stand for the update sent at its height", block.Header(), block.ToAddress)
		}
		contracts[block.ToAddress] = true
		if block.Hash == original.Hash {
			t.Fatalf("update block %v paying %v was not replaced", block.Header(), block.FusedPlasma)
		}
		if block.FusedPlasma <= original.FusedPlasma {
			t.Fatalf("replacement %v pays %v, the original paid %v", block.Header(), block.FusedPlasma, original.FusedPlasma)
		}
	}
}

func TestPillar_UpdateBlocksUnchangedWithoutDynamicPlasma(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	setUpdateMinNumMomentums(t, 5)

	z.InsertMomentumsTo(12)

	var blocks strings.Builder
	for _, key := range g.PillarKeys {
		account := z.Chain().GetFrontierAccountStore(key.Address)
		for height := uint64(1); height <= account.Identifier().Height; height++ {
			block, err := account.ByHeight(height)
			common.FailIfErr(t, err)
			fmt.Fprintf(&blocks, "%v %v %v %v %v %v\n", block.Address, block.Height, block.ToAddress, block.FusedPlasma, block.BasePlasma, block.Hash)
		}
	}
	common.ExpectString(t, blocks.String(), `
z1qqq43dyrswfehx9w9td43exflqzcxrt7g6alah 1 z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f 0 0 ae2bba98ff2278e351db48e1dca935e8a2a1d72d7fb8305ee9ebbd49be7723a4
z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju 1 z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f 0 0 698b28c6d8f4e6f0db36bd66f16ff962ce1eb4521dd1bd7e103b87b4c8562904
z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju 2 z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg 52500 52500 06aac631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb953
z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju 3 z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 52500 52500 7619eba116808daecc1728501a29c61d68dc74eaf32bb88c67e84ab4b4f7209c
z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju 4 z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r 52500 52500 a832e02ac1993cfd0f7e161c55942c5c01ba8363d159595e22dabc4797a67089
z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju 5 z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae 52500 52500 fd25b02b5ede40a79472db0134824990f7cc152fab0a5d8b9f65cb21ae7f20f3
z1qqc8hqalt8je538849rf78nhgek30axq8h0g69 1 z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f 0 0 ce8513d0b0e77092cda68c5e76075178c6d35f7c4c0550cab73aa0253c77bfbe
z1qqc8hqalt8je538849rf78nhgek30axq8h0g69 2 z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg 52500 52500 a70d0443104424ff8389477099637cc3e4cb4b191fde00d90414427f78a91bef
z1qqc8hqalt8je538849rf78nhgek30axq8h0g69 3 z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 52500 52500 414a4b87d689db5b1cd23f0c0df11fcab661a9634b8f8d295fa80ec1b6d42d9b
z1qqc8hqalt8je538849rf78nhgek30axq8h0g69 4 z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r 52500 52500 c86a0d115ebf9752f6d3ef1f431b2db81647bfa0b29910088689026a9238fee5
z1qqc8hqalt8je538849rf78nhgek30axq8h0g69 5 z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae 52500 52500 3d6faa5c3b883cf3f966c0d0812e9ceb6430378fd493a82fbfd37dd66c77b4da
z1qplpsv3wcm64js30jlumxlatgxxkqr6hgv30fg 1 z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f 0 0 f951dee29e8c90304ff631bcb4af538900f99c64b62a11f5fc13d7a00f58e7e3
z1qzv6ch3znujldgkq3krlzq38hu5n2pqg3xsjgv 1 z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f 0 0 5f481733f2c898b3e59c4f0c073216dc7159ffa01585ed64e6cf280096b3f773
z1qqgrqcklnx08k8qwxvrryj9f92gngmcm7ltgms 1 z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f 0 0 8ad42e3bdc3d1359d91edf844409cfbf919d1e0f59efab9f62ac40856f0fe506
z1qq56p6e9s6emkjj689ayz05laz466nwxme4r8z 1 z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f 0 0 2f6a23f62d6b8551857f331ef878f23f35c4b0806d1f5d41a1e01485ead35cac
z1qqqcn34kcg8gy7hcuqs7d7mu6eq4mwryftgads 1 z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f 0 0 5862407c11aaa61c4758bee2ec5800cd167b74104935ee803cba21118be9fba0
`)
}
