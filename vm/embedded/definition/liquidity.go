package definition

import (
	"math/big"
	"strings"

	"github.com/zenon-network/go-zenon/vm/abi"
)

const (
	jsonLiquidity = `
	[
		{"type":"function","name":"Update", "inputs":[]},
		{"type":"function","name":"Donate", "inputs":[]},
		{"type":"function","name":"Fund", "inputs":[
			{"name":"znnReward","type":"uint256"},
			{"name":"qsrReward","type":"uint256"}
		]},
		{"type":"function","name":"BurnZnn", "inputs":[
			{"name":"burnAmount","type":"uint256"}
		]}
	]`

	FundMethodName    = "Fund"
	BurnZnnMethodName = "BurnZnn"
)

var (
	ABILiquidity = abi.JSONToABIContract(strings.NewReader(jsonLiquidity))
)

type FundParam struct {
	ZnnReward *big.Int
	QsrReward *big.Int
}

type BurnParam struct {
	BurnAmount *big.Int
}
