//go:build testnet

package ptlc_test

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/tyler-smith/go-bip39"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	znncrypto "github.com/zenon-network/go-zenon/common/crypto"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	embeddedapi "github.com/zenon-network/go-zenon/rpc/api/embedded"
	rpcclient "github.com/zenon-network/go-zenon/rpc/server"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
)

const (
	defaultRPCURL = "http://localhost:35997"
	devMnemonic   = "abstract affair idle position alien fluid board ordinary exist afraid chapter wood wood guide sun walnut crew perfect place firm poverty model side million"

	zexp = int64(100000000)

	readyMomentumHeight = uint64(7)
	minConfirmations    = uint64(1)

	blockWaitTimeout           = 2 * time.Minute
	contractReceiveWaitTimeout = 3 * time.Minute

	expirationLeadSeconds = int64(240)

	contractSuccess = uint64(1)
	contractFail    = uint64(2)
)

type ptlcSwapLeg struct {
	Name           string
	Locker         types.Address
	Destination    types.Address
	TokenStandard  types.ZenonTokenStandard
	Amount         *big.Int
	ExpirationTime int64
	PointType      uint8
	PointLock      []byte
}

type ptlcSwapTerms struct {
	TradeID    string
	AliceToBob ptlcSwapLeg
	BobToAlice ptlcSwapLeg
}

type harness struct {
	t      *testing.T
	client *rpcclient.Client
	keys   map[uint32]*wallet.KeyPair
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	rpcURL := os.Getenv("PTLC_TESTNET_RPC")
	if rpcURL == "" {
		rpcURL = defaultRPCURL
	}

	var client *rpcclient.Client
	deadline := time.Now().Add(90 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		c, err := rpcclient.DialContext(ctx, rpcURL)
		cancel()
		if err == nil {
			client = c
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial %s: %v", rpcURL, err)
		}
		time.Sleep(1 * time.Second)
	}
	t.Cleanup(client.Close)

	h := &harness{
		t:      t,
		client: client,
		keys:   deriveDevKeys(t),
	}
	h.waitForReady()
	return h
}

func deriveDevKeys(t *testing.T) map[uint32]*wallet.KeyPair {
	t.Helper()

	seed := bip39.NewSeed(devMnemonic, "")
	keys := make(map[uint32]*wallet.KeyPair)
	for _, index := range []uint32{1, 3} {
		key, err := wallet.DeriveWithIndex(index, seed)
		if err != nil {
			t.Fatalf("derive dev key %d: %v", index, err)
		}
		keys[index] = key
	}

	if got := keys[1].Address.String(); got != "z1qq6eg8n43g032hanpsfp02qcdmv7zfj3y2lt5d" {
		t.Fatalf("unexpected index 1 address %s", got)
	}
	if got := keys[3].Address.String(); got != "z1qp3yph55qgresyytz83anynr2f4z39x2z3ej3e" {
		t.Fatalf("unexpected index 3 address %s", got)
	}

	return keys
}

func (h *harness) call(result interface{}, method string, args ...interface{}) error {
	h.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return h.client.CallContext(ctx, result, method, args...)
}

func (h *harness) waitForReady() {
	h.t.Helper()

	h.eventually(5*time.Minute, func() (bool, string) {
		momentum, err := h.frontierMomentum()
		if err != nil {
			return false, err.Error()
		}
		if momentum.ChainIdentifier != 69 {
			h.t.Fatalf("expected devnet chain id 69, got %d", momentum.ChainIdentifier)
		}
		if momentum.Height < readyMomentumHeight {
			return false, fmt.Sprintf("waiting for momentum height >= %d, got %d", readyMomentumHeight, momentum.Height)
		}
		return true, ""
	})
}

func (h *harness) eventually(timeout time.Duration, fn func() (bool, string)) {
	h.t.Helper()

	deadline := time.Now().Add(timeout)
	var last string
	for {
		ok, msg := fn()
		if ok {
			return
		}
		if msg != "" {
			last = msg
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out after %s: %s", timeout, last)
		}
		time.Sleep(1 * time.Second)
	}
}

func (h *harness) frontierMomentum() (*nom.Momentum, error) {
	var momentum nom.Momentum
	if err := h.call(&momentum, "ledger.getFrontierMomentum"); err != nil {
		return nil, err
	}
	return &momentum, nil
}

func (h *harness) accountInfo(address types.Address) *api.AccountInfo {
	h.t.Helper()

	var info api.AccountInfo
	if err := h.call(&info, "ledger.getAccountInfoByAddress", address); err != nil {
		h.t.Fatalf("get account info %s: %v", address, err)
	}
	return &info
}

func (h *harness) balance(address types.Address, token types.ZenonTokenStandard) *big.Int {
	h.t.Helper()

	info := h.accountInfo(address)
	if balance, ok := info.BalanceInfoMap[token]; ok && balance != nil && balance.Balance != nil {
		return new(big.Int).Set(balance.Balance)
	}
	return big.NewInt(0)
}

func (h *harness) frontierAccount(address types.Address) *api.AccountBlock {
	h.t.Helper()

	var block *api.AccountBlock
	if err := h.call(&block, "ledger.getFrontierAccountBlock", address); err != nil {
		h.t.Fatalf("get frontier account block %s: %v", address, err)
	}
	return block
}

func (h *harness) requiredPlasma(address types.Address, blockType uint64, to *types.Address, data []byte) uint64 {
	h.t.Helper()

	var result embeddedapi.GetRequiredResult
	err := h.call(&result, "embedded.plasma.getRequiredPoWForAccountBlock", embeddedapi.GetRequiredParam{
		SelfAddr:  address,
		BlockType: blockType,
		ToAddr:    to,
		Data:      data,
	})
	if err != nil {
		h.t.Fatalf("get required plasma: %v", err)
	}
	if result.RequiredDifficulty != 0 {
		h.t.Fatalf("dev account %s needs PoW difficulty %d; add more fused plasma to devnet genesis", address, result.RequiredDifficulty)
	}
	return result.BasePlasma
}

func (h *harness) publishSend(key *wallet.KeyPair, to types.Address, amount *big.Int, token types.ZenonTokenStandard, data []byte) (types.Hash, error) {
	h.t.Helper()

	block := h.newAccountBlock(key.Address, nom.BlockTypeUserSend)
	block.ToAddress = to
	block.Amount = new(big.Int).Set(amount)
	block.TokenStandard = token
	block.Data = append([]byte(nil), data...)
	block.FusedPlasma = h.requiredPlasma(key.Address, nom.BlockTypeUserSend, &to, data)
	h.sign(block, key)

	err := h.call(nil, "ledger.publishRawTransaction", &api.AccountBlock{AccountBlock: *block})
	return block.Hash, err
}

func (h *harness) mustPublishSend(key *wallet.KeyPair, to types.Address, amount *big.Int, token types.ZenonTokenStandard, data []byte) types.Hash {
	h.t.Helper()

	hash, err := h.publishSend(key, to, amount, token, data)
	if err != nil {
		h.t.Fatalf("publish send %s -> %s: %v", key.Address, to, err)
	}
	return hash
}

func (h *harness) publishReceive(key *wallet.KeyPair, from types.Hash) (types.Hash, error) {
	h.t.Helper()

	block := h.newAccountBlock(key.Address, nom.BlockTypeUserReceive)
	block.FromBlockHash = from
	block.FusedPlasma = h.requiredPlasma(key.Address, nom.BlockTypeUserReceive, nil, nil)
	h.sign(block, key)

	err := h.call(nil, "ledger.publishRawTransaction", &api.AccountBlock{AccountBlock: *block})
	return block.Hash, err
}

func (h *harness) mustPublishReceive(key *wallet.KeyPair, from types.Hash) types.Hash {
	h.t.Helper()

	hash, err := h.publishReceive(key, from)
	if err != nil {
		h.t.Fatalf("publish receive %s from %s: %v", key.Address, from, err)
	}
	return hash
}

func (h *harness) newAccountBlock(address types.Address, blockType uint64) *nom.AccountBlock {
	h.t.Helper()

	momentum, err := h.frontierMomentum()
	if err != nil {
		h.t.Fatalf("get frontier momentum: %v", err)
	}

	block := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      momentum.ChainIdentifier,
		BlockType:            blockType,
		MomentumAcknowledged: momentum.Identifier(),
		Address:              address,
		Amount:               big.NewInt(0),
		TokenStandard:        types.ZeroTokenStandard,
		DescendantBlocks:     make([]*nom.AccountBlock, 0),
	}

	if frontier := h.frontierAccount(address); frontier != nil {
		block.PreviousHash = frontier.Hash
		block.Height = frontier.Height + 1
	} else {
		block.Height = 1
	}

	return block
}

func (h *harness) sign(block *nom.AccountBlock, key *wallet.KeyPair) {
	h.t.Helper()

	block.Hash = block.ComputeHash()
	block.PublicKey = key.Public
	block.Signature = key.Sign(block.Hash.Bytes())
}

func (h *harness) waitBlock(hash types.Hash) *api.AccountBlock {
	h.t.Helper()

	var block *api.AccountBlock
	h.eventually(blockWaitTimeout, func() (bool, string) {
		err := h.call(&block, "ledger.getAccountBlockByHash", hash)
		if err != nil {
			return false, err.Error()
		}
		if block == nil {
			return false, fmt.Sprintf("block %s not cemented yet", hash)
		}
		if block.ConfirmationDetail == nil || block.ConfirmationDetail.NumConfirmations < minConfirmations {
			return false, fmt.Sprintf("block %s has fewer than %d confirmations", hash, minConfirmations)
		}
		return true, ""
	})
	return block
}

func (h *harness) waitContractStatus(sendHash types.Hash) uint64 {
	h.t.Helper()

	var sendBlock *api.AccountBlock
	h.eventually(contractReceiveWaitTimeout, func() (bool, string) {
		sendBlock = h.waitBlock(sendHash)
		if sendBlock.PairedAccountBlock == nil {
			return false, fmt.Sprintf("contract receive for %s not generated yet", sendHash)
		}
		return true, ""
	})

	paired := sendBlock.PairedAccountBlock
	if paired.BlockType != nom.BlockTypeContractReceive {
		h.t.Fatalf("paired block for %s has block type %d, want contract receive", sendHash, paired.BlockType)
	}
	if len(paired.Data) != 8 {
		h.t.Fatalf("contract receive %s has status data length %d", paired.Hash, len(paired.Data))
	}
	return common.BytesToUint64(paired.Data)
}

func (h *harness) waitPtlc(id types.Hash) *definition.PtlcInfo {
	h.t.Helper()

	var info *definition.PtlcInfo
	h.eventually(60*time.Second, func() (bool, string) {
		err := h.call(&info, "embedded.ptlc.getById", id)
		if err != nil {
			return false, err.Error()
		}
		if info == nil {
			return false, "PTLC info is nil"
		}
		return true, ""
	})
	return info
}

func (h *harness) waitPtlcDeleted(id types.Hash) {
	h.t.Helper()

	h.eventually(60*time.Second, func() (bool, string) {
		var info *definition.PtlcInfo
		err := h.call(&info, "embedded.ptlc.getById", id)
		if err != nil {
			return true, ""
		}
		return false, fmt.Sprintf("PTLC %s still exists", id)
	})
}

func (h *harness) receiveAll(key *wallet.KeyPair) {
	h.t.Helper()

	for {
		var list api.AccountBlockList
		if err := h.call(&list, "ledger.getUnreceivedBlocksByAddress", key.Address, uint32(0), uint32(10)); err != nil {
			h.t.Fatalf("get unreceived blocks for %s: %v", key.Address, err)
		}
		if len(list.List) == 0 {
			return
		}
		for _, send := range list.List {
			receiveHash := h.mustPublishReceive(key, send.Hash)
			h.waitBlock(receiveHash)
		}
	}
}

func (h *harness) currentTimestamp() int64 {
	h.t.Helper()

	momentum, err := h.frontierMomentum()
	if err != nil {
		h.t.Fatalf("get frontier momentum: %v", err)
	}
	return int64(momentum.TimestampUnix)
}

func (h *harness) waitTimestampAtLeast(target int64) {
	h.t.Helper()

	h.eventually(5*time.Minute, func() (bool, string) {
		now := h.currentTimestamp()
		if now >= target {
			return true, ""
		}
		return false, fmt.Sprintf("frontier timestamp %d < %d", now, target)
	})
}

func oneZNN(multiplier int64) *big.Int {
	return big.NewInt(multiplier * zexp)
}

func oneQSR(multiplier int64) *big.Int {
	return big.NewInt(multiplier * zexp)
}

func (h *harness) createPtlc(locker *wallet.KeyPair, leg ptlcSwapLeg) types.Hash {
	h.t.Helper()

	createData := definition.ABIPtlc.PackMethodPanic(
		definition.CreatePtlcMethodName,
		leg.ExpirationTime,
		leg.PointType,
		leg.PointLock,
	)
	ptlcID := h.mustPublishSend(locker, types.PtlcContract, leg.Amount, leg.TokenStandard, createData)
	if status := h.waitContractStatus(ptlcID); status != contractSuccess {
		h.t.Fatalf("%s create status = %d, want success", leg.Name, status)
	}
	return ptlcID
}

func (h *harness) assertPtlcMatches(id types.Hash, leg ptlcSwapLeg) {
	h.t.Helper()

	info := h.waitPtlc(id)
	if info.TimeLocked != leg.Locker {
		h.t.Fatalf("%s locker = %s, want %s", leg.Name, info.TimeLocked, leg.Locker)
	}
	if info.TokenStandard != leg.TokenStandard {
		h.t.Fatalf("%s token = %s, want %s", leg.Name, info.TokenStandard, leg.TokenStandard)
	}
	if info.Amount.Cmp(leg.Amount) != 0 {
		h.t.Fatalf("%s amount = %s, want %s", leg.Name, info.Amount, leg.Amount)
	}
	if info.ExpirationTime != leg.ExpirationTime {
		h.t.Fatalf("%s expiration = %d, want %d", leg.Name, info.ExpirationTime, leg.ExpirationTime)
	}
	if info.PointType != leg.PointType {
		h.t.Fatalf("%s point type = %d, want %d", leg.Name, info.PointType, leg.PointType)
	}
	if !bytes.Equal(info.PointLock, leg.PointLock) {
		h.t.Fatalf("%s point lock mismatch", leg.Name)
	}
}

func (h *harness) chainIdentifier() uint64 {
	h.t.Helper()

	momentum, err := h.frontierMomentum()
	if err != nil {
		h.t.Fatalf("frontier momentum: %v", err)
	}
	return momentum.ChainIdentifier
}

func (h *harness) signBIP340Unlock(privateKey *btcec.PrivateKey, id types.Hash, destination types.Address) []byte {
	h.t.Helper()

	message := definition.GetPtlcUnlockMessage(h.chainIdentifier(), definition.PointTypeBIP340, id, destination)
	signature, err := schnorr.Sign(privateKey, message)
	if err != nil {
		h.t.Fatalf("sign BIP340 unlock: %v", err)
	}
	return signature.Serialize()
}

func (h *harness) unlockBIP340Direct(unlocker *wallet.KeyPair, privateKey *btcec.PrivateKey, id types.Hash) types.Hash {
	h.t.Helper()

	signature := h.signBIP340Unlock(privateKey, id, unlocker.Address)
	unlockData := definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, id, signature)
	unlockHash := h.mustPublishSend(unlocker, types.PtlcContract, big.NewInt(0), types.ZeroTokenStandard, unlockData)
	if status := h.waitContractStatus(unlockHash); status != contractSuccess {
		h.t.Fatalf("unlock status = %d, want success", status)
	}
	h.waitPtlcDeleted(id)
	return unlockHash
}

func unpackUnlockSignature(t *testing.T, data []byte) []byte {
	t.Helper()

	param := new(definition.UnlockPtlcParam)
	if err := definition.ABIPtlc.UnpackMethod(param, definition.UnlockPtlcMethodName, data); err != nil {
		t.Fatalf("unpack unlock data: %v", err)
	}
	return append([]byte(nil), param.Signature...)
}

func verifyObservedBIP340Unlock(t *testing.T, pointLock []byte, chainIdentifier uint64, id types.Hash, destination types.Address, signatureBytes []byte) {
	t.Helper()

	publicKey, err := schnorr.ParsePubKey(pointLock)
	if err != nil {
		t.Fatalf("parse BIP340 point lock: %v", err)
	}
	signature, err := schnorr.ParseSignature(signatureBytes)
	if err != nil {
		t.Fatalf("parse BIP340 unlock signature: %v", err)
	}
	message := definition.GetPtlcUnlockMessage(chainIdentifier, definition.PointTypeBIP340, id, destination)
	if !signature.Verify(message, publicKey) {
		t.Fatalf("observed BIP340 unlock signature does not verify for id %s destination %s", id, destination)
	}
}

func TestPtlcCreateValidationViaRPC(t *testing.T) {
	h := newHarness(t)
	locker := h.keys[1]
	recipient := h.keys[3]

	expiration := h.currentTimestamp() + 300
	validCreate := definition.ABIPtlc.PackMethodPanic(
		definition.CreatePtlcMethodName,
		expiration,
		definition.PointTypeED25519,
		recipient.Public,
	)

	if _, err := h.publishSend(locker, types.PtlcContract, big.NewInt(0), types.ZnnTokenStandard, validCreate); err == nil {
		t.Fatalf("zero-amount create unexpectedly succeeded")
	}

	badPointType := definition.ABIPtlc.PackMethodPanic(
		definition.CreatePtlcMethodName,
		expiration,
		uint8(99),
		recipient.Public,
	)
	if _, err := h.publishSend(locker, types.PtlcContract, oneZNN(1), types.ZnnTokenStandard, badPointType); err == nil {
		t.Fatalf("bad point type create unexpectedly succeeded")
	}

	badPointLock := definition.ABIPtlc.PackMethodPanic(
		definition.CreatePtlcMethodName,
		expiration,
		definition.PointTypeED25519,
		recipient.Public[:31],
	)
	if _, err := h.publishSend(locker, types.PtlcContract, oneZNN(1), types.ZnnTokenStandard, badPointLock); err == nil {
		t.Fatalf("bad point lock create unexpectedly succeeded")
	}

	expiredCreate := definition.ABIPtlc.PackMethodPanic(
		definition.CreatePtlcMethodName,
		h.currentTimestamp()-1,
		definition.PointTypeED25519,
		recipient.Public,
	)
	expiredHash := h.mustPublishSend(locker, types.PtlcContract, oneZNN(1), types.ZnnTokenStandard, expiredCreate)
	if status := h.waitContractStatus(expiredHash); status != contractFail {
		t.Fatalf("expired create status = %d, want fail", status)
	}
}

func TestPtlcED25519DomainSeparatedUnlockViaRPC(t *testing.T) {
	h := newHarness(t)
	locker := h.keys[1]
	recipient := h.keys[3]
	h.receiveAll(locker)
	h.receiveAll(recipient)

	amount := oneZNN(2)
	initialRecipient := h.balance(recipient.Address, types.ZnnTokenStandard)
	createData := definition.ABIPtlc.PackMethodPanic(
		definition.CreatePtlcMethodName,
		h.currentTimestamp()+600,
		definition.PointTypeED25519,
		recipient.Public,
	)
	ptlcID := h.mustPublishSend(locker, types.PtlcContract, amount, types.ZnnTokenStandard, createData)
	if status := h.waitContractStatus(ptlcID); status != contractSuccess {
		t.Fatalf("create status = %d, want success", status)
	}
	info := h.waitPtlc(ptlcID)
	if info.TimeLocked != locker.Address || info.TokenStandard != types.ZnnTokenStandard || info.Amount.Cmp(amount) != 0 {
		t.Fatalf("unexpected PTLC info: %+v", info)
	}

	oldMessage := cryptoHashForLegacyUnlock(ptlcID, recipient.Address)
	oldSignature := recipient.Sign(oldMessage)
	oldUnlock := definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, ptlcID, oldSignature)
	oldUnlockHash := h.mustPublishSend(recipient, types.PtlcContract, big.NewInt(0), types.ZeroTokenStandard, oldUnlock)
	if status := h.waitContractStatus(oldUnlockHash); status != contractFail {
		t.Fatalf("legacy unlock status = %d, want fail", status)
	}
	h.waitPtlc(ptlcID)

	momentum, err := h.frontierMomentum()
	if err != nil {
		t.Fatalf("frontier momentum: %v", err)
	}
	message := definition.GetPtlcUnlockMessage(momentum.ChainIdentifier, definition.PointTypeED25519, ptlcID, recipient.Address)
	signature := recipient.Sign(message)
	unlockData := definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, ptlcID, signature)
	unlockHash := h.mustPublishSend(recipient, types.PtlcContract, big.NewInt(0), types.ZeroTokenStandard, unlockData)
	if status := h.waitContractStatus(unlockHash); status != contractSuccess {
		t.Fatalf("unlock status = %d, want success", status)
	}
	h.waitPtlcDeleted(ptlcID)
	h.receiveAll(recipient)

	finalRecipient := h.balance(recipient.Address, types.ZnnTokenStandard)
	if finalRecipient.Cmp(new(big.Int).Add(initialRecipient, amount)) != 0 {
		t.Fatalf("recipient balance = %s, want %s", finalRecipient, new(big.Int).Add(initialRecipient, amount))
	}
}

func TestPtlcBIP340ProxyDestinationBindingViaRPC(t *testing.T) {
	h := newHarness(t)
	locker := h.keys[1]
	recipient := h.keys[3]
	h.receiveAll(locker)
	h.receiveAll(recipient)

	privateKey, publicKey := btcec.PrivKeyFromBytes([]byte{
		0x12, 0x2d, 0x43, 0x9a, 0x88, 0x7c, 0xef, 0x5a,
		0x91, 0x75, 0x68, 0x94, 0x9d, 0xf4, 0x28, 0x51,
		0xc4, 0x32, 0xb3, 0x8d, 0xef, 0x72, 0x02, 0xb1,
		0xf8, 0x52, 0x54, 0x31, 0x22, 0x45, 0x99, 0x01,
	})
	pointLock := schnorr.SerializePubKey(publicKey)

	amount := oneZNN(3)
	initialRecipient := h.balance(recipient.Address, types.ZnnTokenStandard)
	createData := definition.ABIPtlc.PackMethodPanic(
		definition.CreatePtlcMethodName,
		h.currentTimestamp()+600,
		definition.PointTypeBIP340,
		pointLock,
	)
	ptlcID := h.mustPublishSend(locker, types.PtlcContract, amount, types.ZnnTokenStandard, createData)
	if status := h.waitContractStatus(ptlcID); status != contractSuccess {
		t.Fatalf("create status = %d, want success", status)
	}
	h.waitPtlc(ptlcID)

	momentum, err := h.frontierMomentum()
	if err != nil {
		t.Fatalf("frontier momentum: %v", err)
	}
	message := definition.GetPtlcUnlockMessage(momentum.ChainIdentifier, definition.PointTypeBIP340, ptlcID, recipient.Address)
	signature, err := schnorr.Sign(privateKey, message)
	if err != nil {
		t.Fatalf("sign BIP340 unlock: %v", err)
	}

	wrongDestinationData := definition.ABIPtlc.PackMethodPanic(
		definition.ProxyUnlockPtlcMethodName,
		ptlcID,
		locker.Address,
		signature.Serialize(),
	)
	wrongDestinationHash := h.mustPublishSend(locker, types.PtlcContract, big.NewInt(0), types.ZeroTokenStandard, wrongDestinationData)
	if status := h.waitContractStatus(wrongDestinationHash); status != contractFail {
		t.Fatalf("wrong-destination proxy unlock status = %d, want fail", status)
	}
	h.waitPtlc(ptlcID)

	proxyData := definition.ABIPtlc.PackMethodPanic(
		definition.ProxyUnlockPtlcMethodName,
		ptlcID,
		recipient.Address,
		signature.Serialize(),
	)
	proxyHash := h.mustPublishSend(locker, types.PtlcContract, big.NewInt(0), types.ZeroTokenStandard, proxyData)
	if status := h.waitContractStatus(proxyHash); status != contractSuccess {
		t.Fatalf("proxy unlock status = %d, want success", status)
	}
	h.waitPtlcDeleted(ptlcID)
	h.receiveAll(recipient)

	finalRecipient := h.balance(recipient.Address, types.ZnnTokenStandard)
	if finalRecipient.Cmp(new(big.Int).Add(initialRecipient, amount)) != 0 {
		t.Fatalf("recipient balance = %s, want %s", finalRecipient, new(big.Int).Add(initialRecipient, amount))
	}
}

func TestPtlcTwoPartySwapChoreographyViaRPC(t *testing.T) {
	h := newHarness(t)
	alice := h.keys[1]
	bob := h.keys[3]
	h.receiveAll(alice)
	h.receiveAll(bob)

	aliceSwapKey, aliceSwapPub := btcec.PrivKeyFromBytes(bytes.Repeat([]byte{0x41}, 32))
	bobSwapKey, bobSwapPub := btcec.PrivKeyFromBytes(bytes.Repeat([]byte{0x42}, 32))
	now := h.currentTimestamp()
	terms := ptlcSwapTerms{
		TradeID: "devnet-znn-qsr-bip340-swap",
		AliceToBob: ptlcSwapLeg{
			Name:           "alice-locks-znn-for-bob",
			Locker:         alice.Address,
			Destination:    bob.Address,
			TokenStandard:  types.ZnnTokenStandard,
			Amount:         oneZNN(4),
			ExpirationTime: now + 900,
			PointType:      definition.PointTypeBIP340,
			PointLock:      schnorr.SerializePubKey(bobSwapPub),
		},
		BobToAlice: ptlcSwapLeg{
			Name:           "bob-locks-qsr-for-alice",
			Locker:         bob.Address,
			Destination:    alice.Address,
			TokenStandard:  types.QsrTokenStandard,
			Amount:         oneQSR(250),
			ExpirationTime: now + 600,
			PointType:      definition.PointTypeBIP340,
			PointLock:      schnorr.SerializePubKey(aliceSwapPub),
		},
	}

	if terms.AliceToBob.ExpirationTime <= terms.BobToAlice.ExpirationTime {
		t.Fatalf("expected Alice's funding leg to have the longer refund window")
	}

	initialAliceZNN := h.balance(alice.Address, types.ZnnTokenStandard)
	initialAliceQSR := h.balance(alice.Address, types.QsrTokenStandard)
	initialBobZNN := h.balance(bob.Address, types.ZnnTokenStandard)
	initialBobQSR := h.balance(bob.Address, types.QsrTokenStandard)
	initialContractZNN := h.balance(types.PtlcContract, types.ZnnTokenStandard)
	initialContractQSR := h.balance(types.PtlcContract, types.QsrTokenStandard)

	t.Logf("%s: Alice and Bob exchange point locks, destinations, amounts, and expirations off-chain", terms.TradeID)
	aliceToBobID := h.createPtlc(alice, terms.AliceToBob)
	h.assertPtlcMatches(aliceToBobID, terms.AliceToBob)

	t.Log("Bob verifies Alice's on-chain ZNN PTLC matches the negotiated terms before funding his side")
	bobToAliceID := h.createPtlc(bob, terms.BobToAlice)
	h.assertPtlcMatches(bobToAliceID, terms.BobToAlice)

	t.Log("Alice verifies Bob's QSR PTLC, then claims it with her BIP340 unlock signature")
	aliceUnlockHash := h.unlockBIP340Direct(alice, aliceSwapKey, bobToAliceID)
	aliceUnlockBlock := h.waitBlock(aliceUnlockHash)
	observedAliceSignature := unpackUnlockSignature(t, aliceUnlockBlock.Data)
	verifyObservedBIP340Unlock(t, terms.BobToAlice.PointLock, h.chainIdentifier(), bobToAliceID, alice.Address, observedAliceSignature)

	t.Log("Bob observes Alice's unlock material on-chain, then claims Alice's ZNN PTLC")
	h.unlockBIP340Direct(bob, bobSwapKey, aliceToBobID)

	h.receiveAll(alice)
	h.receiveAll(bob)

	if got, want := h.balance(alice.Address, types.ZnnTokenStandard), new(big.Int).Sub(initialAliceZNN, terms.AliceToBob.Amount); got.Cmp(want) != 0 {
		t.Fatalf("Alice ZNN balance = %s, want %s", got, want)
	}
	if got, want := h.balance(alice.Address, types.QsrTokenStandard), new(big.Int).Add(initialAliceQSR, terms.BobToAlice.Amount); got.Cmp(want) != 0 {
		t.Fatalf("Alice QSR balance = %s, want %s", got, want)
	}
	if got, want := h.balance(bob.Address, types.ZnnTokenStandard), new(big.Int).Add(initialBobZNN, terms.AliceToBob.Amount); got.Cmp(want) != 0 {
		t.Fatalf("Bob ZNN balance = %s, want %s", got, want)
	}
	if got, want := h.balance(bob.Address, types.QsrTokenStandard), new(big.Int).Sub(initialBobQSR, terms.BobToAlice.Amount); got.Cmp(want) != 0 {
		t.Fatalf("Bob QSR balance = %s, want %s", got, want)
	}
	if got := h.balance(types.PtlcContract, types.ZnnTokenStandard); got.Cmp(initialContractZNN) != 0 {
		t.Fatalf("PTLC contract ZNN balance = %s, want %s", got, initialContractZNN)
	}
	if got := h.balance(types.PtlcContract, types.QsrTokenStandard); got.Cmp(initialContractQSR) != 0 {
		t.Fatalf("PTLC contract QSR balance = %s, want %s", got, initialContractQSR)
	}
}

func TestPtlcTwoPartySwapAbortRefundViaRPC(t *testing.T) {
	h := newHarness(t)
	alice := h.keys[1]
	bob := h.keys[3]
	h.receiveAll(alice)
	h.receiveAll(bob)

	_, bobSwapPub := btcec.PrivKeyFromBytes(bytes.Repeat([]byte{0x52}, 32))
	abortLeg := ptlcSwapLeg{
		Name:           "alice-locks-znn-before-bob-refuses",
		Locker:         alice.Address,
		Destination:    bob.Address,
		TokenStandard:  types.ZnnTokenStandard,
		Amount:         oneZNN(1),
		ExpirationTime: h.currentTimestamp() + expirationLeadSeconds,
		PointType:      definition.PointTypeBIP340,
		PointLock:      schnorr.SerializePubKey(bobSwapPub),
	}

	initialAliceZNN := h.balance(alice.Address, types.ZnnTokenStandard)
	initialBobZNN := h.balance(bob.Address, types.ZnnTokenStandard)
	initialContractZNN := h.balance(types.PtlcContract, types.ZnnTokenStandard)

	t.Log("Alice funds the first leg, but Bob never funds the reciprocal leg")
	ptlcID := h.createPtlc(alice, abortLeg)
	h.assertPtlcMatches(ptlcID, abortLeg)

	h.waitTimestampAtLeast(abortLeg.ExpirationTime)
	reclaim := definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, ptlcID)
	reclaimHash := h.mustPublishSend(alice, types.PtlcContract, big.NewInt(0), types.ZeroTokenStandard, reclaim)
	if status := h.waitContractStatus(reclaimHash); status != contractSuccess {
		t.Fatalf("abort reclaim status = %d, want success", status)
	}
	h.waitPtlcDeleted(ptlcID)
	h.receiveAll(alice)

	if got := h.balance(alice.Address, types.ZnnTokenStandard); got.Cmp(initialAliceZNN) != 0 {
		t.Fatalf("Alice ZNN balance after abort refund = %s, want %s", got, initialAliceZNN)
	}
	if got := h.balance(bob.Address, types.ZnnTokenStandard); got.Cmp(initialBobZNN) != 0 {
		t.Fatalf("Bob ZNN balance after abort refund = %s, want %s", got, initialBobZNN)
	}
	if got := h.balance(types.PtlcContract, types.ZnnTokenStandard); got.Cmp(initialContractZNN) != 0 {
		t.Fatalf("PTLC contract ZNN balance after abort refund = %s, want %s", got, initialContractZNN)
	}
}

func TestPtlcExpirationAndReclaimViaRPC(t *testing.T) {
	h := newHarness(t)
	locker := h.keys[1]
	recipient := h.keys[3]
	h.receiveAll(locker)
	h.receiveAll(recipient)

	amount := oneZNN(1)
	initialLocker := h.balance(locker.Address, types.ZnnTokenStandard)
	expiration := h.currentTimestamp() + expirationLeadSeconds
	createData := definition.ABIPtlc.PackMethodPanic(
		definition.CreatePtlcMethodName,
		expiration,
		definition.PointTypeED25519,
		recipient.Public,
	)
	ptlcID := h.mustPublishSend(locker, types.PtlcContract, amount, types.ZnnTokenStandard, createData)
	if status := h.waitContractStatus(ptlcID); status != contractSuccess {
		t.Fatalf("create status = %d, want success", status)
	}
	h.waitPtlc(ptlcID)

	earlyReclaim := definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, ptlcID)
	earlyReclaimHash := h.mustPublishSend(locker, types.PtlcContract, big.NewInt(0), types.ZeroTokenStandard, earlyReclaim)
	if status := h.waitContractStatus(earlyReclaimHash); status != contractFail {
		t.Fatalf("early reclaim status = %d, want fail", status)
	}
	h.waitPtlc(ptlcID)

	h.waitTimestampAtLeast(expiration)

	momentum, err := h.frontierMomentum()
	if err != nil {
		t.Fatalf("frontier momentum: %v", err)
	}
	message := definition.GetPtlcUnlockMessage(momentum.ChainIdentifier, definition.PointTypeED25519, ptlcID, recipient.Address)
	expiredUnlock := definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, ptlcID, recipient.Sign(message))
	expiredUnlockHash := h.mustPublishSend(recipient, types.PtlcContract, big.NewInt(0), types.ZeroTokenStandard, expiredUnlock)
	if status := h.waitContractStatus(expiredUnlockHash); status != contractFail {
		t.Fatalf("expired unlock status = %d, want fail", status)
	}
	h.waitPtlc(ptlcID)

	reclaim := definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, ptlcID)
	reclaimHash := h.mustPublishSend(locker, types.PtlcContract, big.NewInt(0), types.ZeroTokenStandard, reclaim)
	if status := h.waitContractStatus(reclaimHash); status != contractSuccess {
		t.Fatalf("reclaim status = %d, want success", status)
	}
	h.waitPtlcDeleted(ptlcID)
	h.receiveAll(locker)

	finalLocker := h.balance(locker.Address, types.ZnnTokenStandard)
	if finalLocker.Cmp(initialLocker) != 0 {
		t.Fatalf("locker balance = %s, want %s", finalLocker, initialLocker)
	}
}

func cryptoHashForLegacyUnlock(id types.Hash, destination types.Address) []byte {
	return znncrypto.Hash(common.JoinBytes(id.Bytes(), destination.Bytes()))
}
