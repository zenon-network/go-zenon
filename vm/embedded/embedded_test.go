package embedded

import (
	"encoding/hex"
	"fmt"
	"sort"
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	cabi "github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

// sporkFlagsContext is a minimal vm_context.AccountVmContext test double. GetEmbeddedMethod only
// calls the Is*SporkEnforced() accessors, so embedding a nil AccountVmContext and overriding just
// those is sufficient: any other method call would panic, but none is reached in these tests.
type sporkFlagsContext struct {
	vm_context.AccountVmContext
	accelerator        bool
	htlc               bool
	bridgeAndLiquidity bool
	dynamicPlasma      bool
	multisig           bool
}

func (c *sporkFlagsContext) IsAcceleratorSporkEnforced() bool        { return c.accelerator }
func (c *sporkFlagsContext) IsHtlcSporkEnforced() bool               { return c.htlc }
func (c *sporkFlagsContext) IsBridgeAndLiquiditySporkEnforced() bool { return c.bridgeAndLiquidity }
func (c *sporkFlagsContext) IsDynamicPlasmaSporkEnforced() bool      { return c.dynamicPlasma }
func (c *sporkFlagsContext) IsMultisigSporkEnforced() bool           { return c.multisig }

func TestDumpContractsABIMethods(t *testing.T) {
	dumps := make([]string, 0)
	for addr, contract := range getAllEmbedded() {
		for _, method := range contract.abi.Methods {
			dumps = append(dumps, fmt.Sprintf(`{"address":"%v", "name":"%v", "id":"%v", "signature":"%v"}`, addr, method.Name, hex.EncodeToString(method.Id()), method.Sig()))
		}
	}
	sort.Strings(dumps)
	dump := "[\n"
	for i := range dumps {
		if i+1 != len(dumps) {
			dump = dump + dumps[i] + "\n"
		} else {
			dump = dump + dumps[i] + "\n"
		}
	}
	dump += "]\n"

	common.Expect(t, dump, `
[
{"address":"z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22", "name":"AddPhase", "id":"c7e13ddc", "signature":"AddPhase(hash,string,string,string,uint256,uint256)"}
{"address":"z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22", "name":"CreateProject", "id":"77c044b6", "signature":"CreateProject(string,string,string,uint256,uint256)"}
{"address":"z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22", "name":"Donate", "id":"cb7f8b2a", "signature":"Donate()"}
{"address":"z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22", "name":"Update", "id":"20093ea6", "signature":"Update()"}
{"address":"z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22", "name":"UpdatePhase", "id":"c1d7d323", "signature":"UpdatePhase(hash,string,string,string,uint256,uint256)"}
{"address":"z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22", "name":"VoteByName", "id":"5c6c1064", "signature":"VoteByName(hash,string,uint8)"}
{"address":"z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22", "name":"VoteByProdAddress", "id":"90ed001c", "signature":"VoteByProdAddress(hash,uint8)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"ChangeAdministrator", "id":"4f6bef7c", "signature":"ChangeAdministrator(address)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"ChangeTssECDSAPubKey", "id":"15a0c641", "signature":"ChangeTssECDSAPubKey(string,string,string)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"Emergency", "id":"fa4ba15f", "signature":"Emergency()"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"Halt", "id":"72334d21", "signature":"Halt(string)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"NominateGuardians", "id":"688ac608", "signature":"NominateGuardians(address[])"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"ProposeAdministrator", "id":"1ca313bd", "signature":"ProposeAdministrator(address)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"Redeem", "id":"d4e06c79", "signature":"Redeem(hash,uint32)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"RemoveNetwork", "id":"3d36aac1", "signature":"RemoveNetwork(uint32,uint32)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"RemoveTokenPair", "id":"b497bf39", "signature":"RemoveTokenPair(uint32,uint32,tokenStandard,string)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"RevokeUnwrapRequest", "id":"fa7c7f3d", "signature":"RevokeUnwrapRequest(hash,uint32)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"SetAllowKeyGen", "id":"4b9b3ecb", "signature":"SetAllowKeyGen(bool)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"SetBridgeMetadata", "id":"96be29e3", "signature":"SetBridgeMetadata(string)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"SetNetwork", "id":"e4f0c639", "signature":"SetNetwork(uint32,uint32,string,string,string)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"SetNetworkMetadata", "id":"ebea4402", "signature":"SetNetworkMetadata(uint32,uint32,string)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"SetOrchestratorInfo", "id":"eed69856", "signature":"SetOrchestratorInfo(uint64,uint32,uint32,uint32)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"SetRedeemDelay", "id":"fd2411ec", "signature":"SetRedeemDelay(uint64)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"SetTokenPair", "id":"d5292476", "signature":"SetTokenPair(uint32,uint32,tokenStandard,string,bool,bool,bool,uint256,uint32,uint32,string)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"Unhalt", "id":"3a16f20e", "signature":"Unhalt()"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"UnwrapToken", "id":"b6069401", "signature":"UnwrapToken(uint32,uint32,hash,uint32,address,string,uint256,string)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"UpdateWrapRequest", "id":"d4bb11c0", "signature":"UpdateWrapRequest(hash,string)"}
{"address":"z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d", "name":"WrapToken", "id":"61d224bc", "signature":"WrapToken(uint32,uint32,string)"}
{"address":"z1qxemdeddedxhtlcxxxxxxxxxxxxxxxxxygecvw", "name":"AllowProxyUnlock", "id":"57758f10", "signature":"AllowProxyUnlock()"}
{"address":"z1qxemdeddedxhtlcxxxxxxxxxxxxxxxxxygecvw", "name":"Create", "id":"5c7e7110", "signature":"Create(address,int64,uint8,uint8,bytes)"}
{"address":"z1qxemdeddedxhtlcxxxxxxxxxxxxxxxxxygecvw", "name":"DenyProxyUnlock", "id":"e17c39ed", "signature":"DenyProxyUnlock()"}
{"address":"z1qxemdeddedxhtlcxxxxxxxxxxxxxxxxxygecvw", "name":"Reclaim", "id":"7e003c8d", "signature":"Reclaim(hash)"}
{"address":"z1qxemdeddedxhtlcxxxxxxxxxxxxxxxxxygecvw", "name":"Unlock", "id":"d33791d3", "signature":"Unlock(hash,bytes)"}
{"address":"z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae", "name":"BurnZnn", "id":"096b75a4", "signature":"BurnZnn(uint256)"}
{"address":"z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae", "name":"CancelLiquidityStake", "id":"b8efc37c", "signature":"CancelLiquidityStake(hash)"}
{"address":"z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae", "name":"ChangeAdministrator", "id":"4f6bef7c", "signature":"ChangeAdministrator(address)"}
{"address":"z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae", "name":"CollectReward", "id":"af43d3f0", "signature":"CollectReward()"}
{"address":"z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae", "name":"Donate", "id":"cb7f8b2a", "signature":"Donate()"}
{"address":"z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae", "name":"Emergency", "id":"fa4ba15f", "signature":"Emergency()"}
{"address":"z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae", "name":"Fund", "id":"912f3c3f", "signature":"Fund(uint256,uint256)"}
{"address":"z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae", "name":"LiquidityStake", "id":"071fa116", "signature":"LiquidityStake(int64)"}
{"address":"z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae", "name":"NominateGuardians", "id":"688ac608", "signature":"NominateGuardians(address[])"}
{"address":"z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae", "name":"ProposeAdministrator", "id":"1ca313bd", "signature":"ProposeAdministrator(address)"}
{"address":"z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae", "name":"SetAdditionalReward", "id":"a8fbfe56", "signature":"SetAdditionalReward(uint256,uint256)"}
{"address":"z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae", "name":"SetIsHalted", "id":"4649fe91", "signature":"SetIsHalted(bool)"}
{"address":"z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae", "name":"SetTokenTuple", "id":"f0ad68db", "signature":"SetTokenTuple(string[],uint32[],uint32[],uint256[])"}
{"address":"z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae", "name":"UnlockLiquidityStakeEntries", "id":"616643ca", "signature":"UnlockLiquidityStakeEntries()"}
{"address":"z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae", "name":"Update", "id":"20093ea6", "signature":"Update()"}
{"address":"z1qxemdeddedxmultysygxxxxxxxxxxxxx42zwd4", "name":"ChangePolicy", "id":"9f202100", "signature":"ChangePolicy(uint8,bytes[],bool)"}
{"address":"z1qxemdeddedxmultysygxxxxxxxxxxxxx42zwd4", "name":"CreateMultisig", "id":"2adf7a33", "signature":"CreateMultisig(uint64,uint8,bytes[])"}
{"address":"z1qxemdeddedxplasmaxxxxxxxxxxxxxxxxsctrp", "name":"CancelFuse", "id":"f9ca9dc3", "signature":"CancelFuse(hash)"}
{"address":"z1qxemdeddedxplasmaxxxxxxxxxxxxxxxxsctrp", "name":"Fuse", "id":"5ac942e8", "signature":"Fuse(address)"}
{"address":"z1qxemdeddedxplasmaxxxxxxxxxxxxxxxxsctrp", "name":"SetVariables", "id":"15db3894", "signature":"SetVariables(uint64,uint64,uint64,uint8,uint8)"}
{"address":"z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg", "name":"CollectReward", "id":"af43d3f0", "signature":"CollectReward()"}
{"address":"z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg", "name":"Delegate", "id":"7c2d5d6e", "signature":"Delegate(string)"}
{"address":"z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg", "name":"DepositQsr", "id":"d49577f4", "signature":"DepositQsr()"}
{"address":"z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg", "name":"Register", "id":"644de927", "signature":"Register(string,address,address,uint8,uint8)"}
{"address":"z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg", "name":"RegisterLegacy", "id":"e4588207", "signature":"RegisterLegacy(string,address,address,uint8,uint8,string,string)"}
{"address":"z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg", "name":"Revoke", "id":"95631306", "signature":"Revoke(string)"}
{"address":"z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg", "name":"Undelegate", "id":"7e8952c8", "signature":"Undelegate()"}
{"address":"z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg", "name":"Update", "id":"20093ea6", "signature":"Update()"}
{"address":"z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg", "name":"UpdatePillar", "id":"de0ae34b", "signature":"UpdatePillar(string,address,address,uint8,uint8)"}
{"address":"z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg", "name":"WithdrawQsr", "id":"b3d658fd", "signature":"WithdrawQsr()"}
{"address":"z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r", "name":"CollectReward", "id":"af43d3f0", "signature":"CollectReward()"}
{"address":"z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r", "name":"DepositQsr", "id":"d49577f4", "signature":"DepositQsr()"}
{"address":"z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r", "name":"Register", "id":"4dd23517", "signature":"Register()"}
{"address":"z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r", "name":"Revoke", "id":"58363e24", "signature":"Revoke()"}
{"address":"z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r", "name":"Update", "id":"20093ea6", "signature":"Update()"}
{"address":"z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r", "name":"WithdrawQsr", "id":"b3d658fd", "signature":"WithdrawQsr()"}
{"address":"z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48", "name":"ActivateSpork", "id":"25c54e96", "signature":"ActivateSpork(hash)"}
{"address":"z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48", "name":"CreateSpork", "id":"b602e311", "signature":"CreateSpork(string,string)"}
{"address":"z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62", "name":"Cancel", "id":"5a92fe32", "signature":"Cancel(hash)"}
{"address":"z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62", "name":"CollectReward", "id":"af43d3f0", "signature":"CollectReward()"}
{"address":"z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62", "name":"Stake", "id":"d802845a", "signature":"Stake(int64)"}
{"address":"z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62", "name":"Update", "id":"20093ea6", "signature":"Update()"}
{"address":"z1qxemdeddedxswapxxxxxxxxxxxxxxxxxxl4yww", "name":"RetrieveAssets", "id":"47f12c81", "signature":"RetrieveAssets(string,string)"}
{"address":"z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0", "name":"Burn", "id":"3395ab94", "signature":"Burn()"}
{"address":"z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0", "name":"IssueToken", "id":"bc410b91", "signature":"IssueToken(string,string,string,uint256,uint256,uint8,bool,bool,bool)"}
{"address":"z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0", "name":"Mint", "id":"cd70f9bc", "signature":"Mint(tokenStandard,uint256,address)"}
{"address":"z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0", "name":"UpdateToken", "id":"2a3cf32c", "signature":"UpdateToken(tokenStandard,address,bool,bool)"}
]`)
}

// TestGetEmbeddedMethod_MultisigDormantByDefault: with no spork enforced (the zero-value
// context), MultisigContract is unresolvable, matching the spork's dormant-on-merge guarantee.
func TestGetEmbeddedMethod_MultisigDormantByDefault(t *testing.T) {
	ctx := &sporkFlagsContext{}

	createId := cabi.ABIMultisig.Methods[cabi.CreateMultisigMethodName].Id()
	_, err := GetEmbeddedMethod(ctx, types.MultisigContract, createId)
	if err != constants.ErrContractDoesntExist {
		t.Fatalf("expected ErrContractDoesntExist, got %v", err)
	}

	changeId := cabi.ABIMultisig.Methods[cabi.ChangePolicyMethodName].Id()
	_, err = GetEmbeddedMethod(ctx, types.MultisigContract, changeId)
	if err != constants.ErrContractDoesntExist {
		t.Fatalf("expected ErrContractDoesntExist, got %v", err)
	}
}

// TestGetEmbeddedMethod_MultisigEnforced: once the multisig spork is enforced,
// CreateMultisig/ChangePolicy resolve to their registered methods.
func TestGetEmbeddedMethod_MultisigEnforced(t *testing.T) {
	ctx := &sporkFlagsContext{multisig: true}

	createId := cabi.ABIMultisig.Methods[cabi.CreateMultisigMethodName].Id()
	method, err := GetEmbeddedMethod(ctx, types.MultisigContract, createId)
	common.FailIfErr(t, err)
	if method == nil {
		t.Fatal("expected CreateMultisig method, got nil")
	}

	changeId := cabi.ABIMultisig.Methods[cabi.ChangePolicyMethodName].Id()
	method, err = GetEmbeddedMethod(ctx, types.MultisigContract, changeId)
	common.FailIfErr(t, err)
	if method == nil {
		t.Fatal("expected ChangePolicy method, got nil")
	}
}

// TestGetEmbeddedMethod_MultisigIndependentResolution: enforcing the multisig spork must not
// affect resolution of any other independently-gated contract — each applyXDiffs only ever
// writes its own contract key.
func TestGetEmbeddedMethod_MultisigIndependentResolution(t *testing.T) {
	ctx := &sporkFlagsContext{
		accelerator:        true,
		htlc:               true,
		bridgeAndLiquidity: true,
		dynamicPlasma:      true,
		multisig:           true,
	}

	// htlc (spork-gated)
	createHtlcId := cabi.ABIHtlc.Methods[cabi.CreateHtlcMethodName].Id()
	method, err := GetEmbeddedMethod(ctx, types.HtlcContract, createHtlcId)
	common.FailIfErr(t, err)
	if method == nil {
		t.Fatal("expected htlc Create method to still resolve")
	}

	// bridge (spork-gated)
	wrapId := cabi.ABIBridge.Methods[cabi.WrapTokenMethodName].Id()
	method, err = GetEmbeddedMethod(ctx, types.BridgeContract, wrapId)
	common.FailIfErr(t, err)
	if method == nil {
		t.Fatal("expected bridge WrapToken method to still resolve")
	}

	// accelerator (spork-gated)
	donateId := cabi.ABIAccelerator.Methods[cabi.DonateMethodName].Id()
	method, err = GetEmbeddedMethod(ctx, types.AcceleratorContract, donateId)
	common.FailIfErr(t, err)
	if method == nil {
		t.Fatal("expected accelerator Donate method to still resolve")
	}

	// origin (unconditional, e.g. pillar registration)
	registerId := cabi.ABIPillars.Methods[cabi.RegisterMethodName].Id()
	method, err = GetEmbeddedMethod(ctx, types.PillarContract, registerId)
	common.FailIfErr(t, err)
	if method == nil {
		t.Fatal("expected pillar Register method to still resolve")
	}

	// and multisig itself
	createMultisigId := cabi.ABIMultisig.Methods[cabi.CreateMultisigMethodName].Id()
	method, err = GetEmbeddedMethod(ctx, types.MultisigContract, createMultisigId)
	common.FailIfErr(t, err)
	if method == nil {
		t.Fatal("expected CreateMultisig method to resolve")
	}
}
