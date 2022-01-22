package genesis

import (
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

var (
	embeddedGenesisHash = types.HexToHashPanic("9e204601d1b7b1427fe12bc82622e610d8a6ad43c40abf020eb66e538bb8eeb0")
)

func TestFieldsExist(t *testing.T) {
	g := embeddedGenesis
	if err := CheckFieldsExist(g); err != nil {
		t.Fatal(err)
	}
}
func TestPlasmaInfo(t *testing.T) {
	g := embeddedGenesis
	if err := CheckPlasmaInfo(g); err != nil {
		t.Fatal(err)
	}
}
func TestSwapAccount(t *testing.T) {
	g := embeddedGenesis
	if err := CheckSwapAccount(g); err != nil {
		t.Fatal(err)
	}
}
func TestPillarBalance(t *testing.T) {
	g := embeddedGenesis
	if err := CheckPillarBalance(g); err != nil {
		t.Fatal(err)
	}
}
func TestTokenTotalSupply(t *testing.T) {
	g := embeddedGenesis
	if err := CheckTokenTotalSupply(g); err != nil {
		t.Fatal(err)
	}
}
func TestGenesisCheckSum(t *testing.T) {
	common.FailIfErr(t, CheckGenesisCheckSum(embeddedGenesis, embeddedGenesisHash))
}
