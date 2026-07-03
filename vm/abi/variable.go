package abi

import (
	"fmt"
	"strings"
)

// Variable describes the layout of one kind of value an embedded
// contract serializes into its key-value storage — a named tuple of
// typed fields, encoded by PackVariable and decoded by
// UnpackVariable. Variables are a Zenon extension with no Ethereum
// ABI counterpart and have no selector.
type Variable struct {
	Name   string
	Inputs Arguments
}

// String renders the variable like a struct declaration, e.g.
// "struct fusionInfo { uint256 amount,uint64 expirationHeight,address beneficiary }".
func (v Variable) String() string {
	inputs := make([]string, len(v.Inputs))
	for i, input := range v.Inputs {
		inputs[i] = fmt.Sprintf("%v %v", input.Type, input.Name)
	}
	return fmt.Sprintf("struct %v { %v }", v.Name, strings.Join(inputs, ","))
}
