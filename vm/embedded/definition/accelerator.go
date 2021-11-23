package definition

import (
	"strings"

	"github.com/zenon-network/go-zenon/vm/abi"
)

const (
	jsonAccelerator = `
	[
		{"type":"function","name":"Donate", "inputs":[]}
	]`
)

var (
	ABIAccelerator = abi.JSONToABIContract(strings.NewReader(jsonAccelerator))
)
