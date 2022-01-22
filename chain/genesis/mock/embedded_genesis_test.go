package g

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/genesis"
	"github.com/zenon-network/go-zenon/common/types"
)

func TestGenesisFieldsExist(t *testing.T) {
	g := EmbeddedGenesis
	if err := genesis.CheckFieldsExist(g); err != nil {
		t.Fatal(err)
	}
}
func TestGenesisPlasmaInfo(t *testing.T) {
	g := EmbeddedGenesis
	if err := genesis.CheckPlasmaInfo(g); err != nil {
		t.Fatal(err)
	}
}
func TestGenesisSwapAccount(t *testing.T) {
	g := EmbeddedGenesis
	if err := genesis.CheckSwapAccount(g); err != nil {
		t.Fatal(err)
	}
}
func TestGenesisPillarBalance(t *testing.T) {
	g := EmbeddedGenesis
	if err := genesis.CheckPillarBalance(g); err != nil {
		t.Fatal(err)
	}
}
func TestGenesisTokenTotalSupply(t *testing.T) {
	g := EmbeddedGenesis
	if err := genesis.CheckTokenTotalSupply(g); err != nil {
		t.Fatal(err)
	}
}
func TestGenesisCheckSum(t *testing.T) {
	g := EmbeddedGenesis
	// manually set up chain identifier
	if err := genesis.CheckGenesisCheckSum(g, types.HexToHashPanic("0385d849ee33b94c8783288c148e3ae741c2ecec98b08b3f59d6bcc219168fe5")); err != nil {
		t.Fatal(err)
	}
}
