package abi

import (
	"fmt"
	"strings"

	"github.com/zenon-network/go-zenon/common/types"
)

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

func (method Method) Sig() string {
	types := make([]string, len(method.Inputs))
	for i, input := range method.Inputs {
		types[i] = input.Type.String()
	}
	return fmt.Sprintf("%v(%v)", method.Name, strings.Join(types, ","))
}
func (method Method) String() string {
	inputs := make([]string, len(method.Inputs))
	for i, input := range method.Inputs {
		inputs[i] = fmt.Sprintf("%v %v", input.Type, input.Name)
	}
	return fmt.Sprintf("onMessage %v(%v)", method.Name, strings.Join(inputs, ", "))
}
func (method Method) Id() []byte {
	return method.id
}
