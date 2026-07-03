// Package abi packs and unpacks the call data and stored variables of
// the embedded contracts. It is derived from go-ethereum's abi package
// and keeps the Ethereum ABI wire format: every static value occupies
// one big-endian 32-byte word (WordSize), while dynamic values
// (string, bytes, slices) are stored out-of-line behind a word holding
// their byte offset, followed at that offset by a length word and the
// right-padded payload. Fixed-size arrays are encoded inline, element
// by element.
//
// On top of the Ethereum types the package adds three Zenon-specific
// argument types: "address" (types.Address), "tokenStandard"
// (types.ZenonTokenStandard) and "hash" (types.Hash), all packed
// left-padded into a single word like integers (fixed-size byte
// arrays, by contrast, pack right-padded).
//
// Each embedded contract declares its interface as a JSON array
// (parsed by JSONToABIContract) of "function" entries — the callable
// methods, dispatched by a 4-byte selector — and "variable" entries,
// which describe the layout of values the contract serializes into
// its key-value storage. A method's selector is the first 4 bytes of
// the SHA3-256 hash of its canonical signature, e.g.
// "Register(string,address,address)" (note: SHA3-256, not the
// Keccak256 used by Ethereum).
//
// vm/embedded/definition holds the JSON definitions and uses
// PackMethod/UnpackMethod for call data and
// PackVariable/UnpackVariable for contract state.
package abi

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/zenon-network/go-zenon/common"
)

// ABIContract is the parsed interface of one embedded contract: its
// callable methods and the named variable layouts used to serialize
// contract state, both keyed by name.
type ABIContract struct {
	Methods   map[string]Method
	Variables map[string]Variable
}

// JSONToABIContract parses a JSON ABI definition (an array of
// "function" and "variable" entries with their typed inputs) into an
// ABIContract. The definitions are compiled into the binary, so a
// malformed one halts the node via common.DealWithErr instead of
// returning an error.
func JSONToABIContract(reader io.Reader) ABIContract {
	dec := json.NewDecoder(reader)

	var abi ABIContract
	if err := dec.Decode(&abi); err != nil {
		common.DealWithErr(err)
		return ABIContract{}
	}

	return abi
}

// PackMethod encodes a call to the named method: the method's 4-byte
// selector followed by args packed per the ABI encoding. It fails if
// the method does not exist or if args do not match the method's
// inputs in count or type.
func (abi ABIContract) PackMethod(name string, args ...interface{}) ([]byte, error) {
	method, exist := abi.Methods[name]
	if !exist {
		return nil, errMethodNotFound(name)
	}
	arguments, err := method.Inputs.Pack(args...)
	if err != nil {
		return nil, err
	}
	// Pack up the method ID too if not a constructor and return
	return append(method.Id(), arguments...), nil
}

// PackMethodPanic is PackMethod for statically known calls: it panics
// instead of returning an error.
func (abi ABIContract) PackMethodPanic(name string, args ...interface{}) []byte {
	data, err := abi.PackMethod(name, args...)
	if err != nil {
		panic(err)
	}
	return data
}

// PackVariable encodes args per the named variable's layout, with no
// selector prefix; the result is what embedded contracts store in
// their key-value storage. It fails if the variable does not exist or
// if args do not match its inputs.
func (abi ABIContract) PackVariable(name string, args ...interface{}) ([]byte, error) {
	variable, exist := abi.Variables[name]
	if !exist {
		return nil, errVariableNotFound(name)
	}

	return variable.Inputs.Pack(args...)
}

// PackVariablePanic is PackVariable for statically known values: it
// panics instead of returning an error.
func (abi ABIContract) PackVariablePanic(name string, args ...interface{}) []byte {
	data, err := abi.PackVariable(name, args...)
	if err != nil {
		panic(err)
	}
	return data
}

// UnpackMethod decodes the call data of the named method into v, a
// pointer to a struct whose fields match the method's inputs (paired
// by capitalised name or an abi:"name" struct tag), or a pointer to
// the bare value for single-input methods. It fails if input is
// 4 bytes or shorter, or if the selector does not resolve to the
// named method.
func (abi ABIContract) UnpackMethod(v interface{}, name string, input []byte) (err error) {
	if len(input) <= 4 {
		return errEmptyInput
	}
	if method, err := abi.MethodById(input[0:4]); err == nil && method.Name == name {
		return method.Inputs.Unpack(v, input[4:])
	}
	return errCouldNotLocateNamedMethod
}

// UnpackEmptyMethod checks the call data of a method that takes no
// arguments: input must be exactly the 4-byte selector of the named
// method. It fails if input is shorter, longer, or selects a
// different method.
func (abi ABIContract) UnpackEmptyMethod(name string, input []byte) (err error) {
	if len(input) < 4 {
		return errEmptyInput
	} else if len(input) > 4 {
		return errInputTooLong
	}
	if method, err := abi.MethodById(input[0:4]); err == nil && method.Name == name {
		return nil
	}
	return errCouldNotLocateNamedMethod
}

// UnpackVariable decodes a stored contract value laid out per the
// named variable into v, a pointer to a matching struct (or to the
// bare value for single-input variables). There is no selector:
// input is decoded from offset 0. It fails on empty input or an
// unknown variable name.
func (abi ABIContract) UnpackVariable(v interface{}, name string, input []byte) (err error) {
	if len(input) == 0 {
		return errEmptyInput
	}
	if variable, ok := abi.Variables[name]; ok {
		return variable.Inputs.Unpack(v, input)
	}
	return errCouldNotLocateNamedVariable
}

// UnpackVariablePanic is UnpackVariable for values the contract wrote
// itself and therefore trusts: it halts the node via
// common.DealWithErr instead of returning an error.
func (abi ABIContract) UnpackVariablePanic(v interface{}, name string, input []byte) {
	common.DealWithErr(abi.UnpackVariable(v, name, input))
}

// MethodById looks up a method by the 4-byte id
// returns nil if none found
func (abi *ABIContract) MethodById(sigdata []byte) (*Method, error) {
	if len(sigdata) < 4 {
		return nil, errMethodIdNotSpecified
	}
	for _, method := range abi.Methods {
		if bytes.Equal(method.Id(), sigdata[:4]) {
			return &method, nil
		}
	}
	return nil, errNoMethodId(sigdata[:4])
}

// UnmarshalJSON implements json.Unmarshaler interface
func (abi *ABIContract) UnmarshalJSON(data []byte) error {
	var fields []struct {
		Type    string
		Name    string
		Inputs  []Argument
		Outputs []Argument
	}

	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}

	abi.Methods = make(map[string]Method)
	abi.Variables = make(map[string]Variable)
	for _, field := range fields {
		switch field.Type {
		case "function":
			abi.Methods[field.Name] = newMethod(field.Name, field.Inputs)
		case "variable":
			if len(field.Inputs) == 0 {
				return errInvalidEmptyVariableInput
			}
			abi.Variables[field.Name] = Variable{
				Name:   field.Name,
				Inputs: field.Inputs,
			}
		}
	}
	return nil
}
