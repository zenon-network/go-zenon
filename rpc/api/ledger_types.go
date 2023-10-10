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

type DetailedMomentum struct {
	AccountBlocks []*AccountBlock `json:"blocks"`
	Momentum      *Momentum       `json:"momentum"`
}
type Momentum struct {
	*nom.Momentum
	Producer types.Address `json:"producer"`
}
type MomentumHeader struct {
	Hash      types.Hash `json:"hash"`
	Height    uint64     `json:"height"`
	Timestamp int64      `json:"timestamp"`
}
type AccountBlockConfirmationDetail struct {
	NumConfirmations  uint64     `json:"numConfirmations"`
	MomentumHeight    uint64     `json:"momentumHeight"`
	MomentumHash      types.Hash `json:"momentumHash"`
	MomentumTimestamp int64      `json:"momentumTimestamp"`
}
type AccountBlock struct {
	nom.AccountBlock

	TokenInfo          *Token                          `json:"token"`
	ConfirmationDetail *AccountBlockConfirmationDetail `json:"confirmationDetail"`
	PairedAccountBlock *AccountBlock                   `json:"pairedAccountBlock"`
}

type AccountBlockMarshal struct {
	nom.AccountBlockMarshal
	TokenInfo          *TokenMarshal                   `json:"token"`
	ConfirmationDetail *AccountBlockConfirmationDetail `json:"confirmationDetail"`
	PairedAccountBlock *AccountBlockMarshal            `json:"pairedAccountBlock"`
}

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

func (block *AccountBlock) MarshalJSON() ([]byte, error) {
	return json.Marshal(block.ToAccountBlockMarshal())
}

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

type AccountInfo struct {
	Address        types.Address                             `json:"address"`
	AccountHeight  uint64                                    `json:"accountHeight"`
	BalanceInfoMap map[types.ZenonTokenStandard]*BalanceInfo `json:"balanceInfoMap"`
}
type BalanceInfo struct {
	TokenInfo *Token   `json:"token"`
	Balance   *big.Int `json:"balance"`
}

type BalanceInfoMarshal struct {
	TokenInfo *TokenMarshal `json:"token"`
	Balance   string        `json:"balance"`
}

func (b *BalanceInfo) ToBalanceInfoMarshal() BalanceInfoMarshal {
	aux := BalanceInfoMarshal{
		TokenInfo: b.TokenInfo.ToTokenMarshal(),
		Balance:   b.Balance.String(),
	}
	return aux
}
func (b *BalanceInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.ToBalanceInfoMarshal())
}
func (b *BalanceInfo) UnmarshalJSON(data []byte) error {
	aux := new(BalanceInfoMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	b.TokenInfo = aux.TokenInfo.FromTokenMarshal()
	b.Balance = common.StringToBigInt(aux.Balance)
	return nil
}

type Token struct {
	TokenName          string                   `json:"name"`
	TokenSymbol        string                   `json:"symbol"`
	TokenDomain        string                   `json:"domain"`
	TotalSupply        *big.Int                 `json:"totalSupply"`
	Decimals           uint8                    `json:"decimals"`
	Owner              types.Address            `json:"owner"`
	ZenonTokenStandard types.ZenonTokenStandard `json:"tokenStandard"`
	MaxSupply          *big.Int                 `json:"maxSupply"`
	IsBurnable         bool                     `json:"isBurnable"`
	IsMintable         bool                     `json:"isMintable"`
	IsUtility          bool                     `json:"isUtility"`
}

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
func (t *Token) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.ToTokenMarshal())
}
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

type AccountBlockList struct {
	List  []*AccountBlock `json:"list"`
	Count int             `json:"count"`
	More  bool            `json:"more"`
}

type AccountBlockListMarshal struct {
	List  []*AccountBlockMarshal `json:"list"`
	Count int                    `json:"count"`
	More  bool                   `json:"more"`
}

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
func (abl *AccountBlockList) MarshalJSON() ([]byte, error) {
	return json.Marshal(abl.ToAccountBlockListMarshal())
}
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

type MomentumList struct {
	List  []*Momentum `json:"list"`
	Count int         `json:"count"`
}
type DetailedMomentumList struct {
	List  []*DetailedMomentum `json:"list"`
	Count int                 `json:"count"`
}

func (block *AccountBlock) ToLedgerBlock() (*nom.AccountBlock, error) {
	return block.AccountBlock.Copy(), nil
}
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
func LedgerTokenInfosToRpc(list []*definition.TokenInfo) []*Token {
	tokenInfos := make([]*Token, 0)
	for _, item := range list {
		tokenInfos = append(tokenInfos, LedgerTokenInfoToRpc(item))
	}

	return tokenInfos
}
