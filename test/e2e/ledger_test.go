//go:build e2e

package e2e

import (
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/wallet"
)

// TestLedger covers TEST-PLAN.md section 5.2: sends, receives, plasma and
// the account-block rules a node must enforce at publish.
func TestLedger(t *testing.T) {
	group(t)
	a := newAccount(t, 0, 100, 1000, 1000) // funded, fused plasma
	b := net.Key(runBase + 1)              // empty, no plasma: pays with proof of work

	var sendZnn *nom.AccountBlock
	// The cases build on each other: B-1 funds b and makes the send several
	// later cases read, and B-3a leaves a send to b that B-4 receives. A case
	// run without the steps it builds on (a -run that selects it alone)
	// stops and names them, instead of failing on missing state.
	passed := map[string]bool{}
	step := func(t *testing.T, id string) { t.Cleanup(func() { passed[id] = !t.Failed() }) }
	requires := func(t *testing.T, ids ...string) {
		t.Helper()
		for _, id := range ids {
			if !passed[id] {
				t.Fatalf("this case builds on %s, which did not run or did not pass; run TestLedger as a whole (or select %s with it)", id, strings.Join(ids, " and "))
			}
		}
	}

	t.Run("B-1_send_and_receive_znn_and_qsr", func(t *testing.T) {
		step(t, "B-1")
		cover(t, "E1", "rpc-method", "LedgerApi.PublishRawTransaction", "LedgerApi.GetFrontierAccountBlock",
			"LedgerApi.GetAccountBlockByHash", "LedgerApi.GetUnreceivedBlocksByAddress", "LedgerApi.GetAccountInfoByAddress",
			"LedgerApi.GetFrontierMomentum", "PlasmaApi.GetRequiredPoWForAccountBlock")
		// A published block passes the whole account-block pipeline
		// (getContext, every rule in all(), then the transaction-level
		// verifyTransaction checks) on every node that admits it.
		cover(t, "E1", "verifier-rule", "accountBlockVerifier.all", "accountVerifier.getContext", "accountVerifier.verifyTransaction")
		aZnn, aQsr := bal(t, a.Address, znn), bal(t, a.Address, qsr)

		sendZnn = transfer(t, a, b.Address, znn, coins(1), nil)
		sendQsr := transfer(t, a, b.Address, qsr, coins(2), nil)
		receiveAll(t, b, 2)

		wantBig(t, "receiver ZNN", bal(t, b.Address, znn), coins(1))
		wantBig(t, "receiver QSR", bal(t, b.Address, qsr), coins(2))
		wantBig(t, "sender ZNN", bal(t, a.Address, znn), new(big.Int).Sub(aZnn, coins(1)))
		wantBig(t, "sender QSR", bal(t, a.Address, qsr), new(big.Int).Sub(aQsr, coins(2)))

		var frontier *api.AccountBlock
		get(t, &frontier, "ledger.getFrontierAccountBlock", b.Address)
		if frontier == nil || frontier.Height != 2 {
			t.Errorf("receiver account height: got %+v, want 2", frontier)
		}
		for _, h := range []types.Hash{sendZnn.Hash, sendQsr.Hash, frontier.Hash} {
			if err := net.Agreement(h, wait); err != nil {
				t.Error(err)
			}
		}
	})

	t.Run("B-2a_fused_plasma_only", func(t *testing.T) {
		requires(t, "B-1")
		got, err := net.Block(0, sendZnn.Hash)
		if err != nil || got == nil {
			t.Fatalf("read back: %v", err)
		}
		if got.Difficulty != 0 || got.FusedPlasma == 0 || got.TotalPlasma < got.BasePlasma {
			t.Errorf("fused=%d difficulty=%d base=%d used=%d, want fused only and used >= base", got.FusedPlasma, got.Difficulty, got.BasePlasma, got.TotalPlasma)
		}
	})

	t.Run("B-2b_proof_of_work_only", func(t *testing.T) {
		requires(t, "B-1")
		var got *api.AccountBlock
		get(t, &got, "ledger.getFrontierAccountBlock", b.Address)
		if got.Difficulty == 0 || got.FusedPlasma != 0 || got.TotalPlasma < got.BasePlasma {
			t.Errorf("fused=%d difficulty=%d base=%d used=%d, want proof of work only and used >= base", got.FusedPlasma, got.Difficulty, got.BasePlasma, got.TotalPlasma)
		}
	})

	t.Run("B-2c_no_plasma_refused", func(t *testing.T) {
		cover(t, "E2", "error", "vm/constants.ErrNotEnoughTotalPlasma")
		err := tryTransfer(a, b.Address, znn, coins(1), &Mutator{Plasma: func(b *nom.AccountBlock) { b.FusedPlasma, b.Difficulty = 0, 0 }})
		mustReject(t, err, constants.ErrNotEnoughTotalPlasma.Error())
	})

	t.Run("B-2d_fused_plasma_the_account_does_not_have_refused", func(t *testing.T) {
		requires(t, "B-1")
		cover(t, "E5", "error", "vm/constants.ErrNotEnoughPlasma")
		err := tryTransfer(b, a.Address, znn, units(1), &Mutator{Plasma: func(b *nom.AccountBlock) { b.FusedPlasma, b.Difficulty = constants.AccountBlockBasePlasma, 0 }})
		mustReject(t, err, constants.ErrNotEnoughPlasma.Error())
	})

	t.Run("B-2e_difficulty_without_work_refused", func(t *testing.T) {
		requires(t, "B-1")
		cover(t, "E6", "error", "verifier.ErrABPoWInvalid")
		cover(t, "E6", "verifier-rule", "accountBlockVerifier.pow")
		err := tryTransfer(b, a.Address, znn, units(1), &Mutator{SkipPoW: true,
			Plasma: func(b *nom.AccountBlock) {
				b.FusedPlasma, b.Difficulty = 0, constants.AccountBlockBasePlasma*constants.PoWDifficultyPerPlasma
			}})
		mustReject(t, err, "nonce/difficulty is invalid")
	})

	t.Run("B-3a_data_at_the_limit_accepted", func(t *testing.T) {
		step(t, "B-3a")
		blk := transfer(t, a, b.Address, znn, units(1), make([]byte, constants.MaxDataLength))
		got, err := net.WaitConfirmed(blk.Hash, wait)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Data) != constants.MaxDataLength {
			t.Errorf("data length read back: got %d, want %d", len(got.Data), constants.MaxDataLength)
		}
	})

	t.Run("B-3b_data_one_byte_over_the_limit_refused", func(t *testing.T) {
		cover(t, "E2", "error", "verifier.ErrABDataTooBig")
		_, err := net.Send(a, &nom.AccountBlock{BlockType: nom.BlockTypeUserSend, ToAddress: b.Address, TokenStandard: znn, Amount: units(1),
			Data: make([]byte, constants.MaxDataLength+1)},
			&Mutator{Plasma: func(b *nom.AccountBlock) { b.FusedPlasma = 2_000_000 }})
		mustReject(t, err, "data field is too big")
	})

	t.Run("B-4_receive_by_the_wrong_account_refused", func(t *testing.T) {
		requires(t, "B-1", "B-3a")
		cover(t, "E3", "error", "verifier.ErrABFromBlockReceiverMismatch")
		cover(t, "E3", "verifier-rule", "accountBlockVerifier.fromHash")
		blk := transfer(t, a, b.Address, znn, units(5), nil)
		if _, err := net.WaitConfirmed(blk.Hash, wait); err != nil {
			t.Fatal(err)
		}
		_, err := net.Receive(a, blk.Hash, nil)
		mustReject(t, err, "receiver mismatch")
		receiveAll(t, b, 2) // this send and the one of B-3a, which nobody received yet
	})

	t.Run("B-5a_double_receive_refused", func(t *testing.T) {
		requires(t, "B-1")
		cover(t, "E4", "error", "verifier.ErrABFromBlockAlreadyReceived")
		cover(t, "E4", "verifier-rule", "accountBlockVerifier.fromHash")
		_, err := net.Receive(b, sendZnn.Hash, nil)
		mustReject(t, err, "already received")
	})

	t.Run("B-5b_receive_of_an_unknown_send_refused", func(t *testing.T) {
		cover(t, "E4", "error", "verifier.ErrABFromBlockMissing")
		cover(t, "E4", "verifier-rule", "accountBlockVerifier.fromHash")
		_, err := net.Receive(b, types.NewHash([]byte("no such block "+runTag)), nil)
		mustReject(t, err, "from-block doesn't exist")
	})

	t.Run("B-5c_wrong_previous_hash_refused", func(t *testing.T) {
		err := tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(b *nom.AccountBlock) { b.PreviousHash = types.NewHash([]byte("wrong")) }})
		mustReject(t, err, "previous block is missing")
		cover(t, "E6", "error", "verifier.ErrABPreviousMissing")
		cover(t, "E6", "verifier-rule", "accountBlockVerifier.previous")
	})

	t.Run("B-5d_wrong_height_refused", func(t *testing.T) {
		cover(t, "E2", "error", "verifier.ErrABPrevHasCementedOnTop", "verifier.ErrABPreviousMissing")
		for delta, want := range map[int64]string{-1: "cemented block on top of it", 1: "previous block is missing", 1000: "previous block is missing"} {
			err := tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(b *nom.AccountBlock) { b.Height = uint64(int64(b.Height) + delta) }})
			mustReject(t, err, want)
		}
		err := tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(b *nom.AccountBlock) { b.Height = 0 }})
		mustReject(t, err, "height must be higher than 0")
		cover(t, "E2", "error", "verifier.ErrABMHeightMissing")
		cover(t, "E2", "verifier-rule", "accountBlockVerifier.previous")
	})

	t.Run("B-5e_bad_signature_refused", func(t *testing.T) {
		cover(t, "E6", "error", "verifier.ErrABSignatureInvalid", "verifier.ErrABSignatureMissing", "verifier.ErrABPublicKeyWrongAddress", "verifier.ErrABPublicKeyMissing")
		cover(t, "E6", "verifier-rule", "accountBlockTransactionVerifier.signature", "accountBlockTransactionVerifier.producer")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Signed: func(b *nom.AccountBlock) { b.Signature[0] ^= 0xff }}), "signature is invalid")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Signed: func(b *nom.AccountBlock) { b.Signature = nil }}), "signature is missing")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Signed: func(blk *nom.AccountBlock) {
			blk.PublicKey = b.Public
			blk.Signature = b.Sign(blk.Hash.Bytes())
		}}), "publicKey doesn't correspond to the address")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Signed: func(b *nom.AccountBlock) { b.PublicKey = nil }}), "publicKey is missing")
	})

	t.Run("B-5f_wrong_chain_identifier_refused", func(t *testing.T) {
		// A wrong identifier is stopped by the RPC layer. A zero identifier
		// passes it and is stopped by the account-block verifier, which
		// answers with the error value declared for momentums
		// (verifier.ErrMChainIdentifierMissing, "momentum chain-identifier is
		// missing"); the account-block value ErrABChainIdentifierMissing is
		// never returned. The case pins what the node does.
		cover(t, "E6", "error", "verifier.ErrMChainIdentifierMissing")
		cover(t, "E6", "verifier-rule", "accountBlockVerifier.chainIdentifier")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(b *nom.AccountBlock) { b.ChainIdentifier = net.ChainID + 1 }}), "the block has a different network Id")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(b *nom.AccountBlock) { b.ChainIdentifier = 0 }}), "momentum chain-identifier is missing")
	})

	t.Run("B-5g_amount_above_balance_refused", func(t *testing.T) {
		cover(t, "E5", "error", "vm/constants.ErrInsufficientBalance")
		over := new(big.Int).Add(bal(t, a.Address, znn), units(1))
		mustReject(t, tryTransfer(a, b.Address, znn, over, &Mutator{}), constants.ErrInsufficientBalance.Error())
	})

	t.Run("B-5h_unknown_token_refused", func(t *testing.T) {
		zts := types.NewZenonTokenStandard([]byte("no such token " + runTag))
		mustReject(t, tryTransfer(a, b.Address, zts, units(1), &Mutator{}), constants.ErrDataNonExistent.Error())
	})

	t.Run("B-5i_hash_not_matching_the_fields_refused", func(t *testing.T) {
		cover(t, "E6", "error", "verifier.ErrABHashInvalid", "verifier.ErrABHashMissing")
		cover(t, "E6", "verifier-rule", "accountBlockTransactionVerifier.hash")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Signed: func(b *nom.AccountBlock) { b.Amount = units(2) }}), "hash is different than the one computed")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Signed: func(b *nom.AccountBlock) { b.Hash = types.ZeroHash }}), "hash must not be zero")
	})

	t.Run("B-5j_version_and_type_refused", func(t *testing.T) {
		cover(t, "E6", "error", "verifier.ErrABVersionMissing", "verifier.ErrABVersionInvalid", "verifier.ErrABTypeMissing", "verifier.ErrABTypeUnsupported")
		cover(t, "E6", "verifier-rule", "accountBlockVerifier.version", "accountBlockVerifier.blockType")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(b *nom.AccountBlock) { b.Version = 0 }}), "version is missing")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(b *nom.AccountBlock) { b.Version = 2 }}), "version is invalid")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Plasma: func(b *nom.AccountBlock) { b.BlockType = 0; b.FusedPlasma = constants.AccountBlockBasePlasma }}), "type is missing")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Plasma: func(b *nom.AccountBlock) { b.BlockType = 9; b.FusedPlasma = constants.AccountBlockBasePlasma }}), "type is not supported")
	})

	t.Run("B-5k_contract_block_types_from_a_user_refused", func(t *testing.T) {
		cover(t, "E3", "error", "verifier.ErrABTypeMustBeUser", "verifier.ErrABTypeMustNotBeGenesis")
		cover(t, "E3", "verifier-rule", "accountBlockVerifier.blockType")
		for typ, want := range map[uint64]string{nom.BlockTypeContractSend: "can't apply BlockTypeContractSend", nom.BlockTypeContractReceive: "not suitable for user-blocks"} {
			err := tryTransfer(a, b.Address, znn, units(1), &Mutator{Plasma: func(b *nom.AccountBlock) { b.BlockType = typ; b.FusedPlasma = constants.AccountBlockBasePlasma }})
			mustReject(t, err, want)
		}
		err := tryTransfer(a, b.Address, znn, units(1), &Mutator{Plasma: func(b *nom.AccountBlock) {
			b.BlockType = nom.BlockTypeGenesisReceive
			b.FusedPlasma = constants.AccountBlockBasePlasma
		}})
		mustReject(t, err, "must not be genesis")
	})

	t.Run("B-5l_receive_block_with_send_fields_refused", func(t *testing.T) {
		cover(t, "E6", "error", "verifier.ErrABAmountMustBeZero", "verifier.ErrABToAddressMustBeZero")
		cover(t, "E6", "verifier-rule", "accountBlockVerifier.amounts")
		blk := transfer(t, a, b.Address, znn, units(7), nil)
		if _, err := net.WaitConfirmed(blk.Hash, wait); err != nil {
			t.Fatal(err)
		}
		_, err := net.Receive(b, blk.Hash, &Mutator{Fields: func(b *nom.AccountBlock) { b.Amount = units(1); b.TokenStandard = znn }})
		mustReject(t, err, "amount must be zero")
		_, err = net.Receive(b, blk.Hash, &Mutator{Fields: func(blk *nom.AccountBlock) { blk.ToAddress = a.Address }})
		mustReject(t, err, "to-address must be zero")
		receiveAll(t, b, 1)
	})

	t.Run("B-5m_momentum_acknowledged_refused", func(t *testing.T) {
		cover(t, "E6", "error", "verifier.ErrABMAMustNotBeZero", "verifier.ErrABMAMissing")
		cover(t, "E6", "verifier-rule", "accountBlockVerifier.momentumAcknowledged")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(b *nom.AccountBlock) { b.MomentumAcknowledged = types.HashHeight{} }}), "momentum-acknowledged missing")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(b *nom.AccountBlock) { b.MomentumAcknowledged.Hash = types.NewHash([]byte("x")) }}), "momentum-acknowledged points to missing momentum")
	})

	t.Run("B-5n_embedded_address_refused", func(t *testing.T) {
		cover(t, "E3", "error", "rpc/api.ErrEmbeddedAddressNotAllowed")
		err := tryTransfer(a, b.Address, znn, units(1), &Mutator{Plasma: func(b *nom.AccountBlock) {
			b.Address = types.StakeContract
			b.FusedPlasma = constants.AccountBlockBasePlasma
			b.Difficulty = 0
		}})
		mustReject(t, err, api.ErrEmbeddedAddressNotAllowed.Error())
	})

	t.Run("B-9_refused_blocks_left_no_trace", func(t *testing.T) {
		// Both accounts exist only in this run, so the chain must show exactly
		// the blocks the node accepted from the driver and none it refused.
		refused := net.Refused()
		if len(refused) < 30 {
			t.Fatalf("only %d blocks were refused in this run; the group did not run as a whole", len(refused))
		}
		for i := range net.URLs {
			for _, h := range refused {
				if blk, err := net.Block(i, h); err != nil || blk != nil {
					t.Errorf("%s knows refused block %s (error: %v)", net.URLs[i], h, err)
				}
			}
			for _, kp := range []*wallet.KeyPair{a, b} {
				var frontier *api.AccountBlock
				if err := net.CallOn(i, &frontier, "ledger.getFrontierAccountBlock", kp.Address); err != nil || frontier == nil {
					t.Fatalf("%s: frontier of %s: %v", net.URLs[i], kp.Address, err)
				}
				if int(frontier.Height) != net.Accepted(kp.Address) {
					t.Errorf("%s: account %s is at height %d, the node accepted %d blocks from it", net.URLs[i], kp.Address, frontier.Height, net.Accepted(kp.Address))
				}
				var list *api.AccountBlockList
				if err := net.CallOn(i, &list, "ledger.getUnconfirmedBlocksByAddress", kp.Address, 0, 10); err != nil || list == nil || list.Count != 0 {
					t.Errorf("%s: unconfirmed blocks of %s: %+v (error: %v)", net.URLs[i], kp.Address, list, err)
				}
			}
		}
		cover(t, "E1", "rpc-method", "LedgerApi.GetUnconfirmedBlocksByAddress")
		// No container has a refused block either, also those without an RPC port.
		for _, h := range refused {
			if have, err := net.KnownOnContainers(h); err != nil || len(have) != 0 {
				t.Errorf("refused block %s is known on %v (error: %v)", h, have, err)
			}
		}
		// Each account's chain is, block for block, what the driver signed
		// and the node accepted, and each balance is what those blocks add up
		// to: computed here from the driver's own blocks, per account and
		// per token, so that value moved between the two accounts shows.
		for _, kp := range []*wallet.KeyPair{a, b} {
			signed := net.Signed(kp.Address)
			want := map[types.ZenonTokenStandard]*big.Int{znn: new(big.Int), qsr: new(big.Int)}
			for _, blk := range signed {
				switch blk.BlockType {
				case nom.BlockTypeUserSend:
					if want[blk.TokenStandard] != nil && blk.Amount != nil {
						want[blk.TokenStandard].Sub(want[blk.TokenStandard], blk.Amount)
					}
				case nom.BlockTypeUserReceive:
					from := net.SignedBlock(blk.FromBlockHash)
					if from == nil {
						t.Fatalf("%s received %s, which is not a block this driver sent", kp.Address, blk.FromBlockHash)
					}
					if want[from.TokenStandard] != nil && from.Amount != nil {
						want[from.TokenStandard].Add(want[from.TokenStandard], from.Amount)
					}
				default:
					t.Fatalf("account %s has a signed block of type %d", kp.Address, blk.BlockType)
				}
			}
			for i, url := range net.URLs {
				var chain *api.AccountBlockList
				if err := net.CallOn(i, &chain, "ledger.getAccountBlocksByHeight", kp.Address, 1, len(signed)+5); err != nil || chain == nil {
					t.Fatalf("%s: chain of %s: %v", url, kp.Address, err)
				}
				if len(chain.List) != len(signed) {
					t.Fatalf("%s: account %s has %d blocks, the driver had %d accepted", url, kp.Address, len(chain.List), len(signed))
				}
				for j, blk := range chain.List {
					if blk.Hash != signed[j].Hash || blk.Height != uint64(j+1) {
						t.Fatalf("%s: block %d of %s is %s at height %d, the driver signed %s", url, j+1, kp.Address, blk.Hash, blk.Height, signed[j].Hash)
					}
				}
				var info *api.AccountInfo
				if err := net.CallOn(i, &info, "ledger.getAccountInfoByAddress", kp.Address); err != nil || info == nil || info.Address != kp.Address {
					t.Fatalf("%s: account info of %s: %v", url, kp.Address, err)
				}
				for zts, w := range want {
					got := big.NewInt(0)
					if info.BalanceInfoMap[zts] != nil {
						got = info.BalanceInfoMap[zts].Balance
					}
					wantBig(t, fmt.Sprintf("%s: %s of %s, from the driver's own blocks", url, zts, kp.Address), got, w)
				}
			}
		}
		cover(t, "E1", "rpc-method", "LedgerApi.GetAccountBlocksByHeight")
		// And both chains are still usable.
		blk := transfer(t, a, b.Address, znn, units(3), nil)
		receiveAll(t, b, 1)
		if err := net.Agreement(blk.Hash, wait); err != nil {
			t.Error(err)
		}
	})
}
