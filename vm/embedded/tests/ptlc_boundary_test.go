package tests

import (
	"bytes"
	"math/big"
	"strings"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	rpc "github.com/zenon-network/go-zenon/rpc/server"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func ptlcFrontier(t *testing.T, z mock.MockZenon) *nom.Momentum {
	t.Helper()
	frontier, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)
	return frontier
}

func ptlcZnnBalance(t *testing.T, z mock.MockZenon, address types.Address) *big.Int {
	t.Helper()
	balance, err := z.Chain().GetFrontierMomentumStore().GetAccountStore(address).GetBalance(types.ZnnTokenStandard)
	common.FailIfErr(t, err)
	return balance
}

func TestPtlc_enforcementHeightBoundary(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	enforcementHeight := startPtlcActivation(t, z)

	unknown := types.NewHash([]byte("no such entry"))
	witness := bytes.Repeat([]byte{1}, 64)
	calls := map[string]func() *nom.AccountBlock{
		definition.CreatePtlcMethodName: func() *nom.AccountBlock {
			return &nom.AccountBlock{
				Address:   g.User1.Address,
				ToAddress: types.PtlcContract,
				Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
					int64(genesisTimestamp+1000),
					definition.PointTypeED25519,
					g.User2.Public,
					types.ZeroAddress,
				),
				TokenStandard: types.ZnnTokenStandard,
				Amount:        big.NewInt(10 * g.Zexp),
			}
		},
		definition.UnlockPtlcMethodName: func() *nom.AccountBlock {
			return &nom.AccountBlock{
				Address:   g.User2.Address,
				ToAddress: types.PtlcContract,
				Data:      definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, unknown, witness),
			}
		},
		definition.ProxyUnlockPtlcMethodName: func() *nom.AccountBlock {
			return &nom.AccountBlock{
				Address:   g.User3.Address,
				ToAddress: types.PtlcContract,
				Data:      definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName, unknown, g.User2.Address, witness),
			}
		},
		definition.ReclaimPtlcMethodName: func() *nom.AccountBlock {
			return &nom.AccountBlock{
				Address:   g.User4.Address,
				ToAddress: types.PtlcContract,
				Data:      definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, unknown),
			}
		},
	}
	if len(calls) != len(definition.ABIPtlc.Methods) {
		t.Fatalf("%d calls for %d methods", len(calls), len(definition.ABIPtlc.Methods))
	}

	z.InsertMomentumsTo(enforcementHeight - 1)
	if height := ptlcFrontier(t, z).Height; height != enforcementHeight-1 {
		t.Fatalf("frontier is %d, want %d", height, enforcementHeight-1)
	}
	for name, call := range calls {
		if block := z.InsertSendBlock(call(), constants.ErrContractDoesntExist, mock.NoVmChanges); block != nil {
			t.Fatalf("%s was taken one momentum before the enforcement height", name)
		}
	}
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)

	z.InsertNewMomentum()
	if height := ptlcFrontier(t, z).Height; height != enforcementHeight {
		t.Fatalf("frontier is %d, want %d", height, enforcementHeight)
	}
	for name, call := range calls {
		if block := z.InsertSendBlock(call(), nil, mock.SkipVmChanges); block == nil {
			t.Fatalf("%s was not taken at the enforcement height", name)
		}
	}
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
}

func TestPtlc_createExpiresBeforeConfirmation(t *testing.T) {
	z := mock.NewMockZenon(t)
	ptlcApi := embedded.NewPtlcApi(z)
	defer z.StopPanic()
	activatePtlc(t, z)

	create := func(expirationTime int64) *nom.AccountBlock {
		return &nom.AccountBlock{
			Address:   g.User1.Address,
			ToAddress: types.PtlcContract,
			Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
				expirationTime,
				definition.PointTypeED25519,
				g.User2.Public,
				types.ZeroAddress,
			),
			TokenStandard: types.ZnnTokenStandard,
			Amount:        big.NewInt(10 * g.Zexp),
		}
	}
	before := ptlcZnnBalance(t, z, g.User1.Address)

	sentAt := ptlcFrontier(t, z).Timestamp.Unix()
	expired := z.CallContract(create(sentAt + 1))
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	expired.Error(t, constants.ErrInvalidExpirationTime)
	if receivedAt := ptlcFrontier(t, z).Timestamp.Unix(); receivedAt <= sentAt+1 {
		t.Fatalf("the confirming momentum did not pass the expiration: %d", receivedAt)
	}

	autoreceive(t, z, g.User1.Address)
	z.InsertNewMomentum()
	if after := ptlcZnnBalance(t, z, g.User1.Address); after.Cmp(before) != 0 {
		t.Fatalf("the refused create was not refunded: %v, then %v", before, after)
	}
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)

	sentAt = ptlcFrontier(t, z).Timestamp.Unix()
	block := z.InsertSendBlock(create(sentAt+1000), nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	if _, err := ptlcApi.GetById(block.Hash); err != nil {
		t.Fatalf("a create confirmed in time was not stored: %v", err)
	}
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
}

func TestPtlc_competingClaimsInOneMomentum(t *testing.T) {
	z := mock.NewMockZenon(t)
	ptlcApi := embedded.NewPtlcApi(z)
	defer z.StopPanic()
	activatePtlc(t, z)

	id := createED25519Ptlc(t, z, g.User2.Public, int64(genesisTimestamp+1000), types.ZnnTokenStandard, big.NewInt(10*g.Zexp))
	z.InsertNewMomentum()
	toUser2 := g.User2.Sign(ptlcUnlockMessage(z, definition.PointTypeED25519, id, g.User2.Address))
	toUser3 := g.User2.Sign(ptlcUnlockMessage(z, definition.PointTypeED25519, id, g.User3.Address))
	held := new(big.Int).Add(ptlcZnnBalance(t, z, g.User2.Address), ptlcZnnBalance(t, z, g.User3.Address))

	sentIn := ptlcFrontier(t, z).Height
	claims := []*nom.AccountBlock{
		{
			Address:   g.User2.Address,
			ToAddress: types.PtlcContract,
			Data:      definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, id, toUser2),
		},
		{
			Address:   g.User3.Address,
			ToAddress: types.PtlcContract,
			Data:      definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, id, toUser3),
		},
		{
			Address:   g.User4.Address,
			ToAddress: types.PtlcContract,
			Data:      definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName, id, g.User2.Address, toUser2),
		},
	}
	for _, claim := range claims {
		if block := z.InsertSendBlock(claim, nil, mock.SkipVmChanges); block == nil {
			t.Fatalf("a claim was not taken")
		}
	}
	if height := ptlcFrontier(t, z).Height; height != sentIn {
		t.Fatalf("the three claims were not sent in one momentum: %d, then %d", sentIn, height)
	}
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	if _, err := ptlcApi.GetById(id); err != constants.ErrDataNonExistent {
		t.Fatalf("the entry outlived its claims: %v", err)
	}
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)

	autoreceive(t, z, g.User2.Address)
	autoreceive(t, z, g.User3.Address)
	z.InsertNewMomentum()
	received := new(big.Int).Add(ptlcZnnBalance(t, z, g.User2.Address), ptlcZnnBalance(t, z, g.User3.Address))
	received.Sub(received, held)
	if received.Cmp(big.NewInt(10*g.Zexp)) != 0 {
		t.Fatalf("the claimants received %v between them, want %v", received, 10*g.Zexp)
	}
}

func TestPtlc_getByIdOverJsonRpc(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	server := rpc.NewServer()
	defer server.Stop()
	common.FailIfErr(t, server.RegisterName("embedded.ptlc", embedded.NewPtlcApi(z)))
	client := rpc.DialInProc(server)
	defer client.Close()

	id := createED25519Ptlc(t, z, g.User2.Public, int64(genesisTimestamp+1000), types.ZnnTokenStandard, big.NewInt(10*g.Zexp))
	z.InsertNewMomentum()

	var entry map[string]interface{}
	common.FailIfErr(t, client.Call(&entry, "embedded.ptlc.getById", id.String()))
	if entry["id"] != id.String() || entry["amount"] != "1000000000" {
		t.Fatalf("unexpected entry over the transport: %v", entry)
	}

	for name, param := range map[string]interface{}{
		"not hex":         "not-a-hash",
		"one digit short": id.String()[1:],
		"one digit long":  id.String() + "0",
		"empty":           "",
		"a number":        7,
		"an object":       map[string]string{"id": id.String()},
	} {
		var refused map[string]interface{}
		err := client.Call(&refused, "embedded.ptlc.getById", param)
		if err == nil {
			t.Errorf("%s: answered %v", name, refused)
		} else if !strings.Contains(err.Error(), "invalid argument") {
			t.Errorf("%s: refused with %q, want an invalid argument", name, err)
		}
	}

	for name, param := range map[string]interface{}{
		"an unknown id":             types.NewHash([]byte("no such entry")).String(),
		"null, read as the zero id": nil,
	} {
		var missing map[string]interface{}
		err := client.Call(&missing, "embedded.ptlc.getById", param)
		if err == nil || err.Error() != constants.ErrDataNonExistent.Error() {
			t.Errorf("%s: answered %v, %v", name, missing, err)
		}
	}

	common.FailIfErr(t, client.Call(&entry, "embedded.ptlc.getById", id.String()))
}
