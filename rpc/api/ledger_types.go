package api

import (
	"encoding/json"
	"math/big"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// DetailedMomentum pairs a momentum with the full account blocks it
// confirms, as returned by GetDetailedMomentumsByHeight. The JSON keys
// are "momentum" and "blocks".
type DetailedMomentum struct {
	AccountBlocks []*AccountBlock `json:"blocks"`
	Momentum      *Momentum       `json:"momentum"`
}

// Momentum is the wire form of a ledger momentum: the embedded
// nom.Momentum extended with the producer address, which is derived
// from the momentum's producer public key so clients do not have to
// recover it themselves.
type Momentum struct {
	*nom.Momentum
	// Producer is the pillar producer address.
	Producer types.Address `json:"producer"`
}

// MomentumHeader is a compact reference to a momentum: its hash, its
// momentum-chain height and its timestamp in Unix seconds.
type MomentumHeader struct {
	Hash      types.Hash `json:"hash"`
	Height    uint64     `json:"height"`
	Timestamp int64      `json:"timestamp"`
}

// AccountBlockConfirmationDetail describes the momentum that confirmed
// an account block.
type AccountBlockConfirmationDetail struct {
	// NumConfirmations counts momentums from the confirming momentum to
	// the frontier, inclusive; 1 means the block was confirmed by the
	// current frontier momentum.
	NumConfirmations uint64 `json:"numConfirmations"`
	// MomentumHeight, MomentumHash and MomentumTimestamp identify the
	// momentum that confirmed the block; the timestamp is Unix seconds.
	MomentumHeight    uint64     `json:"momentumHeight"`
	MomentumHash      types.Hash `json:"momentumHash"`
	MomentumTimestamp int64      `json:"momentumTimestamp"`
}

// AccountBlock is the wire form of a ledger account block: the embedded
// nom.AccountBlock enriched with data prefetched by the ledger API.
// Custom JSON marshalling renders *big.Int amounts as base-10 strings
// (see AccountBlockMarshal).
type AccountBlock struct {
	nom.AccountBlock

	// TokenInfo describes the token referenced by the block's
	// TokenStandard; it is nil when the block moves no tokens.
	TokenInfo *Token `json:"token"`
	// ConfirmationDetail is nil while the block is not yet confirmed by
	// a momentum.
	ConfirmationDetail *AccountBlockConfirmationDetail `json:"confirmationDetail"`
	// PairedAccountBlock is the counterpart of this block: for a send
	// block, the receive block that received it (nil while unreceived);
	// for a receive block, the originating send block. For a
	// BlockTypeGenesisReceive block, which has no real send counterpart,
	// a synthetic empty ContractSend block is fabricated, with
	// confirmation detail anchored at the genesis momentum.
	PairedAccountBlock *AccountBlock `json:"pairedAccountBlock"`
}

// AccountBlockMarshal is the JSON wire representation of AccountBlock,
// with *big.Int fields rendered as base-10 strings. It exists so the
// custom MarshalJSON/UnmarshalJSON of AccountBlock can round-trip
// amounts without precision loss.
type AccountBlockMarshal struct {
	nom.AccountBlockMarshal
	TokenInfo          *TokenMarshal                   `json:"token"`
	ConfirmationDetail *AccountBlockConfirmationDetail `json:"confirmationDetail"`
	PairedAccountBlock *AccountBlockMarshal            `json:"pairedAccountBlock"`
}

// ToAccountBlockMarshal converts the block (including its token info
// and paired block, recursively) to its JSON wire representation with
// big.Int amounts rendered as base-10 strings.
func (block *AccountBlock) ToAccountBlockMarshal() *AccountBlockMarshal {
	aux := &AccountBlockMarshal{
		AccountBlockMarshal: *block.AccountBlock.ToNomMarshalJson(),
		ConfirmationDetail:  block.ConfirmationDetail,
	}
	if block.TokenInfo != nil {
		aux.TokenInfo = block.TokenInfo.ToTokenMarshal()
	}
	if block.PairedAccountBlock != nil {
		aux.PairedAccountBlock = block.PairedAccountBlock.ToAccountBlockMarshal()
	}
	return aux
}

// MarshalJSON encodes the block via its AccountBlockMarshal wire form,
// so big.Int amounts appear as base-10 JSON strings.
func (block *AccountBlock) MarshalJSON() ([]byte, error) {
	return json.Marshal(block.ToAccountBlockMarshal())
}

// UnmarshalJSON decodes the AccountBlockMarshal wire form produced by
// MarshalJSON, converting string-encoded amounts back to *big.Int.
func (block *AccountBlock) UnmarshalJSON(data []byte) error {
	aux := new(AccountBlockMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	block.AccountBlock = *aux.FromNomMarshalJson()
	if aux.TokenInfo != nil {
		block.TokenInfo = aux.TokenInfo.FromTokenMarshal()
	}
	block.ConfirmationDetail = aux.ConfirmationDetail
	if aux.PairedAccountBlock != nil {
		block.PairedAccountBlock = aux.PairedAccountBlock.FromApiMarshalJson()
	}
	return nil
}

// FromApiMarshalJson converts the wire representation back to an
// AccountBlock, parsing string-encoded amounts into *big.Int and
// converting the token info and paired block (recursively) as well.
func (a *AccountBlockMarshal) FromApiMarshalJson() *AccountBlock {
	aux := &AccountBlock{
		ConfirmationDetail: a.ConfirmationDetail,
	}
	block := a.FromNomMarshalJson()
	aux.AccountBlock = *block
	if a.TokenInfo != nil {
		aux.TokenInfo = a.TokenInfo.FromTokenMarshal()
	}
	if a.PairedAccountBlock != nil {
		aux.PairedAccountBlock = a.PairedAccountBlock.FromApiMarshalJson()
	}
	return aux
}

// AccountInfo summarizes an account as returned by
// GetAccountInfoByAddress.
type AccountInfo struct {
	Address types.Address `json:"address"`
	// AccountHeight is the height of the account chain's frontier
	// block, equal to the total number of blocks in the chain; it is 0
	// for an account with no blocks.
	AccountHeight uint64 `json:"accountHeight"`
	// BalanceInfoMap maps each held token standard to its token info
	// and balance.
	BalanceInfoMap map[types.ZenonTokenStandard]*BalanceInfo `json:"balanceInfoMap"`
}

// BalanceInfo is one entry of AccountInfo.BalanceInfoMap: a token
// description and the account's balance in that token.
type BalanceInfo struct {
	TokenInfo *Token `json:"token"`
	// Balance is in raw base units of the token (no decimal scaling)
	// and is serialized as a base-10 JSON string.
	Balance *big.Int `json:"balance"`
}

// BalanceInfoMarshal is the JSON wire representation of BalanceInfo,
// with the balance rendered as a base-10 string.
type BalanceInfoMarshal struct {
	TokenInfo *TokenMarshal `json:"token"`
	Balance   string        `json:"balance"`
}

// ToBalanceInfoMarshal converts the balance info to its JSON wire
// representation, rendering the balance as a base-10 string.
func (b *BalanceInfo) ToBalanceInfoMarshal() BalanceInfoMarshal {
	aux := BalanceInfoMarshal{
		TokenInfo: b.TokenInfo.ToTokenMarshal(),
		Balance:   b.Balance.String(),
	}
	return aux
}

// MarshalJSON encodes the balance info via its BalanceInfoMarshal wire
// form, so the balance appears as a base-10 JSON string.
func (b *BalanceInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.ToBalanceInfoMarshal())
}

// UnmarshalJSON decodes the BalanceInfoMarshal wire form produced by
// MarshalJSON. A balance string that is not a valid base-10 integer
// decodes to 0 rather than producing an error.
func (b *BalanceInfo) UnmarshalJSON(data []byte) error {
	aux := new(BalanceInfoMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	b.TokenInfo = aux.TokenInfo.FromTokenMarshal()
	b.Balance = common.StringToBigInt(aux.Balance)
	return nil
}

// Token is the wire form of an on-chain token (ZTS) description.
// Several fields are renamed in JSON: TokenName is "name", TokenSymbol
// is "symbol", TokenDomain is "domain" and ZenonTokenStandard is
// "tokenStandard". Custom JSON marshalling renders the supplies as
// base-10 strings (see TokenMarshal).
type Token struct {
	TokenName   string `json:"name"`
	TokenSymbol string `json:"symbol"`
	TokenDomain string `json:"domain"`
	// TotalSupply is the circulating supply in raw base units,
	// serialized as a base-10 JSON string.
	TotalSupply *big.Int `json:"totalSupply"`
	// Decimals is the number of decimal places used to display raw
	// base-unit amounts.
	Decimals uint8 `json:"decimals"`
	// Owner is the address allowed to mint and to change ownership.
	Owner types.Address `json:"owner"`
	// ZenonTokenStandard is the token's ZTS identifier.
	ZenonTokenStandard types.ZenonTokenStandard `json:"tokenStandard"`
	// MaxSupply is the maximum mintable supply in raw base units,
	// serialized as a base-10 JSON string.
	MaxSupply  *big.Int `json:"maxSupply"`
	IsBurnable bool     `json:"isBurnable"`
	IsMintable bool     `json:"isMintable"`
	IsUtility  bool     `json:"isUtility"`
}

// TokenMarshal is the JSON wire representation of Token, with
// TotalSupply and MaxSupply rendered as base-10 strings.
type TokenMarshal struct {
	TokenName          string                   `json:"name"`
	TokenSymbol        string                   `json:"symbol"`
	TokenDomain        string                   `json:"domain"`
	TotalSupply        string                   `json:"totalSupply"`
	Decimals           uint8                    `json:"decimals"`
	Owner              types.Address            `json:"owner"`
	ZenonTokenStandard types.ZenonTokenStandard `json:"tokenStandard"`
	MaxSupply          string                   `json:"maxSupply"`
	IsBurnable         bool                     `json:"isBurnable"`
	IsMintable         bool                     `json:"isMintable"`
	IsUtility          bool                     `json:"isUtility"`
}

// ToTokenMarshal converts the token to its JSON wire representation,
// rendering TotalSupply and MaxSupply as base-10 strings.
func (t *Token) ToTokenMarshal() *TokenMarshal {
	aux := &TokenMarshal{
		TokenName:          t.TokenName,
		TokenSymbol:        t.TokenSymbol,
		TokenDomain:        t.TokenDomain,
		MaxSupply:          t.MaxSupply.String(),
		Decimals:           t.Decimals,
		Owner:              t.Owner,
		ZenonTokenStandard: t.ZenonTokenStandard,
		TotalSupply:        t.TotalSupply.String(),
		IsBurnable:         t.IsBurnable,
		IsMintable:         t.IsMintable,
		IsUtility:          t.IsUtility,
	}
	return aux
}

// MarshalJSON encodes the token via its TokenMarshal wire form, so the
// supply fields appear as base-10 JSON strings.
func (t *Token) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.ToTokenMarshal())
}

// UnmarshalJSON decodes the TokenMarshal wire form produced by
// MarshalJSON. Supply strings that are not valid base-10 integers
// decode to 0 rather than producing an error.
func (t *Token) UnmarshalJSON(data []byte) error {
	aux := new(TokenMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	t.TokenName = aux.TokenName
	t.TokenSymbol = aux.TokenSymbol
	t.TokenDomain = aux.TokenDomain
	t.MaxSupply = common.StringToBigInt(aux.MaxSupply)
	t.Decimals = aux.Decimals
	t.Owner = aux.Owner
	t.ZenonTokenStandard = aux.ZenonTokenStandard
	t.TotalSupply = common.StringToBigInt(aux.TotalSupply)
	t.IsBurnable = aux.IsBurnable
	t.IsMintable = aux.IsMintable
	t.IsUtility = aux.IsUtility
	return nil
}

// FromTokenMarshal converts the wire representation back to a Token,
// parsing the supply strings as base-10 integers (invalid strings parse
// to 0).
func (t *TokenMarshal) FromTokenMarshal() *Token {
	return &Token{
		TokenName:          t.TokenName,
		TokenSymbol:        t.TokenSymbol,
		TokenDomain:        t.TokenDomain,
		TotalSupply:        common.StringToBigInt(t.TotalSupply),
		Decimals:           t.Decimals,
		Owner:              t.Owner,
		ZenonTokenStandard: t.ZenonTokenStandard,
		MaxSupply:          common.StringToBigInt(t.MaxSupply),
		IsBurnable:         t.IsBurnable,
		IsMintable:         t.IsMintable,
		IsUtility:          t.IsUtility,
	}
}

// AccountBlockList is a paginated list of account blocks.
type AccountBlockList struct {
	List []*AccountBlock `json:"list"`
	// Count is the total number of blocks available to the query that
	// produced this list, not the length of List; see each LedgerApi
	// method for its exact meaning.
	Count int `json:"count"`
	// More reports whether additional blocks may exist beyond the
	// window the producing method inspected; only
	// GetUnreceivedBlocksByAddress and GetAccountBlocksByPage ever set
	// it to true.
	More bool `json:"more"`
}

// AccountBlockListMarshal is the JSON wire representation of
// AccountBlockList, holding blocks in their string-amount wire form.
type AccountBlockListMarshal struct {
	List  []*AccountBlockMarshal `json:"list"`
	Count int                    `json:"count"`
	More  bool                   `json:"more"`
}

// ToAccountBlockListMarshal converts the list to its JSON wire
// representation, converting each block (including token info and
// paired block) to its string-amount form.
func (abl *AccountBlockList) ToAccountBlockListMarshal() *AccountBlockListMarshal {
	aux := &AccountBlockListMarshal{
		Count: abl.Count,
		More:  abl.More,
	}
	aux.List = make([]*AccountBlockMarshal, 0)
	for idx, block := range abl.List {
		aux.List = append(aux.List, &AccountBlockMarshal{
			AccountBlockMarshal: *block.ToNomMarshalJson(),
			ConfirmationDetail:  block.ConfirmationDetail,
		})
		if block.TokenInfo != nil {
			aux.List[idx].TokenInfo = block.TokenInfo.ToTokenMarshal()
		}
		if block.PairedAccountBlock != nil {
			aux.List[idx].PairedAccountBlock = block.PairedAccountBlock.ToAccountBlockMarshal()
		}
	}
	return aux
}

// MarshalJSON encodes the list via its AccountBlockListMarshal wire
// form, so big.Int amounts appear as base-10 JSON strings.
func (abl *AccountBlockList) MarshalJSON() ([]byte, error) {
	return json.Marshal(abl.ToAccountBlockListMarshal())
}

// UnmarshalJSON decodes the AccountBlockListMarshal wire form produced
// by MarshalJSON, converting string-encoded amounts back to *big.Int.
func (abl *AccountBlockList) UnmarshalJSON(data []byte) error {
	aux := new(AccountBlockListMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	abl.List = make([]*AccountBlock, 0)
	for _, accBl := range aux.List {
		block := accBl.FromApiMarshalJson()
		abl.List = append(abl.List, block)
	}
	abl.Count = aux.Count
	abl.More = aux.More
	return nil
}

// MomentumList is a paginated list of momentums.
type MomentumList struct {
	List []*Momentum `json:"list"`
	// Count is the frontier momentum height at the time of the query
	// (the total number of momentums in the chain), not the length of
	// List.
	Count int `json:"count"`
}

// DetailedMomentumList is a paginated list of momentums paired with the
// account blocks they confirm. Count has the same meaning as in
// MomentumList.
type DetailedMomentumList struct {
	List  []*DetailedMomentum `json:"list"`
	Count int                 `json:"count"`
}

// ToLedgerBlock returns a copy of the embedded nom.AccountBlock (the
// PublicKey backing array is shared with the original), dropping the
// RPC-only extras (token info, confirmation detail, paired block). The
// error is always nil.
func (block *AccountBlock) ToLedgerBlock() (*nom.AccountBlock, error) {
	return block.AccountBlock.Copy(), nil
}

// ComputeHash recomputes the hash of the underlying ledger block from
// its hashed fields; it does not read or modify block.Hash. The error
// is always nil.
func (block *AccountBlock) ComputeHash() (*types.Hash, error) {
	lAb, err := block.ToLedgerBlock()
	if err != nil {
		return nil, err
	}
	hash := lAb.ComputeHash()
	return &hash, nil
}
func (block *AccountBlock) prefetchToken(chain chain.Chain) error {
	store := chain.GetFrontierMomentumStore()
	if block.TokenStandard != types.ZeroTokenStandard {
		token, err := store.GetTokenInfoByTs(block.TokenStandard)
		if err != nil {
			return err
		}
		block.TokenInfo = LedgerTokenInfoToRpc(token)
	}
	return nil
}
func (block *AccountBlock) prefetchPaired(chain chain.Chain) error {
	store := chain.GetFrontierMomentumStore()
	var err error
	var paired *nom.AccountBlock
	if block.BlockType == nom.BlockTypeGenesisReceive {
		genesis := chain.GetGenesisMomentum()
		frontier, _ := store.GetFrontierMomentum()
		block.PairedAccountBlock = &AccountBlock{
			AccountBlock: nom.AccountBlock{
				BlockType:        nom.BlockTypeContractSend,
				Amount:           common.Big0,
				DescendantBlocks: make([]*nom.AccountBlock, 0),
			},
			ConfirmationDetail: &AccountBlockConfirmationDetail{
				NumConfirmations:  frontier.Height - genesis.Height + 1,
				MomentumHeight:    genesis.Height,
				MomentumHash:      genesis.Hash,
				MomentumTimestamp: genesis.Timestamp.Unix(),
			},
		}
		return nil
	}

	if nom.IsSendBlock(block.BlockType) {
		paired, err = store.GetBlockWhichReceives(block.Hash)
	} else {
		paired, err = store.GetAccountBlockByHash(block.FromBlockHash)
	}
	if err != nil {
		return err
	}
	if paired != nil {
		block.PairedAccountBlock = &AccountBlock{
			AccountBlock: *paired.Copy(),
		}
		if err := block.PairedAccountBlock.prefetchToken(chain); err != nil {
			return err
		}
		if err := block.PairedAccountBlock.addConfirmationInfo(chain); err != nil {
			return err
		}
	}

	return nil
}
func (block *AccountBlock) addConfirmationInfo(chain chain.Chain) error {
	store := chain.GetFrontierMomentumStore()
	frontier, err := store.GetFrontierMomentum()
	confirmationHeight, err := chain.GetFrontierMomentumStore().GetBlockConfirmationHeight(block.Hash)
	if err != nil {
		return err
	}
	confirmedBlock, err := chain.GetFrontierMomentumStore().GetMomentumByHeight(confirmationHeight)
	if err != nil {
		return err
	}
	if confirmedBlock != nil && frontier != nil && confirmedBlock.Height <= frontier.Height {
		block.ConfirmationDetail = &AccountBlockConfirmationDetail{
			NumConfirmations:  frontier.Height - confirmedBlock.Height + 1,
			MomentumHeight:    confirmedBlock.Height,
			MomentumHash:      confirmedBlock.Hash,
			MomentumTimestamp: confirmedBlock.Timestamp.Unix(),
		}
	}

	return nil
}
func (block *AccountBlock) addAllExtraInfo(chain chain.Chain) error {
	if err := block.prefetchPaired(chain); err != nil {
		return err
	}
	if err := block.prefetchToken(chain); err != nil {
		return err
	}
	if err := block.addConfirmationInfo(chain); err != nil {
		return err
	}

	return nil
}

func momentumListToDetailedList(chain chain.Chain, list *MomentumList) (*DetailedMomentumList, error) {
	ans := &DetailedMomentumList{
		Count: list.Count,
		List:  make([]*DetailedMomentum, len(list.List)),
	}
	for index, momentum := range list.List {
		store := chain.GetFrontierMomentumStore()
		m, err := store.PrefetchMomentum(momentum.Momentum)
		if err != nil {
			return nil, err
		}
		accountBlocks, err := ledgerAccountBlocksToRpc(chain, m.AccountBlocks)
		if err != nil {
			return nil, err
		}
		ans.List[index] = &DetailedMomentum{
			Momentum:      momentum,
			AccountBlocks: accountBlocks,
		}
	}

	return ans, nil
}
func ledgerMomentumToRpc(m *nom.Momentum) (*Momentum, error) {
	if m == nil {
		return nil, nil
	}
	rm := &Momentum{
		Momentum: m,
		Producer: m.Producer(),
	}

	// Populate null fields with empty ones
	if rm.Data == nil {
		rm.Data = make([]byte, 0)
	}
	if rm.Content == nil {
		rm.Content = make(nom.MomentumContent, 0)
	}

	return rm, nil
}
func ledgerMomentumsToRpc(list []*nom.Momentum) ([]*Momentum, error) {
	momentums := make([]*Momentum, 0, len(list))
	for _, momentum := range list {
		if momentum == nil {
		} else {
			rpc, err := ledgerMomentumToRpc(momentum)
			if err != nil {
				return nil, err
			}
			momentums = append(momentums, rpc)
		}
	}

	return momentums, nil
}
func ledgerAccountBlockToRpc(chain chain.Chain, lAb *nom.AccountBlock) (*AccountBlock, error) {
	rpcBlock := &AccountBlock{
		AccountBlock: *lAb.Copy(),
	}
	if err := rpcBlock.addAllExtraInfo(chain); err != nil {
		return nil, err
	}

	return rpcBlock, nil
}
func ledgerAccountBlocksToRpc(chain chain.Chain, list []*nom.AccountBlock) ([]*AccountBlock, error) {
	if list == nil {
		return []*AccountBlock{}, nil
	}

	blocks := make([]*AccountBlock, 0, len(list))
	for _, block := range list {
		if block == nil {
		} else {
			rpc, err := ledgerAccountBlockToRpc(chain, block)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, rpc)
		}
	}
	return blocks, nil
}

// LedgerTokenInfoToRpc converts an embedded-contract token record to
// the wire Token type, copying supplies as raw base-unit big.Ints. It
// returns nil when tokenInfo is nil.
func LedgerTokenInfoToRpc(tokenInfo *definition.TokenInfo) *Token {
	var rt *Token = nil
	if tokenInfo != nil {
		rt = &Token{
			TokenName:          tokenInfo.TokenName,
			TokenSymbol:        tokenInfo.TokenSymbol,
			TokenDomain:        tokenInfo.TokenDomain,
			TotalSupply:        nil,
			MaxSupply:          nil,
			Decimals:           tokenInfo.Decimals,
			Owner:              tokenInfo.Owner,
			ZenonTokenStandard: tokenInfo.TokenStandard,
			IsBurnable:         tokenInfo.IsBurnable,
			IsMintable:         tokenInfo.IsMintable,
			IsUtility:          tokenInfo.IsUtility,
		}
		if tokenInfo.TotalSupply != nil {
			rt.TotalSupply = tokenInfo.TotalSupply
		}
		if tokenInfo.MaxSupply != nil {
			rt.MaxSupply = tokenInfo.MaxSupply
		}
	}
	return rt
}

// LedgerTokenInfosToRpc converts a slice of embedded-contract token
// records via LedgerTokenInfoToRpc, preserving order. It always returns
// a non-nil slice; a nil or empty input yields an empty slice, and nil
// input entries yield nil output entries.
func LedgerTokenInfosToRpc(list []*definition.TokenInfo) []*Token {
	tokenInfos := make([]*Token, 0)
	for _, item := range list {
		tokenInfos = append(tokenInfos, LedgerTokenInfoToRpc(item))
	}

	return tokenInfos
}
