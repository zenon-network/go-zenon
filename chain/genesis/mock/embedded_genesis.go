package g

import (
	"encoding/base64"
	"math/big"

	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/zenon-network/go-zenon/chain/genesis"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
)

const (
	// Sunday, September 9, 2001 1:46:40
	genesisTimestamp = 1000000000
	// 10 ^ 8
	Zexp = 100000000
)

var (
	// Secp256k1 keys below.
	// Used to test swap messages for both legacy-register and swap
	// generate with > openssl ecparam -genkey -name secp256k1 -rand /dev/urandom -out priv-2.pem
	//               > openssl ec -in priv-2.pem -text -out pub-2.pem

	Secp1PrvKey    = hexutil.MustDecode("0x7e412f0a36c21518519013a0f9b498f6bbd36b4c9861573e0662680d06cd2a40")
	Secp1PubKeyB64 = base64.StdEncoding.EncodeToString(hexutil.MustDecode("0x047306c325fa7216723bee068c0f2ba1438217c22a0736df41434ac32ba38c04f55c8e3ef9be61377f191016df05ab0fcca8a0b9b505371101c460c59aeede6ab6"))
	Secp1KeyIdHex  = "c955c2b650452d670179068995a51132463e2d13f7519d64ff283af99dd14b43"

	Secp2PrvKey    = hexutil.MustDecode("0xd7ef0ace1c32429605291c09fe4fb3c3dc7dde472e203b29c0911e199713c66e")
	Secp2PubKeyB64 = base64.StdEncoding.EncodeToString(hexutil.MustDecode("0x047e21bcfbb9bba1da40e373922d8a14c19d11bd3b58eb0c2700f29655e389e70c9291e32d929b54ae6efc04ad3e3a82c1ebd8222ccc0e29ea1c4c3fb97f80f4fe"))

	Spork, _   = wallet.DeriveWithIndex(1, hexutil.MustDecode("0x01234567890123456789012345678900"))
	Pillar1, _ = wallet.DeriveWithIndex(1, hexutil.MustDecode("0x01234567890123456789012345678901"))
	Pillar2, _ = wallet.DeriveWithIndex(2, hexutil.MustDecode("0x01234567890123456789012345678901"))
	Pillar3, _ = wallet.DeriveWithIndex(3, hexutil.MustDecode("0x01234567890123456789012345678901"))
	Pillar4, _ = wallet.DeriveWithIndex(4, hexutil.MustDecode("0x01234567890123456789012345678901"))
	Pillar5, _ = wallet.DeriveWithIndex(5, hexutil.MustDecode("0x01234567890123456789012345678901"))
	Pillar6, _ = wallet.DeriveWithIndex(6, hexutil.MustDecode("0x01234567890123456789012345678901"))
	Pillar7, _ = wallet.DeriveWithIndex(7, hexutil.MustDecode("0x01234567890123456789012345678901"))
	Pillar8, _ = wallet.DeriveWithIndex(8, hexutil.MustDecode("0x01234567890123456789012345678901"))

	User1, _  = wallet.DeriveWithIndex(1, hexutil.MustDecode("0x01234567890123456789012345678902"))
	User2, _  = wallet.DeriveWithIndex(2, hexutil.MustDecode("0x01234567890123456789012345678902"))
	User3, _  = wallet.DeriveWithIndex(3, hexutil.MustDecode("0x01234567890123456789012345678902"))
	User4, _  = wallet.DeriveWithIndex(4, hexutil.MustDecode("0x01234567890123456789012345678902"))
	User5, _  = wallet.DeriveWithIndex(5, hexutil.MustDecode("0x01234567890123456789012345678902"))
	User6, _  = wallet.DeriveWithIndex(6, hexutil.MustDecode("0x01234567890123456789012345678902"))
	User7, _  = wallet.DeriveWithIndex(7, hexutil.MustDecode("0x01234567890123456789012345678902"))
	User8, _  = wallet.DeriveWithIndex(8, hexutil.MustDecode("0x01234567890123456789012345678902"))
	User9, _  = wallet.DeriveWithIndex(9, hexutil.MustDecode("0x01234567890123456789012345678902"))
	User10, _ = wallet.DeriveWithIndex(10, hexutil.MustDecode("0x01234567890123456789012345678902"))

	Pillar1Name = "TEST-pillar-1"
	Pillar2Name = "TEST-pillar-cool"
	Pillar3Name = "TEST-pillar-znn"
	Pillar4Name = "TEST-pillar-wewe"
	Pillar5Name = "TEST-pillar-zumba"
	Pillar6Name = "TEST-pillar-6-quasar"
	Pillar7Name = "TEST-pillar-community-7"
	Pillar8Name = "TEST-pillar-eight-eclipse"

	PillarKeys = []*wallet.KeyPair{
		Pillar1,
		Pillar2,
		Pillar3,
		Pillar4,
		Pillar5,
		Pillar6,
		Pillar7,
		Pillar8,
	}

	AllKeyPairs = []*wallet.KeyPair{
		Pillar1,
		Pillar2,
		Pillar3,
		Pillar4,
		Pillar5,
		Pillar6,
		Pillar7,
		Pillar8,
		User1,
		User2,
		User3,
		User4,
		User5,
		User6,
		User7,
		User8,
		User9,
		User10,
		Spork,
	}

	EmbeddedGenesis = &genesis.GenesisConfig{
		ChainIdentifier:     100,
		ExtraData:           "This is the genesis config used for testing",
		GenesisTimestampSec: genesisTimestamp,
		SporkAddress:        &Spork.Address,
		PillarConfig: &genesis.PillarContractConfig{
			Pillars: []*definition.PillarInfo{
				{

					Name:                         Pillar1Name,
					BlockProducingAddress:        Pillar1.Address,
					StakeAddress:                 Pillar1.Address,
					RewardWithdrawAddress:        Pillar1.Address,
					Amount:                       new(big.Int).Set(constants.PillarStakeAmount),
					RegistrationTime:             genesisTimestamp,
					RevokeTime:                   0,
					GiveBlockRewardPercentage:    0,
					GiveDelegateRewardPercentage: 100,
					PillarType:                   definition.LegacyPillarType,
				},
				{

					Name:                         Pillar2Name,
					BlockProducingAddress:        Pillar2.Address,
					StakeAddress:                 Pillar2.Address,
					RewardWithdrawAddress:        Pillar2.Address,
					Amount:                       new(big.Int).Set(constants.PillarStakeAmount),
					RegistrationTime:             genesisTimestamp,
					RevokeTime:                   0,
					GiveBlockRewardPercentage:    0,
					GiveDelegateRewardPercentage: 100,
					PillarType:                   definition.LegacyPillarType,
				},
				{
					Name:                         Pillar3Name,
					BlockProducingAddress:        Pillar3.Address,
					StakeAddress:                 Pillar3.Address,
					RewardWithdrawAddress:        Pillar3.Address,
					Amount:                       new(big.Int).Set(constants.PillarStakeAmount),
					RegistrationTime:             genesisTimestamp,
					RevokeTime:                   0,
					GiveBlockRewardPercentage:    0,
					GiveDelegateRewardPercentage: 100,
					PillarType:                   definition.LegacyPillarType,
				},
			},
			Delegations: []*definition.DelegationInfo{
				{Name: Pillar1Name, Backer: Pillar1.Address},
				{Name: Pillar2Name, Backer: Pillar2.Address},
				{Name: Pillar3Name, Backer: Pillar3.Address},
				{Name: Pillar1Name, Backer: User1.Address},
				{Name: Pillar1Name, Backer: User2.Address},
				{Name: Pillar2Name, Backer: User3.Address},
				{Name: Pillar3Name, Backer: User4.Address},
				{Name: Pillar3Name, Backer: User5.Address},
			},
			LegacyEntries: []*definition.LegacyPillarEntry{
				{
					KeyIdHash:   types.HexToHashPanic(Secp1KeyIdHex),
					PillarCount: 3,
				},
			},
		},
		TokenConfig: &genesis.TokenContractConfig{
			Tokens: []*definition.TokenInfo{
				{

					Owner:         types.PillarContract,
					TokenName:     "Zenon Coin",
					TokenSymbol:   "ZNN",
					TokenDomain:   "zenon.network",
					TotalSupply:   big.NewInt(19500000000000),
					MaxSupply:     big.NewInt(4611686018427387903),
					Decimals:      8,
					IsMintable:    true,
					IsBurnable:    true,
					IsUtility:     true,
					TokenStandard: types.ZnnTokenStandard,
				},
				{

					Owner:         types.StakeContract,
					TokenName:     "QuasarCoin",
					TokenSymbol:   "QSR",
					TokenDomain:   "zenon.network",
					TotalSupply:   big.NewInt(180550000000000),
					MaxSupply:     big.NewInt(4611686018427387903),
					Decimals:      8,
					IsMintable:    true,
					IsBurnable:    true,
					IsUtility:     true,
					TokenStandard: types.QsrTokenStandard,
				},
			},
		},
		PlasmaConfig: &genesis.PlasmaContractConfig{
			Fusions: []*definition.FusionInfo{
				{

					Owner:            User1.Address,
					Amount:           big.NewInt(10000 * Zexp),
					ExpirationHeight: 0,
					Beneficiary:      Pillar1.Address,
				},
				{

					Owner:            User1.Address,
					Amount:           big.NewInt(10000 * Zexp),
					ExpirationHeight: 0,
					Beneficiary:      Pillar2.Address,
				},
				{

					Owner:            User1.Address,
					Amount:           big.NewInt(10000 * Zexp),
					ExpirationHeight: 0,
					Beneficiary:      Pillar3.Address,
				},
				{

					Owner:            User1.Address,
					Amount:           big.NewInt(10000 * Zexp),
					ExpirationHeight: 0,
					Beneficiary:      Pillar4.Address,
				},
				{

					Owner:            User1.Address,
					Amount:           big.NewInt(10000 * Zexp),
					ExpirationHeight: 0,
					Beneficiary:      Pillar5.Address,
				},
				{

					Owner:            User1.Address,
					Amount:           big.NewInt(10000 * Zexp),
					ExpirationHeight: 0,
					Beneficiary:      Pillar6.Address,
				},
				{

					Owner:            User1.Address,
					Amount:           big.NewInt(10000 * Zexp),
					ExpirationHeight: 0,
					Beneficiary:      Pillar7.Address,
				},
				{

					Owner:            User1.Address,
					Amount:           big.NewInt(10000 * Zexp),
					ExpirationHeight: 0,
					Beneficiary:      Pillar8.Address,
				},
				{

					Owner:            User1.Address,
					Amount:           big.NewInt(10000 * Zexp),
					ExpirationHeight: 0,
					Beneficiary:      Spork.Address,
				},
				{

					Owner:            User1.Address,
					Amount:           big.NewInt(10000 * Zexp),
					Id:               types.HexToHashPanic("117613e734b6cb0fd7b7583f5b0e863a3f0c856cd32fa36f1b60b464d068c5a6"),
					ExpirationHeight: 0,
					Beneficiary:      User1.Address,
				},
				{

					Owner:            User2.Address,
					Amount:           big.NewInt(10000 * Zexp),
					Id:               types.HexToHashPanic("3d3179e499f839b47c60216b57f79e41264d408e2f21aa6f5462f25d5e094924"),
					ExpirationHeight: 0,
					Beneficiary:      User2.Address,
				},
				{

					Owner:            User3.Address,
					Amount:           big.NewInt(10000 * Zexp),
					ExpirationHeight: 0,
					Beneficiary:      User3.Address,
				},
				{

					Owner:            User4.Address,
					Amount:           big.NewInt(10000 * Zexp),
					ExpirationHeight: 0,
					Beneficiary:      User4.Address,
				},
				{

					Owner:            User5.Address,
					Amount:           big.NewInt(10000 * Zexp),
					ExpirationHeight: 0,
					Beneficiary:      User5.Address,
				},
			},
		},
		GenesisBlocks: &genesis.GenesisBlocksConfig{
			Blocks: []*genesis.GenesisBlockConfig{
				{
					Address: types.PillarContract,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(3 * 15000 * Zexp),
					},
				},
				{
					Address: types.PlasmaContract,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.QsrTokenStandard: big.NewInt(14 * 10000 * Zexp),
					},
				},
				{
					Address: Pillar1.Address,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(1000 * Zexp),
					},
				},
				{
					Address: Pillar2.Address,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(1000 * Zexp),
					},
				},
				{
					Address: Pillar3.Address,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(1000 * Zexp),
					},
				},
				{
					Address: Pillar4.Address,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(16000 * Zexp),
						types.QsrTokenStandard: big.NewInt(200000 * Zexp),
					},
				},
				{
					Address: Pillar5.Address,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(16000 * Zexp),
						types.QsrTokenStandard: big.NewInt(200000 * Zexp),
					},
				},
				{
					Address: Pillar6.Address,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(16000 * Zexp),
						types.QsrTokenStandard: big.NewInt(200000 * Zexp),
					},
				},
				{
					Address: Pillar7.Address,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(16000 * Zexp),
						types.QsrTokenStandard: big.NewInt(200000 * Zexp),
					},
				},
				{
					Address: Pillar8.Address,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(16000 * Zexp),
						types.QsrTokenStandard: big.NewInt(200000 * Zexp),
					},
				},
				{
					Address: User1.Address,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(12000 * Zexp),
						types.QsrTokenStandard: big.NewInt(120000 * Zexp),
					},
				},
				{
					Address: User2.Address,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(8000 * Zexp),
						types.QsrTokenStandard: big.NewInt(80000 * Zexp),
					},
				},
				{
					Address: User3.Address,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(1000 * Zexp),
						types.QsrTokenStandard: big.NewInt(10000 * Zexp),
					},
				},
				{
					Address: User4.Address,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(500 * Zexp),
						types.QsrTokenStandard: big.NewInt(500 * Zexp),
					},
				},
				{
					Address: User5.Address,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(500 * Zexp),
						types.QsrTokenStandard: big.NewInt(5000 * Zexp),
					},
				},
				{
					Address: Spork.Address,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(45000 * Zexp),
						types.QsrTokenStandard: big.NewInt(450000 * Zexp),
					},
				},
			},
		},
		SwapConfig: &genesis.SwapContractConfig{
			Entries: []*definition.SwapAssets{
				{
					KeyIdHash: types.HexToHashPanic(Secp1KeyIdHex),
					Znn:       big.NewInt(15000 * Zexp),
					Qsr:       big.NewInt(150000 * Zexp),
				},
			},
		},
	}
)
