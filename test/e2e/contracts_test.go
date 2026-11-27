//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/crypto"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/abi"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// The cases below cover TEST-PLAN.md section 5.1 for what the stock devnet
// genesis allows: Accelerator and HTLC on, Bridge and Liquidity not
// activated, PTLC absent. Reward collection, expiry of long locks and the
// spork-gated contracts need the genesis variants and are not here.

type obj = map[string]interface{}

// str renders a decoded JSON value; whole numbers print without an exponent.
func str(v interface{}) string {
	if f, ok := v.(float64); ok && f == float64(int64(f)) {
		return fmt.Sprint(int64(f))
	}
	return fmt.Sprint(v)
}

func bigOf(t *testing.T, v interface{}) *big.Int {
	t.Helper()
	n, ok := new(big.Int).SetString(str(v), 10)
	if !ok {
		t.Fatalf("not a number: %v", v)
	}
	return n
}

func sub(a, b *big.Int) *big.Int { return new(big.Int).Sub(a, b) }
func add(a, b *big.Int) *big.Int { return new(big.Int).Add(a, b) }

func TestPlasma(t *testing.T) {
	group(t)
	a := newAccount(t, 10, 10, 500, 1000)
	other := newAccount(t, 11, 0, 20, 500)
	plasma := types.PlasmaContract
	fuse := func(to types.Address) []byte {
		return definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, to)
	}
	cancel := func(id types.Hash) []byte {
		return definition.ABIPlasma.PackMethodPanic(definition.CancelFuseMethodName, id)
	}
	var fusionID, secondID types.Hash
	beneficiary := net.Key(runBase + 12).Address
	// fusionsAre says how a page of the owner's fusions differs from exactly
	// the two that PL-1 and PL-2 made: by id, amount and beneficiary. It is
	// applied to the very readings a refused cancel is judged by.
	fusionsAre := func(raw json.RawMessage) string {
		var page struct {
			Count     int    `json:"count"`
			QsrAmount string `json:"qsrAmount"`
			List      []struct {
				ID          types.Hash    `json:"id"`
				QsrAmount   string        `json:"qsrAmount"`
				Beneficiary types.Address `json:"beneficiary"`
			} `json:"list"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return err.Error()
		}
		want := map[types.Hash]struct {
			amount string
			to     types.Address
		}{fusionID: {coins(50).String(), beneficiary}, secondID: {coins(10).String(), a.Address}}
		if page.Count != 2 || len(page.List) != 2 {
			return fmt.Sprintf("%d fusions (%d listed), want the two of PL-1 and PL-2", page.Count, len(page.List))
		}
		if total := coins(60).String(); page.QsrAmount != total {
			return fmt.Sprintf("the page totals %s QSR; its two fusions are %s", page.QsrAmount, total)
		}
		seen := map[types.Hash]bool{}
		for _, e := range page.List {
			w, ok := want[e.ID]
			if !ok || seen[e.ID] || e.QsrAmount != w.amount || e.Beneficiary != w.to {
				return fmt.Sprintf("fusion %s of %s QSR for %s; want %s and %s as PL-1 and PL-2 made them", e.ID, e.QsrAmount, e.Beneficiary, fusionID, secondID)
			}
			seen[e.ID] = true
		}
		return ""
	}
	owned := func(t *testing.T) {
		t.Helper()
		var raw json.RawMessage
		get(t, &raw, "embedded.plasma.getEntriesByAddress", a.Address, 0, 10)
		if why := fusionsAre(raw); why != "" {
			t.Errorf("fusion entries of the owner: %s", why)
		}
	}

	t.Run("PL-1_fuse_for_another_address", func(t *testing.T) {
		cover(t, "E1", "contract-method", "plasma.Fuse")
		before := bal(t, a.Address, qsr)
		res := callOK(t, a, plasma, fuse(beneficiary), qsr, coins(50))
		fusionID = res.Send.Hash
		wantBig(t, "QSR after fuse", bal(t, a.Address, qsr), sub(before, coins(50)))

		// One reply holds all three: 50 QSR fused, which are 50 fusion units
		// of 2,100 plasma each, and the fresh beneficiary has used none, so
		// current and maximum are both 105,000. Typed fields fail the decode
		// on a fraction or a string; a missing field stays nil.
		var plasma struct {
			Qsr     *string `json:"qsrAmount"`
			Current *uint64 `json:"currentPlasma"`
			Max     *uint64 `json:"maxPlasma"`
		}
		get(t, &plasma, "embedded.plasma.get", beneficiary)
		want := uint64(50) * constants.PlasmaPerFusionUnit
		if plasma.Qsr == nil || plasma.Current == nil || plasma.Max == nil || *plasma.Qsr != coins(50).String() || *plasma.Current != want || *plasma.Max != want {
			t.Errorf("beneficiary's plasma: %+v; 50 QSR (%s units) fused for an unused address gives %d current and maximum", plasma, coins(50), want)
		}
		var entries obj
		get(t, &entries, "embedded.plasma.getEntriesByAddress", a.Address, 0, 10)
		list, _ := entries["list"].([]interface{})
		if c, ok := entries["count"].(float64); !ok || c != 1 || str(entries["qsrAmount"]) != coins(50).String() || len(list) != 1 || str(list[0].(obj)["id"]) != fusionID.String() || str(list[0].(obj)["beneficiary"]) != beneficiary.String() || str(list[0].(obj)["qsrAmount"]) != coins(50).String() {
			t.Errorf("fusion entries of the owner: %v", entries)
		}
		if err := net.Agreement(res.Receive.Hash, wait); err != nil {
			t.Error(err)
		}
	})

	t.Run("PL-2_fuse_amount_boundaries", func(t *testing.T) {
		cover(t, "E2", "contract-method", "plasma.Fuse")
		cover(t, "E5", "contract-method", "plasma.Fuse")
		cover(t, "E2", "error", "vm/constants.ErrInvalidTokenOrAmount")
		bad := constants.ErrInvalidTokenOrAmount.Error()
		callReject(t, bad, a, plasma, fuse(a.Address), qsr, sub(coins(10), units(1))) // one unit under the minimum
		callReject(t, bad, a, plasma, fuse(a.Address), qsr, add(coins(10), units(1))) // not a whole fusion unit
		callReject(t, bad, a, plasma, fuse(a.Address), qsr, units(0))
		callReject(t, bad, a, plasma, fuse(a.Address), znn, coins(10))             // wrong token
		secondID = callOK(t, a, plasma, fuse(a.Address), qsr, coins(10)).Send.Hash // exactly the minimum
		owned(t)                                                                   // both fusions, each as it was made
	})

	t.Run("PL-3_cancel_before_expiry_fails", func(t *testing.T) {
		cover(t, "E4", "contract-method", "plasma.CancelFuse")
		cover(t, "E12", "contract-method", "plasma.CancelFuse")
		before := bal(t, a.Address, qsr)
		callFail(t, a, plasma, cancel(fusionID), znn, units(0), watchWith(fusionsAre, "embedded.plasma.getEntriesByAddress", a.Address, 0, 10))
		wantBig(t, "QSR unchanged", bal(t, a.Address, qsr), before)
		owned(t)
	})

	t.Run("PL-4_cancel_by_another_account_fails", func(t *testing.T) {
		cover(t, "E3", "contract-method", "plasma.CancelFuse")
		callFail(t, other, plasma, cancel(fusionID), znn, units(0), watchWith(fusionsAre, "embedded.plasma.getEntriesByAddress", a.Address, 0, 10))
		owned(t)
	})

	t.Run("PL-5_cancel_of_an_unknown_id_fails", func(t *testing.T) {
		callFail(t, a, plasma, cancel(types.NewHash([]byte("no fusion "+runTag))), znn, units(0), watchWith(fusionsAre, "embedded.plasma.getEntriesByAddress", a.Address, 0, 10))
		owned(t)
	})

	t.Run("PL-6_cancel_with_an_amount_refused", func(t *testing.T) {
		cover(t, "E5", "contract-method", "plasma.CancelFuse")
		callReject(t, constants.ErrInvalidTokenOrAmount.Error(), a, plasma, cancel(fusionID), qsr, coins(1))
	})

	t.Run("PL-7_set_variables_from_a_normal_address_refused", func(t *testing.T) {
		data := definition.ABIPlasma.PackMethodPanic(definition.SetVariablesMethodName, uint64(2100000), uint64(1000000), uint64(1000000), uint8(10), uint8(8))
		if !live["dynamic-plasma"] {
			// The method arrives with dynamic plasma; before it, the contract does not have it.
			cover(t, "E7", "contract-method", "plasma.SetVariables")
			callReject(t, constants.ErrContractMethodNotFound.Error(), a, plasma, data, znn, units(0))
			return
		}
		cover(t, "E3", "contract-method", "plasma.SetVariables")
		cover(t, "E3", "error", "vm/constants.ErrPermissionDenied")
		callReject(t, constants.ErrPermissionDenied.Error(), a, plasma, data, znn, units(0))
		var vars obj
		get(t, &vars, "embedded.plasma.getVariables")
		if len(vars) != 5 {
			t.Errorf("plasma variables: %v", vars)
		}
	})

	t.Run("PL-8_malformed_call_data_refused", func(t *testing.T) {
		cover(t, "E6", "contract-method", "plasma.Fuse")
		cover(t, "E6", "error", "vm/constants.ErrUnpackError", "vm/constants.ErrContractMethodNotFound")
		if live["dynamic-plasma"] {
			cover(t, "E7", "error", "vm/constants.ErrABDataTrailingBytes")
		}
		good := fuse(a.Address)
		callReject(t, constants.ErrUnpackError.Error(), a, plasma, good[:len(good)-1], qsr, coins(10))
		callReject(t, constants.ErrUnpackError.Error(), a, plasma, good[:4], qsr, coins(10))
		callReject(t, constants.ErrContractMethodNotFound.Error(), a, plasma, []byte{1, 2, 3, 4}, qsr, coins(10))
		callReject(t, constants.ErrContractMethodNotFound.Error(), a, plasma, nil, qsr, coins(10))
		// Trailing bytes are refused once dynamic plasma is enforced, and ignored before.
		trailing := append(append([]byte{}, good...), 0)
		if live["dynamic-plasma"] {
			callReject(t, constants.ErrABDataTrailingBytes.Error(), a, plasma, trailing, qsr, coins(10))
		} else {
			// Before the spork the node drops the extra byte when it re-packs
			// the data, so the hash the sender signed no longer matches.
			callReject(t, "hash is different than the one computed", a, plasma, trailing, qsr, coins(10))
		}
		// Non-canonical left-padding in the address word: the exact length
		// passes the trailing-bytes rule and the ABI decoder reads the same
		// address, but the node re-packs the decoded call to canonical form
		// during execution, so the hash the sender signed no longer matches
		// the mutated block — on either side of the spork.
		padded := append([]byte{}, good...)
		padded[4] = 0x01 // first byte of the 12-byte left-padding of the 32-byte address word
		callReject(t, "hash is different than the one computed", a, plasma, padded, qsr, coins(10))
	})
}

func TestToken(t *testing.T) {
	group(t)
	owner := newAccount(t, 20, 10, 10, 1000)
	other := newAccount(t, 21, 2, 0, 500)
	token := types.TokenContract
	name := "E2E-" + runTag
	var zts types.ZenonTokenStandard
	info := func(t *testing.T) obj {
		var o obj
		get(t, &o, "embedded.token.getByZts", zts)
		return o
	}
	mint := func(amount *big.Int, to types.Address) []byte {
		return definition.ABIToken.PackMethodPanic(definition.MintMethodName, zts, amount, to)
	}
	issue := func(name, symbol, domain string, total, max *big.Int, decimals uint8, mintable bool) []byte {
		return definition.ABIToken.PackMethodPanic(definition.IssueMethodName, name, symbol, domain, total, max, decimals, mintable, true, false)
	}

	t.Run("TK-1_issue", func(t *testing.T) {
		cover(t, "E1", "contract-method", "token.IssueToken")
		before := bal(t, owner.Address, znn)
		callOK(t, owner, token, issue(name, "EE"+runTag, "e2e.test", units(1000), units(10000), 2, true), znn, constants.TokenIssueAmount)
		receiveAll(t, owner, 1)
		wantBig(t, "ZNN after the issue fee", bal(t, owner.Address, znn), sub(before, constants.TokenIssueAmount))

		var mine obj
		get(t, &mine, "embedded.token.getByOwner", owner.Address, 0, 10)
		list, _ := mine["list"].([]interface{})
		if len(list) != 1 {
			t.Fatalf("tokens of the owner: %v", mine)
		}
		zts = types.ParseZTSPanic(str(list[0].(obj)["tokenStandard"]))
		wantBig(t, "owner balance of the new token", bal(t, owner.Address, zts), units(1000))
		if got := info(t); str(got["name"]) != name || str(got["totalSupply"]) != "1000" || str(got["maxSupply"]) != "10000" {
			t.Errorf("token read back: %v", got)
		}
		var all obj
		get(t, &all, "embedded.token.getAll", 0, 10)
		if str(all["count"]) == "2" {
			t.Errorf("token list still has only ZNN and QSR: %v", all["count"])
		}
	})

	t.Run("TK-2_issue_refusals", func(t *testing.T) {
		cover(t, "E2", "contract-method", "token.IssueToken")
		cover(t, "E5", "contract-method", "token.IssueToken")
		cover(t, "E2", "error", "vm/constants.ErrTokenInvalidText", "vm/constants.ErrTokenInvalidAmount")
		text, amount := constants.ErrTokenInvalidText.Error(), constants.ErrTokenInvalidAmount.Error()
		fee := constants.TokenIssueAmount
		callReject(t, text, owner, token, issue("Ok", "ZNN", "", units(1), units(1), 0, false), znn, fee)
		callReject(t, text, owner, token, issue("", "OK", "", units(1), units(1), 0, false), znn, fee)
		callReject(t, text, owner, token, issue(strings.Repeat("a", 41), "OK", "", units(1), units(1), 0, false), znn, fee)
		callReject(t, text, owner, token, issue("Ok", "lower", "", units(1), units(1), 0, false), znn, fee)
		callReject(t, text, owner, token, issue("Ok", "OK", "not a domain", units(1), units(1), 0, false), znn, fee)
		callReject(t, text, owner, token, issue("Ok", "OK", "", units(1), units(1), 19, false), znn, fee)
		callReject(t, amount, owner, token, issue("Ok", "OK", "", units(0), units(0), 0, true), znn, fee)
		callReject(t, amount, owner, token, issue("Ok", "OK", "", units(2), units(1), 0, true), znn, fee)
		callReject(t, amount, owner, token, issue("Ok", "OK", "", units(1), units(2), 0, false), znn, fee) // not mintable, so max must equal total
		callReject(t, amount, owner, token, issue("Ok", "OK", "", units(1), add(constants.TokenMaxSupplyBig, units(1)), 0, true), znn, fee)
		bad := constants.ErrInvalidTokenOrAmount.Error()
		callReject(t, bad, owner, token, issue("Ok", "OK", "", units(1), units(1), 0, false), znn, sub(fee, units(1)))
		callReject(t, bad, owner, token, issue("Ok", "OK", "", units(1), units(1), 0, false), znn, add(fee, units(1)))
		callReject(t, bad, owner, token, issue("Ok", "OK", "", units(1), units(1), 0, false), qsr, fee)
	})

	t.Run("TK-3_mint_to_another_account", func(t *testing.T) {
		cover(t, "E1", "contract-method", "token.Mint")
		callOK(t, owner, token, mint(units(500), other.Address), znn, units(0))
		receiveAll(t, other, 1)
		wantBig(t, "minted balance", bal(t, other.Address, zts), units(500))
		wantBig(t, "total supply", bigOf(t, info(t)["totalSupply"]), units(1500))
	})

	t.Run("TK-4_mint_by_a_non_owner_fails", func(t *testing.T) {
		cover(t, "E3", "contract-method", "token.Mint")
		callFail(t, other, token, mint(units(1), other.Address), znn, units(0), watch("embedded.token.getByZts", zts))
		wantBig(t, "total supply", bigOf(t, info(t)["totalSupply"]), units(1500))
	})

	t.Run("TK-5_mint_supply_boundaries", func(t *testing.T) {
		cover(t, "E2", "contract-method", "token.Mint")
		callFail(t, owner, token, mint(units(8501), owner.Address), znn, units(0), watch("embedded.token.getByZts", zts)) // one over the maximum supply
		callReject(t, constants.ErrInvalidTokenOrAmount.Error(), owner, token, mint(units(0), owner.Address), znn, units(0))
		callReject(t, constants.ErrInvalidTokenOrAmount.Error(), owner, token, mint(units(1), owner.Address), znn, units(1))
		callOK(t, owner, token, mint(units(8500), owner.Address), znn, units(0)) // exactly to the maximum
		receiveAll(t, owner, 1)
		wantBig(t, "total supply", bigOf(t, info(t)["totalSupply"]), units(10000))
	})

	t.Run("TK-6_transfer_of_the_custom_token", func(t *testing.T) {
		blk := transfer(t, owner, other.Address, zts, units(25), nil)
		receiveAll(t, other, 1)
		wantBig(t, "receiver balance", bal(t, other.Address, zts), units(525))
		if err := net.Agreement(blk.Hash, wait); err != nil {
			t.Error(err)
		}
	})

	t.Run("TK-7_burn", func(t *testing.T) {
		cover(t, "E1", "contract-method", "token.Burn")
		cover(t, "E5", "contract-method", "token.Burn")
		burn := definition.ABIToken.PackMethodPanic(definition.BurnMethodName)
		callOK(t, other, token, burn, zts, units(100))
		wantBig(t, "holder balance", bal(t, other.Address, zts), units(425))
		wantBig(t, "total supply", bigOf(t, info(t)["totalSupply"]), units(9900))
		callReject(t, constants.ErrInvalidTokenOrAmount.Error(), other, token, burn, zts, units(0))
	})

	t.Run("TK-8_update_by_a_non_owner_fails", func(t *testing.T) {
		cover(t, "E3", "contract-method", "token.UpdateToken")
		callFail(t, other, token, definition.ABIToken.PackMethodPanic(definition.UpdateTokenMethodName, zts, other.Address, true, true), znn, units(0), watch("embedded.token.getByZts", zts))
		if got := info(t); str(got["owner"]) != owner.Address.String() {
			t.Errorf("owner changed by a non-owner: %v", got["owner"])
		}
	})

	t.Run("TK-9_update_to_non_mintable_then_mint_fails", func(t *testing.T) {
		cover(t, "E1", "contract-method", "token.UpdateToken")
		cover(t, "E4", "contract-method", "token.Mint")
		callOK(t, owner, token, definition.ABIToken.PackMethodPanic(definition.UpdateTokenMethodName, zts, owner.Address, false, true), znn, units(0))
		got := info(t)
		if str(got["isMintable"]) != "false" {
			t.Errorf("isMintable after update: %v", got["isMintable"])
		}
		// Giving up minting fixes the maximum supply at the current supply.
		t.Logf("after the update: totalSupply=%v maxSupply=%v", got["totalSupply"], got["maxSupply"])
		callFail(t, owner, token, mint(units(1), owner.Address), znn, units(0), watch("embedded.token.getByZts", zts))
	})

	t.Run("TK-10_update_owner", func(t *testing.T) {
		cover(t, "E1", "contract-method", "token.UpdateToken")
		callOK(t, owner, token, definition.ABIToken.PackMethodPanic(definition.UpdateTokenMethodName, zts, other.Address, false, true), znn, units(0))
		if got := info(t); str(got["owner"]) != other.Address.String() {
			t.Errorf("owner after update: %v", got["owner"])
		}
	})
}

func TestStake(t *testing.T) {
	group(t)
	a := newAccount(t, 30, 50, 0, 1000)
	other := newAccount(t, 31, 0, 0, 300)
	stake := types.StakeContract
	month := constants.StakeTimeUnitSec
	data := func(sec int64) []byte { return definition.ABIStake.PackMethodPanic(definition.StakeMethodName, sec) }
	var id types.Hash

	t.Run("ST-1_stake_each_end_of_the_duration_range", func(t *testing.T) {
		cover(t, "E1", "contract-method", "stake.Stake")
		cover(t, "E2", "contract-method", "stake.Stake")
		before := bal(t, a.Address, znn)
		id = callOK(t, a, stake, data(month), znn, coins(10)).Send.Hash
		callOK(t, a, stake, data(12*month), znn, constants.StakeMinAmount)
		wantBig(t, "ZNN after staking", bal(t, a.Address, znn), sub(before, coins(11)))
		var list obj
		get(t, &list, "embedded.stake.getEntriesByAddress", a.Address, 0, 10)
		if str(list["count"]) != "2" || str(list["totalAmount"]) != coins(11).String() {
			t.Errorf("stake entries: %v", list)
		}
	})

	t.Run("ST-2_stake_refusals", func(t *testing.T) {
		cover(t, "E2", "contract-method", "stake.Stake")
		cover(t, "E5", "contract-method", "stake.Stake")
		cover(t, "E2", "error", "vm/constants.ErrInvalidStakingPeriod")
		period, bad := constants.ErrInvalidStakingPeriod.Error(), constants.ErrInvalidTokenOrAmount.Error()
		callReject(t, bad, a, stake, data(month), znn, sub(constants.StakeMinAmount, units(1)))
		callReject(t, bad, other, stake, data(month), qsr, coins(1))
		callReject(t, period, a, stake, data(month-1), znn, coins(1))
		callReject(t, period, a, stake, data(month+1), znn, coins(1))
		callReject(t, period, a, stake, data(13*month), znn, coins(1))
		callReject(t, period, a, stake, data(0), znn, coins(1))
		callReject(t, period, a, stake, data(-month), znn, coins(1))
	})

	t.Run("ST-3_cancel_before_expiry_fails", func(t *testing.T) {
		cover(t, "E4", "contract-method", "stake.Cancel")
		cover(t, "E12", "contract-method", "stake.Cancel")
		before := bal(t, a.Address, znn)
		callFail(t, a, stake, definition.ABIStake.PackMethodPanic(definition.CancelStakeMethodName, id), znn, units(0), watch("embedded.stake.getEntriesByAddress", a.Address, 0, 10))
		wantBig(t, "ZNN unchanged", bal(t, a.Address, znn), before)
	})

	t.Run("ST-4_cancel_by_another_account_fails", func(t *testing.T) {
		cover(t, "E3", "contract-method", "stake.Cancel")
		callFail(t, other, stake, definition.ABIStake.PackMethodPanic(definition.CancelStakeMethodName, id), znn, units(0), watch("embedded.stake.getEntriesByAddress", a.Address, 0, 10))
		var list obj
		get(t, &list, "embedded.stake.getEntriesByAddress", a.Address, 0, 10)
		if str(list["count"]) != "2" {
			t.Errorf("stake entries after failed cancels: %v", list)
		}
	})

	t.Run("ST-5_reward_getters", func(t *testing.T) {
		var reward, page obj
		get(t, &reward, "embedded.stake.getUncollectedReward", a.Address)
		get(t, &page, "embedded.stake.getFrontierRewardByPage", a.Address, 0, 10)
		if str(reward["qsrAmount"]) != "0" {
			t.Errorf("uncollected reward before any epoch: %v", reward)
		}
	})
}

func TestHtlc(t *testing.T) {
	group(t)
	needs(t, "htlc")
	a := newAccount(t, 40, 20, 0, 1500) // creator, the time-locked side
	b := newAccount(t, 41, 0, 0, 1000)  // the hash-locked side
	htlc := types.HtlcContract
	preimage := []byte("e2e preimage " + runTag)
	sha3Lock := crypto.Hash(preimage)
	sha2 := sha256.Sum256(preimage)
	create := func(to types.Address, expiry int64, hashType, keyMax uint8, lock []byte) []byte {
		return definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName, to, expiry, hashType, keyMax, lock)
	}
	unlock := func(id types.Hash, pre []byte) []byte {
		return definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName, id, pre)
	}
	reclaim := func(id types.Hash) []byte {
		return definition.ABIHtlc.PackMethodPanic(definition.ReclaimHtlcMethodName, id)
	}
	now := func(t *testing.T) int64 {
		m, err := net.Frontier()
		if err != nil {
			t.Fatal(err)
		}
		return int64(m.TimestampUnix)
	}
	var id types.Hash

	t.Run("HT-1_create", func(t *testing.T) {
		cover(t, "E1", "contract-method", "htlc.Create")
		before := bal(t, a.Address, znn)
		expiry := now(t) + 3600
		id = callOK(t, a, htlc, create(b.Address, expiry, definition.HashTypeSHA3, 32, sha3Lock), znn, coins(2)).Send.Hash
		wantBig(t, "ZNN locked", bal(t, a.Address, znn), sub(before, coins(2)))
		var got obj
		get(t, &got, "embedded.htlc.getById", id)
		if str(got["timeLocked"]) != a.Address.String() || str(got["hashLocked"]) != b.Address.String() ||
			str(got["amount"]) != coins(2).String() || str(got["expirationTime"]) != fmt.Sprint(expiry) {
			t.Errorf("htlc read back: %v", got)
		}
	})

	t.Run("HT-2_create_refusals", func(t *testing.T) {
		cover(t, "E2", "contract-method", "htlc.Create")
		cover(t, "E5", "contract-method", "htlc.Create")
		cover(t, "E2", "error", "vm/constants.ErrInvalidHashType", "vm/constants.ErrInvalidHashDigest")
		expiry := now(t) + 3600
		callReject(t, constants.ErrInvalidHashType.Error(), a, htlc, create(b.Address, expiry, 2, 32, sha3Lock), znn, coins(1))
		callReject(t, constants.ErrInvalidHashDigest.Error(), a, htlc, create(b.Address, expiry, definition.HashTypeSHA3, 32, sha3Lock[:31]), znn, coins(1))
		callReject(t, constants.ErrInvalidHashDigest.Error(), a, htlc, create(b.Address, expiry, definition.HashTypeSHA3, 32, append(append([]byte{}, sha3Lock...), 0)), znn, coins(1))
		callReject(t, constants.ErrInvalidTokenOrAmount.Error(), a, htlc, create(b.Address, expiry, definition.HashTypeSHA3, 32, sha3Lock), znn, units(0))
	})

	t.Run("HT-3_create_already_expired_fails_and_refunds", func(t *testing.T) {
		cover(t, "E12", "contract-method", "htlc.Create")
		before := bal(t, a.Address, znn)
		res := callFail(t, a, htlc, create(b.Address, now(t)-1, definition.HashTypeSHA3, 32, sha3Lock), znn, coins(1))
		if len(res.Receive.DescendantBlocks) != 1 {
			t.Fatalf("failed call with an amount produced %d descendant blocks, want 1 refund", len(res.Receive.DescendantBlocks))
		}
		receiveAll(t, a, 1)
		wantBig(t, "ZNN after the refund", bal(t, a.Address, znn), before)
	})

	t.Run("HT-4_unlock_with_a_wrong_preimage_fails", func(t *testing.T) {
		cover(t, "E5", "contract-method", "htlc.Unlock")
		callFail(t, b, htlc, unlock(id, []byte("wrong")), znn, units(0), watch("embedded.htlc.getById", id))
		callFail(t, b, htlc, unlock(id, make([]byte, 33)), znn, units(0), watch("embedded.htlc.getById", id)) // longer than keyMaxSize
		var got obj
		get(t, &got, "embedded.htlc.getById", id)
	})

	t.Run("HT-5_deny_then_allow_proxy_unlock", func(t *testing.T) {
		cover(t, "E1", "contract-method", "htlc.DenyProxyUnlock", "htlc.AllowProxyUnlock")
		cover(t, "E3", "contract-method", "htlc.Unlock")
		var allowed bool
		get(t, &allowed, "embedded.htlc.getProxyUnlockStatus", b.Address)
		if !allowed {
			t.Errorf("proxy unlock is not allowed by default")
		}
		callOK(t, b, htlc, definition.ABIHtlc.PackMethodPanic(definition.DenyHtlcProxyUnlockMethodName), znn, units(0))
		get(t, &allowed, "embedded.htlc.getProxyUnlockStatus", b.Address)
		if allowed {
			t.Errorf("proxy unlock still allowed after deny")
		}
		// With proxy unlock denied, nobody but the hash-locked address may unlock.
		callFail(t, a, htlc, unlock(id, preimage), znn, units(0), watch("embedded.htlc.getById", id))
		callOK(t, b, htlc, definition.ABIHtlc.PackMethodPanic(definition.AllowHtlcProxyUnlockMethodName), znn, units(0))
		get(t, &allowed, "embedded.htlc.getProxyUnlockStatus", b.Address)
		if !allowed {
			t.Errorf("proxy unlock not allowed after allow")
		}
	})

	t.Run("HT-6_reclaim_before_expiry_fails", func(t *testing.T) {
		cover(t, "E12", "contract-method", "htlc.Reclaim")
		callFail(t, a, htlc, reclaim(id), znn, units(0), watch("embedded.htlc.getById", id))
	})

	t.Run("HT-7_reclaim_by_the_wrong_account_fails", func(t *testing.T) {
		cover(t, "E3", "contract-method", "htlc.Reclaim")
		callFail(t, b, htlc, reclaim(id), znn, units(0), watch("embedded.htlc.getById", id))
	})

	t.Run("HT-8_unlock_pays_the_hash_locked_address", func(t *testing.T) {
		cover(t, "E1", "contract-method", "htlc.Unlock")
		before := bal(t, b.Address, znn)
		res := callOK(t, b, htlc, unlock(id, preimage), znn, units(0))
		receiveAll(t, b, 1)
		wantBig(t, "ZNN paid out", bal(t, b.Address, znn), add(before, coins(2)))
		var got obj
		if err := net.Call(&got, "embedded.htlc.getById", id); err == nil {
			t.Errorf("htlc still readable after unlock: %v", got)
		}
		if err := net.Agreement(res.Receive.Hash, wait); err != nil {
			t.Error(err)
		}
	})

	t.Run("HT-9_second_unlock_fails", func(t *testing.T) {
		cover(t, "E4", "contract-method", "htlc.Unlock")
		callFail(t, b, htlc, unlock(id, preimage), znn, units(0), watchMissing(constants.ErrDataNonExistent.Error(), "embedded.htlc.getById", id))
	})

	t.Run("HT-10_sha256_lock_unlocked_by_a_proxy", func(t *testing.T) {
		cover(t, "E1", "contract-method", "htlc.Create", "htlc.Unlock")
		id2 := callOK(t, a, htlc, create(b.Address, now(t)+3600, definition.HashTypeSHA256, 255, sha2[:]), znn, coins(1)).Send.Hash
		before := bal(t, b.Address, znn)
		callOK(t, a, htlc, unlock(id2, preimage), znn, units(0)) // sent by the creator, paid to the hash-locked address
		receiveAll(t, b, 1)
		wantBig(t, "ZNN paid to the hash-locked address", bal(t, b.Address, znn), add(before, coins(1)))
	})

	t.Run("HT-11_after_expiry_unlock_fails_and_reclaim_pays_back", func(t *testing.T) {
		cover(t, "E12", "contract-method", "htlc.Unlock", "htlc.Reclaim")
		cover(t, "E1", "contract-method", "htlc.Reclaim")
		before := bal(t, a.Address, znn)
		expiry := now(t) + 40
		id3 := callOK(t, a, htlc, create(b.Address, expiry, definition.HashTypeSHA3, 32, sha3Lock), znn, coins(3)).Send.Hash
		for now(t) < expiry {
			time.Sleep(2 * time.Second)
		}
		callFail(t, b, htlc, unlock(id3, preimage), znn, units(0), watch("embedded.htlc.getById", id3))
		callOK(t, a, htlc, reclaim(id3), znn, units(0))
		receiveAll(t, a, 1)
		wantBig(t, "ZNN after reclaim", bal(t, a.Address, znn), before)
	})
}

func TestPillar(t *testing.T) {
	group(t)
	a := newAccount(t, 50, 15010, 200, 1500)
	pillar := types.PillarContract
	pack := func(name string, args ...interface{}) []byte {
		return definition.ABIPillars.PackMethodPanic(name, args...)
	}
	delegated := func(t *testing.T) interface{} {
		var got interface{}
		get(t, &got, "embedded.pillar.getDelegatedPillar", a.Address)
		return got
	}

	t.Run("PI-1_getters", func(t *testing.T) {
		var all, byName, history, epoch obj
		var byOwner []interface{}
		var free bool
		var cost string
		get(t, &all, "embedded.pillar.getAll", 0, 10)
		get(t, &byName, "embedded.pillar.getByName", "dev1")
		get(t, &byOwner, "embedded.pillar.getByOwner", net.Key(IdxOwner1).Address)
		get(t, &free, "embedded.pillar.checkNameAvailability", "dev1")
		get(t, &cost, "embedded.pillar.getQsrRegistrationCost")
		get(t, &history, "embedded.pillar.getPillarEpochHistory", "dev1", 0, 10)
		get(t, &epoch, "embedded.pillar.getPillarsHistoryByEpoch", 0, 0, 10)
		if str(all["count"]) != "3" || str(byName["name"]) != "dev1" || len(byOwner) != 1 || free {
			t.Errorf("pillar getters: count=%v name=%v owned=%d dev1 free=%v", all["count"], byName["name"], len(byOwner), free)
		}
		t.Logf("QSR registration cost: %s", cost)
	})

	t.Run("PI-2_delegate_and_change", func(t *testing.T) {
		cover(t, "E1", "contract-method", "pillars.Delegate")
		callOK(t, a, pillar, pack(definition.DelegateMethodName, "dev1"), znn, units(0))
		if got, _ := delegated(t).(obj); str(got["name"]) != "dev1" {
			t.Errorf("delegation: %v", got)
		}
		callOK(t, a, pillar, pack(definition.DelegateMethodName, "dev2"), znn, units(0))
		if got, _ := delegated(t).(obj); str(got["name"]) != "dev2" {
			t.Errorf("delegation after change: %v", got)
		}
	})

	t.Run("PI-3_delegate_refusals", func(t *testing.T) {
		cover(t, "E4", "contract-method", "pillars.Delegate")
		cover(t, "E5", "contract-method", "pillars.Delegate")
		callFail(t, a, pillar, pack(definition.DelegateMethodName, "no-such-pillar"), znn, units(0), watch("embedded.pillar.getDelegatedPillar", a.Address))
		callReject(t, constants.ErrInvalidTokenOrAmount.Error(), a, pillar, pack(definition.DelegateMethodName, "dev1"), znn, units(1))
		callReject(t, constants.ErrInvalidName.Error(), a, pillar, pack(definition.DelegateMethodName, ""), znn, units(0))
		if got, _ := delegated(t).(obj); str(got["name"]) != "dev2" {
			t.Errorf("delegation changed by a refused call: %v", got)
		}
	})

	t.Run("PI-4_undelegate_then_again", func(t *testing.T) {
		cover(t, "E1", "contract-method", "pillars.Undelegate")
		cover(t, "E4", "contract-method", "pillars.Undelegate")
		callOK(t, a, pillar, pack(definition.UndelegateMethodName), znn, units(0))
		if got := delegated(t); got != nil {
			t.Errorf("delegation after undelegate: %v", got)
		}
		callFail(t, a, pillar, pack(definition.UndelegateMethodName), znn, units(0), watchNull("embedded.pillar.getDelegatedPillar", a.Address))
	})

	t.Run("PI-5_deposit_and_withdraw_qsr", func(t *testing.T) {
		cover(t, "E1", "contract-method", "pillars.DepositQsr", "pillars.WithdrawQsr")
		cover(t, "E4", "contract-method", "pillars.WithdrawQsr")
		cover(t, "E5", "contract-method", "pillars.DepositQsr")
		before := bal(t, a.Address, qsr)
		callOK(t, a, pillar, pack(definition.DepositQsrMethodName), qsr, coins(100))
		var deposited string
		get(t, &deposited, "embedded.pillar.getDepositedQsr", a.Address)
		if deposited != coins(100).String() {
			t.Errorf("deposited QSR: %s", deposited)
		}
		callReject(t, constants.ErrInvalidTokenOrAmount.Error(), a, pillar, pack(definition.DepositQsrMethodName), znn, coins(1))
		callOK(t, a, pillar, pack(definition.WithdrawQsrMethodName), znn, units(0))
		receiveAll(t, a, 1)
		wantBig(t, "QSR after withdraw", bal(t, a.Address, qsr), before)
		callFail(t, a, pillar, pack(definition.WithdrawQsrMethodName), znn, units(0), watch("embedded.pillar.getDepositedQsr", a.Address)) // nothing left
	})

	t.Run("PI-6_register_without_the_qsr_deposit_fails_and_refunds", func(t *testing.T) {
		cover(t, "E4", "contract-method", "pillars.Register")
		before := bal(t, a.Address, znn)
		producer := net.Key(runBase + 51).Address
		res := callFail(t, a, pillar, pack(definition.RegisterMethodName, "e2e-"+runTag, producer, a.Address, uint8(0), uint8(100)), znn, constants.PillarStakeAmount)
		if len(res.Receive.DescendantBlocks) != 1 {
			t.Fatalf("failed registration produced %d descendant blocks, want 1 refund", len(res.Receive.DescendantBlocks))
		}
		receiveAll(t, a, 1)
		wantBig(t, "ZNN after the refund", bal(t, a.Address, znn), before)
		var all obj
		get(t, &all, "embedded.pillar.getAll", 0, 10)
		if str(all["count"]) != "3" {
			t.Errorf("pillar count after a failed registration: %v", all["count"])
		}
	})

	t.Run("PI-7_register_refusals_at_publish", func(t *testing.T) {
		cover(t, "E2", "contract-method", "pillars.Register")
		cover(t, "E5", "contract-method", "pillars.Register")
		producer := net.Key(runBase + 51).Address
		reg := func(name string, give, delegate uint8) []byte {
			return pack(definition.RegisterMethodName, name, producer, a.Address, give, delegate)
		}
		callReject(t, constants.ErrInvalidTokenOrAmount.Error(), a, pillar, reg("e2e-x", 0, 100), znn, sub(constants.PillarStakeAmount, units(1)))
		callReject(t, constants.ErrInvalidName.Error(), a, pillar, reg(strings.Repeat("a", 41), 0, 100), znn, constants.PillarStakeAmount)
		callReject(t, constants.ErrForbiddenParam.Error(), a, pillar, reg("e2e-x", 101, 100), znn, constants.PillarStakeAmount)
		callReject(t, constants.ErrForbiddenParam.Error(), a, pillar, reg("e2e-x", 0, 101), znn, constants.PillarStakeAmount)
	})

	t.Run("PI-8_owner_only_methods_from_a_stranger_fail", func(t *testing.T) {
		cover(t, "E3", "contract-method", "pillars.Revoke", "pillars.UpdatePillar")
		// Everything a stranger's update or revoke could change, read before
		// and after. (The whole reply cannot be compared: it also carries the
		// pillar's running production counts.)
		fields := []string{"ownerAddress", "producerAddress", "withdrawAddress", "giveMomentumRewardPercentage", "giveDelegateRewardPercentage", "revokeTimestamp", "name", "type"}
		read := func() map[string]string {
			var byName obj
			get(t, &byName, "embedded.pillar.getByName", "dev1")
			out := map[string]string{}
			for _, f := range fields {
				if _, ok := byName[f]; !ok {
					t.Fatalf("dev1 has no field %s: %v", f, byName)
				}
				out[f] = str(byName[f])
			}
			return out
		}
		before := read()
		if before["ownerAddress"] != net.Key(IdxOwner1).Address.String() || before["revokeTimestamp"] != "0" {
			t.Fatalf("dev1 before the calls: %v", before)
		}
		callFail(t, a, pillar, pack(definition.RevokeMethodName, "dev1"), znn, units(0))
		callFail(t, a, pillar, pack(definition.UpdatePillarMethodName, "dev1", a.Address, a.Address, uint8(0), uint8(100)), znn, units(0))
		after := read()
		for _, f := range fields {
			if after[f] != before[f] {
				t.Errorf("dev1 %s after a stranger's refused calls: %s, before: %s", f, after[f], before[f])
			}
		}
	})

	t.Run("PI-9_revoke_by_the_owner_outside_the_window_fails", func(t *testing.T) {
		// A pillar is revocable for 7 days in every 90, counted from its
		// registration time. The genesis pillars are registered at time 0, so
		// the cycle is counted from 1 January 1970 and whether dev3 is locked
		// depends on the date. What the node says is held against that
		// calendar, computed here, on every date; the refusal itself can only
		// be tried while the pillar is locked. Inside the window the refusal
		// is exercised on the test build only (FPI-4).
		period := constants.PillarEpochLockTime + constants.PillarEpochRevokeTime
		var info struct {
			IsRevocable    *bool  `json:"isRevocable"`
			RevokeCooldown *int64 `json:"revokeCooldown"`
		}
		// The getter uses the frontier momentum's time, so the two readings
		// of it around the call bound the answer. A reading across the
		// moment the window opens or closes is taken again.
		var before, after int64
		var locked bool
		for try := 0; ; try++ {
			info.IsRevocable, info.RevokeCooldown = nil, nil
			before = frontierTime(t) % period
			get(t, &info, "embedded.pillar.getByName", "dev3")
			after = frontierTime(t) % period
			locked = before < constants.PillarEpochLockTime
			if after >= before && locked == (after < constants.PillarEpochLockTime) {
				break
			}
			if try == 5 {
				t.Fatalf("six readings of dev3 in a row straddled the edge of its revoke window")
			}
			time.Sleep(15 * time.Second)
		}
		if info.IsRevocable == nil || info.RevokeCooldown == nil {
			t.Fatalf("embedded.pillar.getByName(dev3) has no isRevocable or no revokeCooldown")
		}
		end := period
		if locked {
			end = constants.PillarEpochLockTime
		}
		if *info.IsRevocable == locked || *info.RevokeCooldown > end-before || *info.RevokeCooldown < end-after {
			t.Fatalf("dev3 reads isRevocable=%v, cooldown %d s; %d to %d s into the 90-day cycle the pillar is locked=%v with %d to %d s left",
				*info.IsRevocable, *info.RevokeCooldown, before, after, locked, end-after, end-before)
		}
		if !locked {
			t.Logf("dev3 is inside its revoke window (%d s left); the refusal is not tried, revoking would remove a devnet pillar", *info.RevokeCooldown)
			return
		}
		// The revoke is published only if the lock outlasts its execution by
		// a wide margin: one that executed after the window opened would
		// succeed and take a pillar off the devnet.
		if left := end - after; left < 1800 {
			t.Logf("dev3 is locked for only %d s more; the refusal is not tried this close to the window", left)
			return
		}
		cover(t, "E12", "contract-method", "pillars.Revoke")
		res, err := call(net.Key(IdxOwner3), pillar, pack(definition.RevokeMethodName, "dev3"), znn, units(0))
		mustFail(t, res, err)
		var all obj
		get(t, &all, "embedded.pillar.getAll", 0, 10)
		if str(all["count"]) != "3" {
			t.Errorf("pillar count: %v", all["count"])
		}
	})
}

func TestSentinel(t *testing.T) {
	group(t)
	a := newAccount(t, 60, 10010, 50100, 1500)
	sentinel := types.SentinelContract
	pack := func(name string) []byte { return definition.ABISentinel.PackMethodPanic(name) }
	active := func(t *testing.T) int {
		var all obj
		get(t, &all, "embedded.sentinel.getAllActive", 0, 10)
		list, _ := all["list"].([]interface{})
		return len(list)
	}

	t.Run("SE-1_register_without_the_qsr_deposit_fails_and_refunds", func(t *testing.T) {
		cover(t, "E4", "contract-method", "sentinel.Register")
		before := bal(t, a.Address, znn)
		res := callFail(t, a, sentinel, pack(definition.RegisterSentinelMethodName), znn, constants.SentinelZnnRegisterAmount)
		if len(res.Receive.DescendantBlocks) != 1 {
			t.Fatalf("failed registration produced %d descendant blocks, want 1 refund", len(res.Receive.DescendantBlocks))
		}
		receiveAll(t, a, 1)
		wantBig(t, "ZNN after the refund", bal(t, a.Address, znn), before)
	})

	t.Run("SE-2_deposit_and_register", func(t *testing.T) {
		cover(t, "E1", "contract-method", "sentinel.DepositQsr", "sentinel.Register")
		n := active(t)
		callOK(t, a, sentinel, pack(definition.DepositQsrMethodName), qsr, constants.SentinelQsrDepositAmount)
		var deposited string
		get(t, &deposited, "embedded.sentinel.getDepositedQsr", a.Address)
		if deposited != constants.SentinelQsrDepositAmount.String() {
			t.Errorf("deposited QSR: %s", deposited)
		}
		callOK(t, a, sentinel, pack(definition.RegisterSentinelMethodName), znn, constants.SentinelZnnRegisterAmount)
		var mine obj
		get(t, &mine, "embedded.sentinel.getByOwner", a.Address)
		if str(mine["owner"]) != a.Address.String() || str(mine["active"]) != "true" {
			t.Errorf("sentinel read back: %v", mine)
		}
		if got := active(t); got != n+1 {
			t.Errorf("active sentinels: got %d, want %d", got, n+1)
		}
		get(t, &deposited, "embedded.sentinel.getDepositedQsr", a.Address)
		if deposited != "0" {
			t.Errorf("deposited QSR after registration: %s, want 0", deposited)
		}
	})

	t.Run("SE-3_register_refusals", func(t *testing.T) {
		cover(t, "E2", "contract-method", "sentinel.Register")
		cover(t, "E5", "contract-method", "sentinel.Register")
		bad := constants.ErrInvalidTokenOrAmount.Error()
		callReject(t, bad, a, sentinel, pack(definition.RegisterSentinelMethodName), znn, sub(constants.SentinelZnnRegisterAmount, units(1)))
		callReject(t, bad, a, sentinel, pack(definition.RegisterSentinelMethodName), znn, add(constants.SentinelZnnRegisterAmount, units(1)))
		callReject(t, bad, a, sentinel, pack(definition.RegisterSentinelMethodName), qsr, constants.SentinelZnnRegisterAmount)
	})

	t.Run("SE-4_second_registration_fails_and_refunds", func(t *testing.T) {
		cover(t, "E4", "contract-method", "sentinel.Register")
		before := bal(t, a.Address, znn)
		callFail(t, a, sentinel, pack(definition.RegisterSentinelMethodName), znn, constants.SentinelZnnRegisterAmount)
		receiveAll(t, a, 1)
		wantBig(t, "ZNN after the refund", bal(t, a.Address, znn), before)
	})

	t.Run("SE-5_revoke_outside_the_window_fails", func(t *testing.T) {
		cover(t, "E12", "contract-method", "sentinel.Revoke")
		callFail(t, a, sentinel, pack(definition.RevokeSentinelMethodName), znn, units(0))
		var mine obj
		get(t, &mine, "embedded.sentinel.getByOwner", a.Address)
		if str(mine["active"]) != "true" {
			t.Errorf("sentinel after a failed revoke: %v", mine)
		}
	})

	t.Run("SE-6_withdraw_with_nothing_deposited_fails", func(t *testing.T) {
		cover(t, "E4", "contract-method", "sentinel.WithdrawQsr")
		callFail(t, a, sentinel, pack(definition.WithdrawQsrMethodName), znn, units(0), watch("embedded.sentinel.getDepositedQsr", a.Address))
	})

	t.Run("SE-7_reward_getters", func(t *testing.T) {
		var reward, page obj
		get(t, &reward, "embedded.sentinel.getUncollectedReward", a.Address)
		get(t, &page, "embedded.sentinel.getFrontierRewardByPage", a.Address, 0, 10)
		if str(reward["address"]) != a.Address.String() || str(reward["znnAmount"]) != "0" || str(reward["qsrAmount"]) != "0" || count(t, page, "count") != 0 {
			t.Errorf("rewards of a sentinel registered minutes ago: %v, history %v; want none", reward, page)
		}
	})
}

func TestAccelerator(t *testing.T) {
	group(t)
	needs(t, "accelerator")
	a := newAccount(t, 70, 10, 10, 1500)
	az := types.AcceleratorContract
	pack := func(name string, args ...interface{}) []byte {
		return definition.ABIAccelerator.PackMethodPanic(name, args...)
	}
	project := func(name, desc, url string, znnFunds, qsrFunds *big.Int) []byte {
		return pack(definition.CreateProjectMethodName, name, desc, url, znnFunds, qsrFunds)
	}
	var id types.Hash

	t.Run("AZ-1_donate", func(t *testing.T) {
		cover(t, "E1", "contract-method", "accelerator.Donate")
		cover(t, "E5", "contract-method", "accelerator.Donate")
		before := bal(t, az, znn)
		callOK(t, a, az, pack(definition.DonateMethodName), znn, coins(1))
		wantBig(t, "accelerator ZNN", bal(t, az, znn), add(before, coins(1)))
		callReject(t, constants.ErrInvalidTokenOrAmount.Error(), a, az, pack(definition.DonateMethodName), znn, units(0))
	})

	t.Run("AZ-2_create_project", func(t *testing.T) {
		cover(t, "E1", "contract-method", "accelerator.CreateProject")
		id = callOK(t, a, az, project("e2e "+runTag, "driver project", "e2e.test", coins(100), coins(1000)), znn, constants.ProjectCreationAmount).Send.Hash
		var got, all obj
		get(t, &got, "embedded.accelerator.getProjectById", id)
		get(t, &all, "embedded.accelerator.getAll", 0, 10)
		if str(got["owner"]) != a.Address.String() || str(got["znnFundsNeeded"]) != coins(100).String() || str(got["status"]) != "0" {
			t.Errorf("project read back: %v", got)
		}
	})

	t.Run("AZ-3_create_project_refusals", func(t *testing.T) {
		cover(t, "E2", "contract-method", "accelerator.CreateProject")
		cover(t, "E5", "contract-method", "accelerator.CreateProject")
		fee := constants.ProjectCreationAmount
		callReject(t, constants.ErrInvalidTokenOrAmount.Error(), a, az, project("ok", "d", "e2e.test", coins(1), coins(1)), znn, sub(fee, units(1)))
		callReject(t, constants.ErrInvalidTokenOrAmount.Error(), a, az, project("ok", "d", "e2e.test", coins(1), coins(1)), qsr, fee)
		callReject(t, constants.ErrAcceleratorInvalidFunds.Error(), a, az, project("ok", "d", "e2e.test", add(constants.ProjectZnnMaximumFunds, units(1)), coins(1)), znn, fee)
		callReject(t, constants.ErrAcceleratorInvalidFunds.Error(), a, az, project("ok", "d", "e2e.test", coins(1), add(constants.ProjectQsrMaximumFunds, units(1))), znn, fee)
		callReject(t, constants.ErrInvalidName.Error(), a, az, project(strings.Repeat("n", constants.ProjectNameLengthMax+1), "d", "e2e.test", coins(1), coins(1)), znn, fee)
		callReject(t, constants.ErrInvalidName.Error(), a, az, project("", "d", "e2e.test", coins(1), coins(1)), znn, fee)
		callReject(t, constants.ErrInvalidDescription.Error(), a, az, project("ok", strings.Repeat("d", constants.ProjectDescriptionLengthMax+1), "e2e.test", coins(1), coins(1)), znn, fee)
	})

	t.Run("AZ-4_vote_by_pillar_owners", func(t *testing.T) {
		cover(t, "E1", "contract-method", "accelerator.VoteByName")
		callOK(t, net.Key(IdxOwner1), az, pack(definition.VoteByNameMethodName, id, "dev1", definition.VoteYes), znn, units(0))
		callOK(t, net.Key(IdxOwner2), az, pack(definition.VoteByNameMethodName, id, "dev2", definition.VoteNo), znn, units(0))
		var votes obj
		get(t, &votes, "embedded.accelerator.getVoteBreakdown", id)
		if str(votes["yes"]) != "1" || str(votes["no"]) != "1" || str(votes["total"]) != "2" {
			t.Errorf("vote breakdown: %v", votes)
		}
		var pillarVotes []interface{}
		get(t, &pillarVotes, "embedded.accelerator.getPillarVotes", "dev1", []types.Hash{id})
		if len(pillarVotes) != 1 || pillarVotes[0] == nil {
			t.Errorf("pillar votes: %v", pillarVotes)
		}
	})

	t.Run("AZ-5_vote_refusals", func(t *testing.T) {
		cover(t, "E3", "contract-method", "accelerator.VoteByName", "accelerator.VoteByProdAddress")
		cover(t, "E2", "contract-method", "accelerator.VoteByName")
		cover(t, "E4", "contract-method", "accelerator.VoteByName")
		callFail(t, a, az, pack(definition.VoteByNameMethodName, id, "dev1", definition.VoteYes), znn, units(0), watch("embedded.accelerator.getVoteBreakdown", id)) // not the pillar's owner
		callFail(t, a, az, pack(definition.VoteByProdAddressMethodName, id, definition.VoteYes), znn, units(0), watch("embedded.accelerator.getVoteBreakdown", id))  // not a producer address
		// A value that is not a vote is refused when the send is validated.
		callReject(t, constants.ErrForbiddenParam.Error(), net.Key(IdxOwner1), az, pack(definition.VoteByNameMethodName, id, "dev1", definition.VoteNotValid), znn, units(0))
		callFail(t, net.Key(IdxOwner1), az, pack(definition.VoteByNameMethodName, types.NewHash([]byte("no project "+runTag)), "dev1", definition.VoteYes), znn, units(0), watch("embedded.accelerator.getVoteBreakdown", id))
		var votes obj
		get(t, &votes, "embedded.accelerator.getVoteBreakdown", id)
		if str(votes["total"]) != "2" {
			t.Errorf("vote breakdown after refused votes: %v", votes)
		}
	})

	t.Run("AZ-6_add_phase_to_a_project_that_is_not_accepted_fails", func(t *testing.T) {
		cover(t, "E4", "contract-method", "accelerator.AddPhase")
		phase := pack(definition.AddPhaseMethodName, id, "phase", "d", "e2e.test", coins(1), coins(1))
		callFail(t, a, az, phase, znn, units(0))
		// Whether a stranger is refused is judged on an accepted project, where
		// only ownership can refuse the call: FAZ-2 on the fast build.
	})
}

func TestSpork(t *testing.T) {
	group(t)
	a := newAccount(t, 80, 0, 0, 500)
	sporkKey := net.Key(IdxProducer1) // the genesis spork address
	spork := types.SporkContract
	create := func(name, desc string) []byte {
		return definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName, name, desc)
	}
	activate := func(id types.Hash) []byte {
		return definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName, id)
	}
	find := func(t *testing.T, id types.Hash) obj {
		var all obj
		get(t, &all, "embedded.spork.getAll", 0, 50)
		list, _ := all["list"].([]interface{})
		for _, s := range list {
			if str(s.(obj)["id"]) == id.String() {
				return s.(obj)
			}
		}
		return nil
	}
	var id types.Hash

	t.Run("SP-1_create_and_activate_from_a_normal_address_refused", func(t *testing.T) {
		cover(t, "E3", "contract-method", "spork.CreateSpork", "spork.ActivateSpork")
		callReject(t, constants.ErrPermissionDenied.Error(), a, spork, create("e2e-denied", "d"), znn, units(0))
		callReject(t, constants.ErrPermissionDenied.Error(), a, spork, activate(types.NewHash([]byte("x"))), znn, units(0))
	})

	t.Run("SP-2_create_refusals", func(t *testing.T) {
		cover(t, "E2", "contract-method", "spork.CreateSpork")
		cover(t, "E5", "contract-method", "spork.CreateSpork")
		forbidden := constants.ErrForbiddenParam.Error()
		callReject(t, forbidden, sporkKey, spork, create("abcd", "d"), znn, units(0))                                                         // one under the minimum name length
		callReject(t, forbidden, sporkKey, spork, create(strings.Repeat("n", constants.SporkNameMaxLength+1), "d"), znn, units(0))            // one over the maximum
		callReject(t, forbidden, sporkKey, spork, create("e2e-x", strings.Repeat("d", constants.SporkDescriptionMaxLength+1)), znn, units(0)) // description too long
		callReject(t, constants.ErrInvalidTokenOrAmount.Error(), sporkKey, spork, create("e2e-x", "d"), znn, units(1))
	})

	t.Run("SP-3_create", func(t *testing.T) {
		cover(t, "E1", "contract-method", "spork.CreateSpork")
		id = callOK(t, sporkKey, spork, create("e2e-"+runTag, "created by the driver"), znn, units(0)).Send.Hash
		got := find(t, id)
		if got == nil || str(got["activated"]) != "false" || str(got["name"]) != "e2e-"+runTag {
			t.Fatalf("spork read back: %v", got)
		}
	})

	t.Run("SP-4_activate_unknown_id_fails", func(t *testing.T) {
		cover(t, "E4", "contract-method", "spork.ActivateSpork")
		callFail(t, sporkKey, spork, activate(types.NewHash([]byte("no spork "+runTag))), znn, units(0), watch("embedded.spork.getAll", 0, 50))
	})
}

// TestHaltOnUnknownSpork activates a spork this binary does not implement.
// Every node must then refuse to build the enforcement height and stop
// there: the chain is unusable afterwards and the devnet has to be wiped.
// It runs only with E2E_DESTRUCTIVE=1, and then it is the only test.
func TestHaltOnUnknownSpork(t *testing.T) {
	if os.Getenv("E2E_DESTRUCTIVE") != "1" {
		t.Skip("set E2E_DESTRUCTIVE=1 to run; the devnet must be wiped afterwards")
	}
	// SP-7 reads all five nodes. Establish that before SP-5 puts the spork on
	// chain: afterwards the chain cannot be used again.
	want := map[string]bool{}
	for _, name := range []string{"pillar", "pillar2", "pillar3", "rpc", "observer"} {
		want["znnd-devnet-"+name] = true
	}
	seen := map[string]bool{}
	for _, c := range net.Containers {
		if !want[c] || seen[c] {
			t.Fatalf("this group reads the five nodes once each; set E2E_NODES=pillar,pillar2,pillar3,rpc,observer (got %v)", net.Containers)
		}
		seen[c] = true
	}
	if len(seen) != len(want) {
		t.Fatalf("this group reads all five nodes; set E2E_NODES=pillar,pillar2,pillar3,rpc,observer (got %v)", net.Containers)
	}
	if _, err := net.Frontiers(); err != nil {
		t.Fatalf("a node does not answer before the activation: %v", err)
	}
	sporkKey := net.Key(IdxProducer1)
	spork := types.SporkContract
	find := func(t *testing.T, id types.Hash) obj {
		var all obj
		get(t, &all, "embedded.spork.getAll", 0, 50)
		list, _ := all["list"].([]interface{})
		for _, s := range list {
			if str(s.(obj)["id"]) == id.String() {
				return s.(obj)
			}
		}
		return nil
	}
	var id types.Hash
	var enforcement uint64

	t.Run("SP-5_activate", func(t *testing.T) {
		cover(t, "E1", "contract-method", "spork.ActivateSpork")
		id = callOK(t, sporkKey, spork, definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName, "e2e-halt-"+runTag, "not implemented by any binary"), znn, units(0)).Send.Hash
		res := callOK(t, sporkKey, spork, definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName, id), znn, units(0))
		got := find(t, id)
		if got == nil || str(got["activated"]) != "true" {
			t.Fatalf("spork after activation: %v", got)
		}
		// The contract sets the enforcement height to the frontier it ran on
		// plus the minimum delay, and the receive block acknowledges exactly
		// that frontier momentum (vm.finalizeEmbedded). So the height is
		// known exactly from the receive, whichever momentum confirmed it.
		ack := res.Receive.MomentumAcknowledged
		enforcement = ack.Height + constants.SporkMinHeightDelay
		if str(got["enforcementHeight"]) != fmt.Sprint(enforcement) {
			t.Fatalf("enforcement height %v, want %d: the receive acknowledges momentum %d and the delay is %d", got["enforcementHeight"], enforcement, ack.Height, constants.SporkMinHeightDelay)
		}
		var acked struct {
			List []struct {
				Hash types.Hash `json:"hash"`
			} `json:"list"`
		}
		get(t, &acked, "ledger.getMomentumsByHeight", ack.Height, 1)
		if len(acked.List) != 1 || acked.List[0].Hash != ack.Hash {
			t.Fatalf("the receive acknowledges %s at height %d, the chain has %v there", ack.Hash, ack.Height, acked.List)
		}
	})

	t.Run("SP-6_second_activation_fails", func(t *testing.T) {
		cover(t, "E4", "contract-method", "spork.ActivateSpork")
		res, err := call(sporkKey, spork, definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName, id), znn, units(0))
		mustFail(t, res, err)
		if after := find(t, id); str(after["enforcementHeight"]) != fmt.Sprint(enforcement) {
			t.Errorf("enforcement height moved by a second activation: %v, want %d", after, enforcement)
		}
	})

	t.Run("SP-7_every_node_stops_one_below_the_enforcement_height", func(t *testing.T) {
		if len(net.Containers) < 5 {
			t.Fatalf("this case reads all five nodes; set E2E_NODES=pillar,pillar2,pillar3,rpc,observer (got %v)", net.Containers)
		}
		if err := net.WaitHeight(enforcement-1, wait); err != nil {
			t.Fatal(err)
		}
		// Sampled three times over a minute and a half, which is nine producer
		// slots: every node, at the same height and on the same momentum.
		var first []NodeFrontier
		for sample := 0; sample < 3; sample++ {
			time.Sleep(30 * time.Second)
			nodes, err := net.Frontiers()
			if err != nil {
				t.Fatal(err)
			}
			if sample == 0 {
				first = nodes
			}
			for i, n := range nodes {
				if n.Height != enforcement-1 || n.Hash != nodes[0].Hash || n.Hash != first[i].Hash {
					t.Errorf("sample %d: %s is at height %d on %s; want every node stopped at %d on %s", sample, n.Node, n.Height, n.Hash, enforcement-1, first[0].Hash)
				}
			}
		}
		t.Logf("%d nodes stopped at height %d on momentum %s", len(first), enforcement-1, first[0].Hash)
	})
}

// rung says from which spork on a contract method is registered. The table
// below was written by reading vm/embedded/embedded.go by hand; it is the
// independent statement the node is compared with, so it must not be derived
// from that file by code.
type rung int

const (
	rOrigin rung = iota // from genesis, in every spork state
	rAZ                 // Accelerator, or any later spork of the ladder
	rBL                 // Bridge and Liquidity, or HTLC
	rHT                 // HTLC
	rPT                 // PTLC
	rDP                 // dynamic plasma
	rNever              // declared in the ABI, registered nowhere
)

func (r rung) registered() bool {
	switch r {
	case rOrigin:
		return true
	case rAZ:
		return live["accelerator"] || live["bridge"] || live["htlc"]
	case rBL:
		return live["bridge"] || live["htlc"]
	case rHT:
		return live["htlc"]
	case rPT:
		return live["ptlc"]
	case rDP:
		return live["dynamic-plasma"]
	}
	return false
}

type contractSpec struct {
	file    string // name in the inventory, the ABI's definition file
	addr    types.Address
	abi     abi.ABIContract
	methods map[string]rung
}

func same(r rung, names ...string) map[string]rung {
	m := map[string]rung{}
	for _, n := range names {
		m[n] = r
	}
	return m
}

func with(m map[string]rung, r rung, names ...string) map[string]rung {
	for _, n := range names {
		m[n] = r
	}
	return m
}

var registry = []contractSpec{
	{"plasma", types.PlasmaContract, definition.ABIPlasma, with(same(rOrigin, "Fuse", "CancelFuse"), rDP, "SetVariables")},
	{"pillars", types.PillarContract, definition.ABIPillars, same(rOrigin, "Register", "RegisterLegacy", "Revoke", "UpdatePillar", "Delegate", "Undelegate", "Update", "DepositQsr", "WithdrawQsr", "CollectReward")},
	{"token", types.TokenContract, definition.ABIToken, same(rOrigin, "IssueToken", "Mint", "Burn", "UpdateToken")},
	{"sentinel", types.SentinelContract, definition.ABISentinel, same(rOrigin, "Register", "Revoke", "Update", "DepositQsr", "WithdrawQsr", "CollectReward")},
	{"swap", types.SwapContract, definition.ABISwap, same(rOrigin, "RetrieveAssets")},
	{"stake", types.StakeContract, definition.ABIStake, same(rOrigin, "Stake", "Cancel", "Update", "CollectReward")},
	{"spork", types.SporkContract, definition.ABISpork, same(rOrigin, "CreateSpork", "ActivateSpork")},
	{"accelerator", types.AcceleratorContract, definition.ABIAccelerator, with(same(rAZ, "CreateProject", "AddPhase", "Update", "UpdatePhase", "VoteByName", "VoteByProdAddress"), rOrigin, "Donate")},
	{"liquidity", types.LiquidityContract, definition.ABILiquidity, with(with(same(rBL, "SetTokenTuple", "LiquidityStake", "CancelLiquidityStake", "UnlockLiquidityStakeEntries", "CollectReward", "SetIsHalted", "SetAdditionalReward", "ChangeAdministrator", "ProposeAdministrator", "NominateGuardians", "Emergency"),
		rAZ, "Fund", "BurnZnn"), rOrigin, "Update", "Donate")},
	{"bridge", types.BridgeContract, definition.ABIBridge, with(same(rBL, "WrapToken", "UpdateWrapRequest", "Redeem", "UnwrapToken", "RevokeUnwrapRequest", "SetNetwork", "RemoveNetwork", "SetTokenPair", "RemoveTokenPair", "Halt", "NominateGuardians", "Unhalt",
		"ProposeAdministrator", "Emergency", "ChangeTssECDSAPubKey", "ChangeAdministrator", "SetAllowKeyGen", "SetOrchestratorInfo", "SetBridgeMetadata", "SetNetworkMetadata"), rNever, "SetRedeemDelay")},
	{"htlc", types.HtlcContract, definition.ABIHtlc, same(rHT, "Create", "Reclaim", "Unlock", "DenyProxyUnlock", "AllowProxyUnlock")},
	{"ptlc", types.PtlcContract, definition.ABIPtlc, same(rPT, "Create", "Reclaim", "Unlock", "ProxyUnlock", "CreateWithReclaimLock", "ReclaimWithSignature")},
}

// TestSporkLadder compares, for every method of every embedded contract, what
// the node answers in this chain's spork state with the table above.
//
// The plasma quote RPC looks a call up in the node's registry and changes
// nothing, so it is asked about every address and selector on every endpoint:
// a registered method must be priced, a method that is not registered must
// answer "method not found in the abi", and a contract none of whose methods
// is registered must answer "contract doesn't exist". Nothing else passes,
// including a transport error. A method that is not registered is then also
// published, and the node must refuse it there with the same message.
func TestSporkLadder(t *testing.T) {
	group(t)
	a := newAccount(t, 90, 5, 5, 500)

	t.Run("LAD-1_registry_matches_the_table_for_every_method", func(t *testing.T) {
		cover(t, "E7", "spork", "AcceleratorSpork", "BridgeAndLiquiditySpork", "HtlcSpork", "PtlcSpork", "DynamicPlasmaSpork")
		cover(t, "E1", "rpc-method", "PlasmaApi.GetRequiredPoWForAccountBlock")
		checked := 0
		for _, c := range registry {
			if len(c.abi.Methods) != len(c.methods) {
				t.Fatalf("%s: the ABI declares %d methods, the table has %d; a method was added or removed and the table must be reviewed", c.file, len(c.abi.Methods), len(c.methods))
			}
			exists := false
			for _, r := range c.methods {
				exists = exists || r.registered()
			}
			for name, r := range c.methods {
				m, ok := c.abi.Methods[name]
				if !ok {
					t.Fatalf("%s.%s is in the table and not in the ABI", c.file, name)
				}
				want := "" // priced
				if !exists {
					want = constants.ErrContractDoesntExist.Error()
				} else if !r.registered() {
					want = constants.ErrContractMethodNotFound.Error()
				}
				for i, url := range net.URLs {
					to := c.addr
					var q plasmaQuote
					err := net.CallOn(i, &q, "embedded.plasma.getRequiredPoWForAccountBlock", plasmaParam{Address: a.Address, BlockType: nom.BlockTypeUserSend, ToAddress: &to, Data: m.Id()})
					switch {
					case want == "" && (err != nil || q.BasePlasma == 0):
						t.Errorf("%s: %s.%s must be registered in this spork state; the node answers %v, base plasma %d", url, c.file, name, err, q.BasePlasma)
					case want != "" && (err == nil || err.Error() != want):
						t.Errorf("%s: %s.%s must answer %q in this spork state; the node answers %v", url, c.file, name, want, err)
					}
				}
				checked++
			}
		}
		t.Logf("%d methods of %d contracts compared on %d endpoints", checked, len(registry), len(net.URLs))
	})

	t.Run("LAD-2_a_method_that_is_not_registered_is_refused_at_publish", func(t *testing.T) {
		refused := 0
		for _, c := range registry {
			exists := false
			for _, r := range c.methods {
				exists = exists || r.registered()
			}
			for name, r := range c.methods {
				if r.registered() {
					continue
				}
				want := constants.ErrContractMethodNotFound.Error()
				if !exists {
					want = constants.ErrContractDoesntExist.Error()
				}
				_, err := net.Send(a, &nom.AccountBlock{BlockType: nom.BlockTypeUserSend, ToAddress: c.addr, Data: c.abi.Methods[name].Id(), TokenStandard: znn, Amount: units(0)}, &Mutator{})
				mustReject(t, err, want)
				refused++
				// Only the error this run saw is marked.
				if exists {
					cover(t, "E7", "error", "vm/constants.ErrContractMethodNotFound")
				} else {
					cover(t, "E7", "error", "vm/constants.ErrContractDoesntExist")
				}
				if r != rNever { // an entry registered nowhere has a disposition, not a case
					cover(t, "E7", "contract-method", c.file+"."+name)
				}
			}
		}
		t.Logf("%d unregistered methods refused at publish", refused)
	})

	t.Run("LAD-3_bridge_and_liquidity_getters_answer", func(t *testing.T) {
		var networks, wraps, unwraps, stakes, reward obj
		get(t, &networks, "embedded.bridge.getAllNetworks", 0, 10)
		get(t, &wraps, "embedded.bridge.getAllWrapTokenRequests", 0, 10)
		get(t, &unwraps, "embedded.bridge.getAllUnwrapTokenRequests", 0, 10)
		get(t, &stakes, "embedded.liquidity.getLiquidityStakeEntriesByAddress", a.Address, 0, 10)
		get(t, &reward, "embedded.liquidity.getUncollectedReward", a.Address)
		// A fresh account has no liquidity stake and no reward; the lists are lists.
		if count(t, stakes, "count") != 0 || str(reward["znnAmount"]) != "0" || str(reward["qsrAmount"]) != "0" || str(reward["address"]) != a.Address.String() {
			t.Errorf("liquidity state of a fresh account: stakes %v, reward %v", stakes, reward)
		}
		for name, r := range map[string]obj{"networks": networks, "wrap requests": wraps, "unwrap requests": unwraps} {
			if _, ok := r["list"].([]interface{}); !ok || count(t, r, "count") < 0 {
				t.Errorf("bridge %s: not a list reply: %v", name, r)
			}
		}
	})

	t.Run("SW-1_swap_retrieve_with_a_bad_key_refused", func(t *testing.T) {
		cover(t, "E6", "contract-method", "swap.RetrieveAssets")
		cover(t, "E6", "error", "vm/constants.ErrInvalidB64Decode")
		data := definition.ABISwap.PackMethodPanic(definition.RetrieveAssetsMethodName, "not base64!", "not base64!")
		callReject(t, constants.ErrInvalidB64Decode.Error(), a, types.SwapContract, data, znn, units(0))
		var v interface{}
		get(t, &v, "embedded.swap.getAssets")
	})
}
