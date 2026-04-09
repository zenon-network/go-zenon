package definition

import (
	"math/big"
	"strings"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/vm/abi"
	"github.com/zenon-network/go-zenon/vm/constants"
)

const (
	jsonBitcoinSpv = `
	[
		{"type":"function","name":"SubmitHeaders","inputs":[
			{"name":"headers","type":"bytes"}
		]},
		{"type":"function","name":"VerifyTransaction","inputs":[
			{"name":"txHash","type":"bytes"},
			{"name":"blockHeight","type":"uint64"},
			{"name":"merkleProof","type":"bytes"},
			{"name":"txIndex","type":"uint32"}
		]},

		{"type":"variable","name":"blockHeader","inputs":[
			{"name":"headerHash","type":"bytes"},
			{"name":"prevHash","type":"bytes"},
			{"name":"merkleRoot","type":"bytes"},
			{"name":"timestamp","type":"uint32"},
			{"name":"bits","type":"uint32"},
			{"name":"nonce","type":"uint32"},
			{"name":"chainWork","type":"uint256"}
		]},
		{"type":"variable","name":"spvState","inputs":[
			{"name":"bestHeight","type":"uint64"},
			{"name":"bestHash","type":"bytes"},
			{"name":"bestChainWork","type":"uint256"},
			{"name":"headerCount","type":"uint64"}
		]},
		{"type":"variable","name":"spvVerification","inputs":[
			{"name":"blockHeight","type":"uint64"},
			{"name":"txIndex","type":"uint32"},
			{"name":"verified","type":"bool"}
		]}
	]`

	SubmitHeadersMethodName     = "SubmitHeaders"
	VerifyTransactionMethodName = "VerifyTransaction"

	variableNameBlockHeader     = "blockHeader"
	variableNameSpvState        = "spvState"
	variableNameSpvVerification = "spvVerification"
)

var (
	ABIBitcoinSpv = abi.JSONToABIContract(strings.NewReader(jsonBitcoinSpv))

	// Key prefix []byte{1} + height (8 bytes big-endian) -> blockHeader data
	blockHeaderKeyPrefix = []byte{1}
	// Key prefix []byte{2} + hash (32 bytes) -> height (for hash-to-height lookup)
	hashToHeightKeyPrefix = []byte{2}
	// Key prefix []byte{3} -> spvState (singleton)
	spvStateKeyPrefix = []byte{3}
	// Key prefix []byte{4} + txHash (32 bytes) -> spvVerification
	spvVerificationKeyPrefix = []byte{4}
)

// SubmitHeadersParam is the parameter for the SubmitHeaders method.
type SubmitHeadersParam struct {
	Headers []byte `json:"headers"`
}

// VerifyTransactionParam is the parameter for the VerifyTransaction method.
type VerifyTransactionParam struct {
	TxHash      []byte `json:"txHash"`
	BlockHeight uint64 `json:"blockHeight"`
	MerkleProof []byte `json:"merkleProof"`
	TxIndex     uint32 `json:"txIndex"`
}

// BlockHeaderInfo stores a validated Bitcoin block header.
type BlockHeaderInfo struct {
	Height     uint64   `json:"height"`
	HeaderHash []byte   `json:"headerHash"`
	PrevHash   []byte   `json:"prevHash"`
	MerkleRoot []byte   `json:"merkleRoot"`
	Timestamp  uint32   `json:"timestamp"`
	Bits       uint32   `json:"bits"`
	Nonce      uint32   `json:"nonce"`
	ChainWork  *big.Int `json:"chainWork"`
}

// SpvState stores the best chain tip information.
type SpvState struct {
	BestHeight    uint64   `json:"bestHeight"`
	BestHash      []byte   `json:"bestHash"`
	BestChainWork *big.Int `json:"bestChainWork"`
	HeaderCount   uint64   `json:"headerCount"`
}

// SpvVerification stores a verified transaction proof.
type SpvVerification struct {
	TxHash      []byte `json:"txHash"`
	BlockHeight uint64 `json:"blockHeight"`
	TxIndex     uint32 `json:"txIndex"`
	Verified    bool   `json:"verified"`
}

// --- BlockHeader storage ---

func getBlockHeaderKey(height uint64) []byte {
	return common.JoinBytes(blockHeaderKeyPrefix, common.Uint64ToBytes(height))
}

func getHashToHeightKey(hash []byte) []byte {
	return common.JoinBytes(hashToHeightKeyPrefix, hash)
}

func getSpvStateKey() []byte {
	return spvStateKeyPrefix
}

func getSpvVerificationKey(txHash []byte) []byte {
	return common.JoinBytes(spvVerificationKeyPrefix, txHash)
}

func (h *BlockHeaderInfo) Save(context db.DB) error {
	data, err := ABIBitcoinSpv.PackVariable(
		variableNameBlockHeader,
		h.HeaderHash,
		h.PrevHash,
		h.MerkleRoot,
		h.Timestamp,
		h.Bits,
		h.Nonce,
		h.ChainWork,
	)
	if err != nil {
		return err
	}
	// Store header by height
	if err := context.Put(getBlockHeaderKey(h.Height), data); err != nil {
		return err
	}
	// Store hash -> height mapping
	return context.Put(getHashToHeightKey(h.HeaderHash), common.Uint64ToBytes(h.Height))
}

func GetBlockHeaderByHeight(context db.DB, height uint64) (*BlockHeaderInfo, error) {
	key := getBlockHeaderKey(height)
	data, err := context.Get(key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, constants.ErrHeaderNotFound
	}
	return parseBlockHeaderInfo(height, data)
}

func GetHeightByHash(context db.DB, hash []byte) (uint64, error) {
	key := getHashToHeightKey(hash)
	data, err := context.Get(key)
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, constants.ErrHeaderNotFound
	}
	return common.BytesToUint64(data), nil
}

func parseBlockHeaderInfo(height uint64, data []byte) (*BlockHeaderInfo, error) {
	if len(data) == 0 {
		return nil, constants.ErrDataNonExistent
	}
	info := new(BlockHeaderInfo)
	if err := ABIBitcoinSpv.UnpackVariable(info, variableNameBlockHeader, data); err != nil {
		return nil, err
	}
	info.Height = height
	return info, nil
}

// --- SpvState storage ---

func (s *SpvState) Save(context db.DB) error {
	data, err := ABIBitcoinSpv.PackVariable(
		variableNameSpvState,
		s.BestHeight,
		s.BestHash,
		s.BestChainWork,
		s.HeaderCount,
	)
	if err != nil {
		return err
	}
	return context.Put(getSpvStateKey(), data)
}

func GetSpvState(context db.DB) (*SpvState, error) {
	key := getSpvStateKey()
	data, err := context.Get(key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		// No state yet — return empty initial state
		return &SpvState{
			BestHeight:    0,
			BestHash:      make([]byte, 32),
			BestChainWork: big.NewInt(0),
			HeaderCount:   0,
		}, nil
	}
	state := new(SpvState)
	if err := ABIBitcoinSpv.UnpackVariable(state, variableNameSpvState, data); err != nil {
		return nil, err
	}
	return state, nil
}

// --- SpvVerification storage ---

func (v *SpvVerification) Save(context db.DB) error {
	data, err := ABIBitcoinSpv.PackVariable(
		variableNameSpvVerification,
		v.BlockHeight,
		v.TxIndex,
		v.Verified,
	)
	if err != nil {
		return err
	}
	return context.Put(getSpvVerificationKey(v.TxHash), data)
}

func GetSpvVerification(context db.DB, txHash []byte) (*SpvVerification, error) {
	key := getSpvVerificationKey(txHash)
	data, err := context.Get(key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, constants.ErrDataNonExistent
	}
	info := new(SpvVerification)
	if err := ABIBitcoinSpv.UnpackVariable(info, variableNameSpvVerification, data); err != nil {
		return nil, err
	}
	info.TxHash = txHash
	return info, nil
}
