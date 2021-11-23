package types

import (
	"encoding/json"
	"testing"

	"github.com/zenon-network/go-zenon/common"
)

type ComplexStruct struct {
	AH1 AccountHeader
	AH2 *AccountHeader
	AH3 *AccountHeader

	A1 Address
	A2 *Address
	A3 *Address

	H1 Hash
	H2 *Hash
	H3 *Hash

	HH1 HashHeight
	HH2 *HashHeight
	HH3 *HashHeight

	ZTS1 ZenonTokenStandard
	ZTS2 *ZenonTokenStandard
	ZTS3 *ZenonTokenStandard
}

var (
	address1 = ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	address2 = ParseAddressPanic("z1qqmqp40duzvtxvg7dwxph7724mq63t3mru297p")

	zts1 = ParseZTSPanic("zts1znnxxxxxxxxxxxxx9z4ulx")
	zts2 = ParseZTSPanic("zts1qsrxxxxxxxxxxxxxmrhjll")

	hash1 = HexToHashPanic("1dbd7d0b561a41d23c2a469ad42fbd70d5438bae826f6fd607413190c37c363b")
	hash2 = HexToHashPanic("6cddb367afbd583bb48f9bbd7d5ba3b1d0738b4881b1cddd38169526d8158137")
)

func TestJsonSerialization(t *testing.T) {
	c := &ComplexStruct{
		AH1: AccountHeader{
			Address: address1,
			HashHeight: HashHeight{
				Height: 10,
				Hash:   hash1,
			},
		},
		AH2: &AccountHeader{
			Address: address2,
			HashHeight: HashHeight{
				Height: 20,
				Hash:   hash2,
			},
		},
		A1: address1,
		A2: &address2,
		H1: hash1,
		H2: &hash2,
		HH1: HashHeight{
			Height: 1,
			Hash:   hash1,
		},
		HH2: &HashHeight{
			Height: 2,
			Hash:   hash2,
		},
		ZTS1: zts1,
		ZTS2: &zts2,
	}
	common.ExpectJson(t, c, `{
	"AH1": {
		"address": "z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt",
		"hash": "1dbd7d0b561a41d23c2a469ad42fbd70d5438bae826f6fd607413190c37c363b",
		"height": 10
	},
	"AH2": {
		"address": "z1qqmqp40duzvtxvg7dwxph7724mq63t3mru297p",
		"hash": "6cddb367afbd583bb48f9bbd7d5ba3b1d0738b4881b1cddd38169526d8158137",
		"height": 20
	},
	"AH3": null,
	"A1": "z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt",
	"A2": "z1qqmqp40duzvtxvg7dwxph7724mq63t3mru297p",
	"A3": null,
	"H1": "1dbd7d0b561a41d23c2a469ad42fbd70d5438bae826f6fd607413190c37c363b",
	"H2": "6cddb367afbd583bb48f9bbd7d5ba3b1d0738b4881b1cddd38169526d8158137",
	"H3": null,
	"HH1": {
		"hash": "1dbd7d0b561a41d23c2a469ad42fbd70d5438bae826f6fd607413190c37c363b",
		"height": 1
	},
	"HH2": {
		"hash": "6cddb367afbd583bb48f9bbd7d5ba3b1d0738b4881b1cddd38169526d8158137",
		"height": 2
	},
	"HH3": null,
	"ZTS1": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"ZTS2": "zts1qsrxxxxxxxxxxxxxmrhjll",
	"ZTS3": null
}`)
}

func TestJsonDeserialization(t *testing.T) {
	serialized := `{
	"AH1": {
		"address": "z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt",
		"hash": "1dbd7d0b561a41d23c2a469ad42fbd70d5438bae826f6fd607413190c37c363b",
		"height": 10
	},
	"AH2": {
		"address": "z1qqmqp40duzvtxvg7dwxph7724mq63t3mru297p",
		"hash": "6cddb367afbd583bb48f9bbd7d5ba3b1d0738b4881b1cddd38169526d8158137",
		"height": 20
	},
	"AH3": null,
	"A1": "z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt",
	"A2": "z1qqmqp40duzvtxvg7dwxph7724mq63t3mru297p",
	"A3": null,
	"H1": "1dbd7d0b561a41d23c2a469ad42fbd70d5438bae826f6fd607413190c37c363b",
	"H2": "6cddb367afbd583bb48f9bbd7d5ba3b1d0738b4881b1cddd38169526d8158137",
	"H3": null,
	"HH1": {
		"hash": "1dbd7d0b561a41d23c2a469ad42fbd70d5438bae826f6fd607413190c37c363b",
		"height": 1
	},
	"HH2": {
		"hash": "6cddb367afbd583bb48f9bbd7d5ba3b1d0738b4881b1cddd38169526d8158137",
		"height": 2
	},
	"HH3": null,
	"ZTS1": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"ZTS2": "zts1qsrxxxxxxxxxxxxxmrhjll",
	"ZTS3": null
}`

	c := &ComplexStruct{}
	common.FailIfErr(t, json.Unmarshal([]byte(serialized), c))
	common.ExpectJson(t, c, serialized)
}
