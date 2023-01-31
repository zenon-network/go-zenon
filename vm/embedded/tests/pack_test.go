package tests

import (
	"crypto/sha256"
	"math/big"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/vm/abi"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

const (
	jsonPack = `
	[{"type":"variable","name":"ComplexDataStructure","inputs":[
			{"name":"title","type":"string"},
			{"name":"keys","type":"string[]"},
			{"name":"values1","type":"string[]"},
			{"name":"values2","type":"uint256[]"}
		]}
	]`

	ComplexDataStructureVariableName = "ComplexDataStructure"

	charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

var (
	ABIMock = abi.JSONToABIContract(strings.NewReader(jsonPack))

	complexDataStructureKeyPrefix = []byte{1}
	seededRand                    = rand.New(rand.NewSource(time.Now().UnixNano()))
)

type ComplexDataStructureVariable struct {
	Title   string
	Keys    []string
	Values1 []string
	Values2 []*big.Int
}

func (adt *ComplexDataStructureVariable) Save(context db.DB) error {
	data, err := ABIMock.PackVariable(
		ComplexDataStructureVariableName,
		adt.Title,
		adt.Keys,
		adt.Values1,
		adt.Values2,
	)
	if err != nil {
		return err
	}
	return context.Put(
		adt.Key(),
		data,
	)
}
func (adt *ComplexDataStructureVariable) Key() []byte {
	h := sha256.New()
	h.Write([]byte(adt.Title))
	networkHash := h.Sum(nil)
	return common.JoinBytes(complexDataStructureKeyPrefix, networkHash)
}
func (adt *ComplexDataStructureVariable) Delete(context db.DB) error {
	return context.Delete(adt.Key())
}
func parseStringArrayVariable(data []byte) (*ComplexDataStructureVariable, error) {
	if len(data) > 0 {
		networkInfo := new(ComplexDataStructureVariable)
		if err := ABIMock.UnpackVariable(networkInfo, ComplexDataStructureVariableName, data); err != nil {
			return nil, err
		}
		return networkInfo, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}
func GetComplexDataStructureVariable(context db.DB, title string) (*ComplexDataStructureVariable, error) {
	sA := &ComplexDataStructureVariable{Title: title}
	if data, err := context.Get(sA.Key()); err != nil {
		return nil, err
	} else {
		upd, err := parseStringArrayVariable(data)
		if err == constants.ErrDataNonExistent {
			return &ComplexDataStructureVariable{Title: "", Keys: nil, Values1: nil, Values2: nil}, nil
		}
		return upd, err
	}
}

func StringWithCharset(length int, charset string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func String(length int) string {
	return StringWithCharset(length, charset)
}

func TestPack_SimpleTest(t *testing.T) {
	context := vm_context.NewGenesisMomentumVMContext()
	storage := context.GetAccountStore(g.User1.Address).Storage()
	variable, err := GetComplexDataStructureVariable(storage, "znn")
	assert.Nil(t, err)
	assert.Equal(t, &ComplexDataStructureVariable{Title: "", Keys: nil, Values1: nil, Values2: nil}, variable)
	keys := make([]string, 0)
	values1 := make([]string, 0)
	values2 := make([]*big.Int, 0)
	for i := 1; i < 10; i++ {
		keys = append(keys, String(i))
		length := rand.Intn(1000)
		values1 = append(values1, String(length))
		values2 = append(values2, big.NewInt(int64(length*g.Zexp)))
	}
	refVariable := &ComplexDataStructureVariable{Title: "znn", Keys: keys, Values1: values1, Values2: values2}
	err = refVariable.Save(storage)
	assert.Nil(t, err)
	variable, err = GetComplexDataStructureVariable(storage, "znn")
	assert.Nil(t, err)
	assert.Equal(t, refVariable, variable)
}

func generateComplexDataStructure(title string) *ComplexDataStructureVariable {
	keys := make([]string, 0)
	values1 := make([]string, 0)
	values2 := make([]*big.Int, 0)
	size := rand.Intn(1000)
	for i := 1; i < size; i++ {
		keys = append(keys, String(i))
	}
	size = rand.Intn(1000)
	for i := 1; i < size; i++ {
		length := rand.Intn(1000)
		values1 = append(values1, String(length))
	}
	size = rand.Intn(1000)
	for i := 1; i < size; i++ {
		length := rand.Intn(g.Zexp)
		values2 = append(values2, big.NewInt(int64(length*g.Zexp)))
	}
	variable := &ComplexDataStructureVariable{Title: title, Keys: keys, Values1: values1, Values2: values2}
	return variable
}

func TestPack_ComplexTest(t *testing.T) {
	context := vm_context.NewGenesisMomentumVMContext()
	storage := context.GetAccountStore(g.User1.Address).Storage()
	variable, err := GetComplexDataStructureVariable(storage, "znn")
	assert.Nil(t, err)
	assert.Equal(t, &ComplexDataStructureVariable{Title: "", Keys: nil, Values1: nil, Values2: nil}, variable)
	znnRefVariable := generateComplexDataStructure("znn")
	err = znnRefVariable.Save(storage)
	assert.Nil(t, err)
	qsrRefVariable := generateComplexDataStructure("qsr")
	err = qsrRefVariable.Save(storage)
	assert.Nil(t, err)
	variable, err = GetComplexDataStructureVariable(storage, "znn")
	assert.Nil(t, err)
	assert.Equal(t, znnRefVariable, variable)
	variable, err = GetComplexDataStructureVariable(storage, "qsr")
	assert.Nil(t, err)
	assert.Equal(t, qsrRefVariable, variable)
}
