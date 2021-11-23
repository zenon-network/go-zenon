package definition

import (
	"strings"

	"github.com/zenon-network/go-zenon/vm/abi"
)

const (
	jsonLiquidity = `
	[
		{"type":"function","name":"Update", "inputs":[]},
		{"type":"function","name":"Donate", "inputs":[]}
	]`
)

var (
	ABILiquidity = abi.JSONToABIContract(strings.NewReader(jsonLiquidity))
)
