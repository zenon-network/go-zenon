package abi

import (
	"fmt"
	"strings"

	"github.com/zenon-network/go-zenon/common/types"
)

// Method is one callable function of an embedded contract: its name,
// its typed inputs, and the 4-byte selector derived from them at
// parse time. Embedded methods have no ABI-level outputs; results are
// delivered as send blocks issued by the contract.
type Method struct {
	Name   string
	id     []byte
	Inputs Arguments
}

func newMethod(name string, inputs Arguments) Method {
	m := Method{
		Name:   name,
		Inputs: inputs,
	}
	m.id = types.NewHash([]byte(m.Sig())).Bytes()[:4]
	return m
}

// Sig returns the canonical signature the selector is hashed from:
// the method name followed by the comma-joined input types in
// parentheses, e.g. "Fuse(address)".
func (method Method) Sig() string {
	types := make([]string, len(method.Inputs))
	for i, input := range method.Inputs {
		types[i] = input.Type.String()
	}
	return fmt.Sprintf("%v(%v)", method.Name, strings.Join(types, ","))
}

// String renders the method with named parameters for human
// consumption, e.g. "onMessage Fuse(address address)".
func (method Method) String() string {
	inputs := make([]string, len(method.Inputs))
	for i, input := range method.Inputs {
		inputs[i] = fmt.Sprintf("%v %v", input.Type, input.Name)
	}
	return fmt.Sprintf("onMessage %v(%v)", method.Name, strings.Join(inputs, ", "))
}

// Id returns the method's selector: the first 4 bytes of the
// SHA3-256 hash (types.NewHash) of Sig(). Call data targeting an
// embedded contract starts with these bytes.
func (method Method) Id() []byte {
	return method.id
}
