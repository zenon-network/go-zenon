package embedded

import (
	"encoding/json"
	"github.com/inconshreveable/log15"
	"github.com/pkg/errors"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
	"github.com/zenon-network/go-zenon/zenon"
)

// BridgeApi implements the "embedded.bridge" JSON-RPC namespace, which
// reads the state of the bridge embedded contract at the account-pool
// frontier. The bridge moves ZTS tokens between NoM and external
// networks: wrap requests lock tokens on NoM for release on a
// destination network once a decentralized orchestrator co-signs them
// with its threshold ECDSA (TSS) key, and unwrap requests register
// TSS-signed transfers coming back from an external network for the
// recipient to redeem on NoM after a per-token delay. Every exported
// method is served as embedded.bridge.<lowerCamelMethodName>.
type BridgeApi struct {
	chain chain.Chain
	log   log15.Logger
}

// NewBridgeApi returns a BridgeApi bound to the given node's chain. It
// is called by the RPC server when the "embedded" namespace is enabled;
// it is not itself an RPC method.
func NewBridgeApi(z zenon.Zenon) *BridgeApi {
	return &BridgeApi{
		chain: z.Chain(),
		log:   common.RPCLogger.New("module", "embedded_bridge_api"),
	}
}

// GetBridgeInfo returns the contract's global state: the Administrator
// address, the compressed and decompressed TSS ECDSA public key
// produced by the orchestrator's key generation ceremony, the
// AllowKeyGen flag that authorizes a new ceremony, the halt state and
// the TssNonce that sequences administrative TSS-signed messages. The
// bridge is halted either while Halted is true (set by the
// administrator, by a TSS-signed Halt call, or by the Emergency method,
// which also resets the administrator to the zero address and clears
// both TSS public keys) or, after an Unhalt call, while the momentum
// height is still at most UnhaltedAt + UnhaltDurationInMomentums.
// Before the state is first written it reads as
// defaults: the initial administrator hard-coded in vm/constants, empty
// keys, not halted.
//
// JSON-RPC: embedded.bridge.getBridgeInfo
func (a *BridgeApi) GetBridgeInfo() (*definition.BridgeInfoVariable, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	return bridgeInfo, nil
}

// GetSecurityInfo returns the time-challenge security configuration of
// the bridge contract: the guardian addresses that can vote in a new
// administrator during an emergency, their current votes, and the two
// challenge delays in momentums (AdministratorDelay for administrator
// and guardian changes, SoftDelay for the other protected methods).
// Before security is initialized it reads as the minimum delays from
// vm/constants with no guardians.
//
// JSON-RPC: embedded.bridge.getSecurityInfo
func (a *BridgeApi) GetSecurityInfo() (*definition.SecurityInfoVariable, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}

	security, err := definition.GetSecurityInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	return security, nil
}

// GetOrchestratorInfo returns the parameters the orchestrator network
// operates under: WindowSize, the length in momentums of the window
// within which only one signing ceremony (wrap or unwrap) can run;
// KeyGenThreshold, the minimum number of participants required for a
// key generation ceremony; ConfirmationsToFinality, the momentums a
// wrap request must age before the orchestrator signs it;
// EstimatedMomentumTime, the assumed seconds per momentum; and
// AllowKeyGenHeight, the momentum height the orchestrator uses as a
// reference to check the last 24 hours of momentum producers before a
// key generation. Before the state is first written every field reads
// as 0.
//
// JSON-RPC: embedded.bridge.getOrchestratorInfo
func (a *BridgeApi) GetOrchestratorInfo() (*definition.OrchestratorInfo, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}

	orchestratorInfo, err := definition.GetOrchestratorInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	return orchestratorInfo, nil
}

// TimeChallengesList holds the recorded time challenges of an embedded
// contract, as reported by the GetTimeChallengesInfo methods of the
// bridge and liquidity APIs. A time challenge delays sensitive
// administrator methods: the first call only records the hash of the
// parameters and the momentum height it was made at, and the change
// takes effect when the same call is repeated with identical parameters
// after the security delay has passed. Each entry keeps the method
// name, the pending parameters hash (all zero once a challenge has
// completed) and the height the challenge started at.
type TimeChallengesList struct {
	Count int                             `json:"count"`
	List  []*definition.TimeChallengeInfo `json:"list"`
}

// GetTimeChallengesInfo returns the recorded time challenges for the
// bridge contract's protected administrator methods: NominateGuardians,
// ChangeTssECDSAPubKey, ChangeAdministrator and SetTokenPair. Methods
// that have never been challenged are omitted from the list.
//
// JSON-RPC: embedded.bridge.getTimeChallengesInfo
func (a *BridgeApi) GetTimeChallengesInfo() (*TimeChallengesList, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}

	ans := make([]*definition.TimeChallengeInfo, 0)
	methods := []string{"NominateGuardians", "ChangeTssECDSAPubKey", "ChangeAdministrator", "SetTokenPair"}

	for _, m := range methods {
		timeC, err := definition.GetTimeChallengeInfoVariable(context.Storage(), m)
		if err != nil {
			return nil, err
		}
		if timeC != nil {
			ans = append(ans, timeC)
		}
	}

	return &TimeChallengesList{
		Count: len(ans),
		List:  ans,
	}, nil
}

// GetNetworkInfo returns the bridged network identified by networkClass
// and chainId. The class distinguishes ledger families (1 for NoM, 2
// for EVM networks) and chainId the specific chain within it. The
// result carries the network's name, the address of the bridge contract
// deployed on that network, free-form metadata and the token pairs that
// can cross it: each pair maps a ZTS token to its address on the other
// network, with bridgeable/redeemable switches, a minimum wrap amount,
// the wrap fee in basis points, the redeem delay in momentums and the
// Owned flag: when the bridge owns the ZTS token, wraps burn the net
// amount and redeems mint it, otherwise wraps hold the tokens in the
// contract and redeems pay out of its balance. An unknown network
// yields a zeroed entry with an empty name rather than an error.
//
// JSON-RPC: embedded.bridge.getNetworkInfo
func (a *BridgeApi) GetNetworkInfo(networkClass uint32, chainId uint32) (*definition.NetworkInfo, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}

	networkInfo, err := definition.GetNetworkInfoVariable(context.Storage(), networkClass, chainId)
	if err != nil {
		return nil, err
	}

	return networkInfo, nil
}

// GetAllNetworks pages over every network registered with the bridge,
// ordered by ascending network class and, within a class, ascending
// chain id. Count is the total number of registered networks that
// parse from storage (unparseable entries are skipped). A
// pageSize above 1024 is rejected with api.ErrPageSizeParamTooBig.
//
// JSON-RPC: embedded.bridge.getAllNetworks
func (a *BridgeApi) GetAllNetworks(pageIndex, pageSize uint32) (*NetworkInfoList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}

	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}
	networkList, err := definition.GetNetworkList(context.Storage())
	if err != nil {
		return nil, err
	}
	start, end := api.GetRange(pageIndex, pageSize, uint32(len(networkList)))
	list := networkList[start:end]

	result := &NetworkInfoList{
		Count: len(networkList),
		List:  list,
	}
	return result, nil
}

// NetworkInfoList is one page of bridged networks as reported by
// GetAllNetworks. Count is the total number of parseable registered
// networks, not the number of entries in List.
type NetworkInfoList struct {
	Count int                       `json:"count"`
	List  []*definition.NetworkInfo `json:"list"`
}

func (a *BridgeApi) toRequest(context vm_context.AccountVmContext, abiRequest *definition.WrapTokenRequest) *definition.WrapTokenRequest {
	if abiRequest == nil {
		return nil
	}
	networkInfoVariable, err := definition.GetNetworkInfoVariable(context.Storage(), abiRequest.NetworkClass, abiRequest.ChainId)
	if err != nil {
		return nil
	}
	tokenAddress := ""
	for i := 0; i < len(networkInfoVariable.TokenPairs); i++ {
		if reflect.DeepEqual(networkInfoVariable.TokenPairs[i].TokenStandard.Bytes(), abiRequest.TokenStandard.Bytes()) {
			tokenAddress = networkInfoVariable.TokenPairs[i].TokenAddress
		}
	}
	if tokenAddress == "" {
		return nil
	}
	request := &definition.WrapTokenRequest{
		NetworkClass: abiRequest.NetworkClass,
		ChainId:      abiRequest.ChainId,
		Id:           abiRequest.Id,
		ToAddress:    abiRequest.ToAddress,
		TokenAddress: tokenAddress,
		Amount:       abiRequest.Amount,
		Signature:    abiRequest.Signature,
	}
	return request
}

// WrapTokenRequest is a contract wrap request augmented with RPC-only
// context. The embedded request records a transfer leaving NoM: Id is
// the hash of the send block that created it, ToAddress the destination
// address on the target network (stored lowercased), Amount the full
// amount sent in smallest units of TokenStandard, Fee the part of it
// kept by the bridge (Amount times the token pair's fee percentage in
// basis points), and Signature the orchestrator's ECDSA signature,
// empty until the request has been signed. TokenInfo is the ZTS token
// registry entry (nil if the token no longer exists) and
// ConfirmationsToFinality the number of momentums still to elapse
// before the orchestrator considers the request final and signs it, 0
// once the request has aged ConfirmationsToFinality momentums from
// CreationMomentumHeight.
type WrapTokenRequest struct {
	*definition.WrapTokenRequest
	TokenInfo               *api.Token `json:"token"`
	ConfirmationsToFinality uint64     `json:"confirmationsToFinality"`
}

// MarshalJSON encodes the request via the wire forms of its embedded
// request and token, so the amount and fee appear as base-10 JSON
// strings.
func (w *WrapTokenRequest) MarshalJSON() ([]byte, error) {
	aux := struct {
		*definition.WrapTokenRequestMarshal
		TokenInfo               *api.TokenMarshal `json:"token"`
		ConfirmationsToFinality uint64            `json:"confirmationsToFinality"`
	}{
		WrapTokenRequestMarshal: w.WrapTokenRequest.ToMarshalJson(),
		ConfirmationsToFinality: w.ConfirmationsToFinality,
	}
	if w.TokenInfo != nil {
		aux.TokenInfo = w.TokenInfo.ToTokenMarshal()
	}

	return json.Marshal(aux)
}

// UnmarshalJSON decodes the wire form produced by MarshalJSON. Amount
// strings that are not valid base-10 integers decode to 0 rather than
// producing an error.
func (w *WrapTokenRequest) UnmarshalJSON(data []byte) error {
	aux := &struct {
		*definition.WrapTokenRequestMarshal
		TokenInfo               *api.TokenMarshal `json:"token"`
		ConfirmationsToFinality uint64            `json:"confirmationsToFinality"`
	}{
		WrapTokenRequestMarshal: &definition.WrapTokenRequestMarshal{},
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	w.WrapTokenRequest = &definition.WrapTokenRequest{
		NetworkClass:           aux.WrapTokenRequestMarshal.NetworkClass,
		ChainId:                aux.ChainId,
		Id:                     aux.Id,
		ToAddress:              aux.ToAddress,
		TokenStandard:          aux.TokenStandard,
		TokenAddress:           aux.TokenAddress,
		Amount:                 common.StringToBigInt(aux.Amount),
		Fee:                    common.StringToBigInt(aux.Fee),
		Signature:              aux.Signature,
		CreationMomentumHeight: aux.CreationMomentumHeight,
	}
	if aux.TokenInfo != nil {
		w.TokenInfo = aux.TokenInfo.FromTokenMarshal()
	}
	w.ConfirmationsToFinality = aux.ConfirmationsToFinality
	return nil
}

func (a *BridgeApi) getToken(zts types.ZenonTokenStandard) (*api.Token, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.TokenContract)
	if err != nil {
		return nil, err
	}
	tokenInfo, err := definition.GetTokenInfo(context.Storage(), zts)
	if err == constants.ErrDataNonExistent {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if tokenInfo != nil {
		return api.LedgerTokenInfoToRpc(tokenInfo), nil
	}
	return nil, nil
}

// countdownFrom returns how many momentums after start the momentum at
// height reaches start+delay: 0 once it has, otherwise the remaining
// count. The request heights come from the account-pool frontier
// (api.GetFrontierContext), which can be ahead of the momentum the
// handler reports against, so start may exceed height; the heights are
// ordered before subtracting so that case counts down from start
// instead of wrapping the unsigned difference to a large value that
// reads as "already reached". A remainder that does not fit in uint64
// saturates rather than wrapping; chain heights never come near it.
func countdownFrom(start uint64, delay uint32, height uint64) uint64 {
	if height >= start {
		if elapsed := height - start; elapsed < uint64(delay) {
			return uint64(delay) - elapsed
		}
		return 0
	}
	behind := start - height
	if behind > math.MaxUint64-uint64(delay) {
		return math.MaxUint64
	}
	return behind + uint64(delay)
}

func (a *BridgeApi) getRedeemableIn(unwrapTokenRequest definition.UnwrapTokenRequest, tokenPair definition.TokenPair, momentum nom.Momentum) uint64 {
	return countdownFrom(unwrapTokenRequest.RegistrationMomentumHeight, tokenPair.RedeemDelay, momentum.Height)
}

func (a *BridgeApi) getConfirmationsToFinality(wrapTokenRequest definition.WrapTokenRequest, confirmationsToFinality uint32, momentum nom.Momentum) uint64 {
	return countdownFrom(wrapTokenRequest.CreationMomentumHeight, confirmationsToFinality, momentum.Height)
}

// GetWrapTokenRequestById returns the wrap request whose id is the hash
// of the send block that created it, augmented with its ZTS token info
// and the momentums left until the orchestrator considers it final. An
// unknown id produces an error rather than a nil result.
//
// JSON-RPC: embedded.bridge.getWrapTokenRequestById
func (a *BridgeApi) GetWrapTokenRequestById(id types.Hash) (*WrapTokenRequest, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}

	wrapTokenRequest, err := definition.GetWrapTokenRequestById(context.Storage(), id)
	if err != nil {
		return nil, err
	}

	token, err := a.getToken(wrapTokenRequest.TokenStandard)
	if err != nil {
		return nil, err
	}

	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	orchestratorInfo, err := definition.GetOrchestratorInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}
	confirmationsToFinality := a.getConfirmationsToFinality(*wrapTokenRequest, orchestratorInfo.ConfirmationsToFinality, *momentum)

	return &WrapTokenRequest{wrapTokenRequest, token, confirmationsToFinality}, nil
}

// WrapTokenRequestList is one page of wrap requests. Count is the total
// number of requests that matched the query, not the number of entries
// in List.
type WrapTokenRequestList struct {
	Count int                 `json:"count"`
	List  []*WrapTokenRequest `json:"list"`
}

// GetAllWrapTokenRequests pages over every wrap request ever created,
// newest first (descending creation momentum height, ties by ascending
// request id). Count is the total number of requests. A request whose
// ZTS token no longer exists is returned with nil token info; a token
// lookup that fails outright fails the whole call.
//
// JSON-RPC: embedded.bridge.getAllWrapTokenRequests
func (a *BridgeApi) GetAllWrapTokenRequests(pageIndex, pageSize uint32) (*WrapTokenRequestList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}

	requests, err := definition.GetWrapTokenRequests(context.Storage())
	if err != nil {
		return nil, err
	}

	result := &WrapTokenRequestList{
		Count: len(requests),
		List:  make([]*WrapTokenRequest, 0),
	}

	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	orchestratorInfo, err := definition.GetOrchestratorInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	start, end := api.GetRange(pageIndex, pageSize, uint32(len(requests)))
	for i := start; i < end; i++ {
		token, err := a.getToken(requests[i].TokenStandard)
		if err != nil {
			return nil, err
		}
		confirmationsToFinality := a.getConfirmationsToFinality(*requests[i], orchestratorInfo.ConfirmationsToFinality, *momentum)
		wrapReqest := &WrapTokenRequest{requests[i], token, confirmationsToFinality}
		result.List = append(result.List, wrapReqest)
	}
	return result, nil
}

// GetAllWrapTokenRequestsByToAddress pages over the wrap requests whose
// destination address equals toAddress, newest first. toAddress is
// lowercased before the comparison, matching how requests store the
// destination, so mixed-case input matches; an empty toAddress matches
// every request. Count is the number of matching requests.
//
// JSON-RPC: embedded.bridge.getAllWrapTokenRequestsByToAddress
func (a *BridgeApi) GetAllWrapTokenRequestsByToAddress(toAddress string, pageIndex, pageSize uint32) (*WrapTokenRequestList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}
	// stored wrap requests lowercase ToAddress at write time; normalize so
	// checksummed EVM addresses match
	toAddress = strings.ToLower(toAddress)
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}

	requests, err := definition.GetWrapTokenRequests(context.Storage())
	if err != nil {
		return nil, err
	}

	result := &WrapTokenRequestList{
		List: make([]*WrapTokenRequest, 0),
	}

	specificRequests := make([]*definition.WrapTokenRequest, 0)
	if toAddress == "" {
		specificRequests = append(specificRequests, requests...)
	} else {
		for _, request := range requests {
			if request.ToAddress == toAddress {
				specificRequests = append(specificRequests, request)
			}
		}
	}
	result.Count = len(specificRequests)
	start, end := api.GetRange(pageIndex, pageSize, uint32(len(specificRequests)))

	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	orchestratorInfo, err := definition.GetOrchestratorInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}
	for i := start; i < end; i++ {
		token, err := a.getToken(specificRequests[i].TokenStandard)
		if err != nil {
			return nil, err
		}
		confirmationsToFinality := a.getConfirmationsToFinality(*specificRequests[i], orchestratorInfo.ConfirmationsToFinality, *momentum)
		wrapRequest := &WrapTokenRequest{specificRequests[i], token, confirmationsToFinality}
		result.List = append(result.List, wrapRequest)
	}
	return result, nil
}

// GetAllWrapTokenRequestsByToAddressNetworkClassAndChainId pages over
// the wrap requests headed to the network identified by networkClass
// and chainId, newest first, optionally narrowed to a destination
// address: an empty toAddress matches every request of the network,
// otherwise the same lowercased match as
// GetAllWrapTokenRequestsByToAddress applies. Count is the number of
// matching requests.
//
// JSON-RPC: embedded.bridge.getAllWrapTokenRequestsByToAddressNetworkClassAndChainId
func (a *BridgeApi) GetAllWrapTokenRequestsByToAddressNetworkClassAndChainId(toAddress string, networkClass, chainId uint32, pageIndex, pageSize uint32) (*WrapTokenRequestList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}
	toAddress = strings.ToLower(toAddress)
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}

	requests, err := definition.GetWrapTokenRequests(context.Storage())
	if err != nil {
		return nil, err
	}

	result := &WrapTokenRequestList{
		List: make([]*WrapTokenRequest, 0),
	}

	specificRequests := make([]*definition.WrapTokenRequest, 0)
	for _, request := range requests {
		if request.NetworkClass == networkClass && request.ChainId == chainId && (toAddress == "" || request.ToAddress == toAddress) {
			specificRequests = append(specificRequests, request)
		}
	}
	result.Count = len(specificRequests)
	start, end := api.GetRange(pageIndex, pageSize, uint32(len(specificRequests)))

	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	orchestratorInfo, err := definition.GetOrchestratorInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	for i := start; i < end; i++ {
		token, err := a.getToken(specificRequests[i].TokenStandard)
		if err != nil {
			return nil, err
		}
		confirmationsToFinality := a.getConfirmationsToFinality(*specificRequests[i], orchestratorInfo.ConfirmationsToFinality, *momentum)
		wrapRequest := &WrapTokenRequest{specificRequests[i], token, confirmationsToFinality}
		result.List = append(result.List, wrapRequest)
	}
	return result, nil
}

// GetAllUnsignedWrapTokenRequests pages over the wrap requests the
// orchestrator has not signed yet (empty signature), oldest first
// (ascending creation momentum height). Count is the total number of
// unsigned requests.
//
// JSON-RPC: embedded.bridge.getAllUnsignedWrapTokenRequests
func (a *BridgeApi) GetAllUnsignedWrapTokenRequests(pageIndex, pageSize uint32) (*WrapTokenRequestList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}

	requests, err := definition.GetWrapTokenRequests(context.Storage())
	if err != nil {
		return nil, err
	}

	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	orchestratorInfo, err := definition.GetOrchestratorInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	// Select the page first; token and finality lookups are only done for
	// the requests on it.
	page, count := selectUnsignedWrapRequests(requests, pageIndex, pageSize)
	result := &WrapTokenRequestList{
		Count: count,
		List:  make([]*WrapTokenRequest, 0, len(page)),
	}
	for _, request := range page {
		token, err := a.getToken(request.TokenStandard)
		if err != nil {
			return nil, err
		}
		confirmationsToFinality := a.getConfirmationsToFinality(*request, orchestratorInfo.ConfirmationsToFinality, *momentum)
		result.List = append(result.List, &WrapTokenRequest{request, token, confirmationsToFinality})
	}
	return result, nil
}

// selectUnsignedWrapRequests returns the requested page of the requests
// without a signature and their total count. Storage lists requests newest
// first (the key holds the creation height subtracted from MaxInt64), and
// the page is taken walking that list backwards, so it is oldest first: the
// order this method has always returned. Only the page is allocated.
func selectUnsignedWrapRequests(requests []*definition.WrapTokenRequest, pageIndex, pageSize uint32) ([]*definition.WrapTokenRequest, int) {
	count := 0
	for _, request := range requests {
		if request.Signature == "" {
			count++
		}
	}
	start, end := api.GetRange(pageIndex, pageSize, uint32(count))
	page := make([]*definition.WrapTokenRequest, 0, end-start)
	seen := uint32(0)
	for i := len(requests) - 1; i >= 0 && seen < end; i-- {
		if requests[i].Signature != "" {
			continue
		}
		if seen >= start {
			page = append(page, requests[i])
		}
		seen++
	}
	return page, count
}

// UnwrapTokenRequest is a contract unwrap request augmented with
// RPC-only context. The embedded request records a transfer entering
// NoM, registered by an orchestrator-signed UnwrapToken call and
// identified by the transaction hash and log index of the deposit event
// on the source network: ToAddress is the NoM address the redemption
// pays (anyone may send the Redeem call), Amount the amount in smallest
// units of TokenStandard, and the Redeemed and Revoked flags are 1 once
// the request has been redeemed or revoked by the administrator.
// TokenInfo is the ZTS token registry entry (nil if the token no longer
// exists) and RedeemableIn the number of momentums still to elapse
// before the token pair's redeem delay has passed, counted from
// RegistrationMomentumHeight; 0 means the delay has elapsed, not that
// Redeem will succeed, which also needs the request unredeemed and
// unrevoked and the bridge active.
type UnwrapTokenRequest struct {
	*definition.UnwrapTokenRequest
	TokenInfo    *api.Token `json:"token"`
	RedeemableIn uint64     `json:"redeemableIn"`
}

// MarshalJSON encodes the request via the wire forms of its embedded
// request and token, so the amount appears as a base-10 JSON string.
func (u *UnwrapTokenRequest) MarshalJSON() ([]byte, error) {
	aux := struct {
		*definition.UnwrapTokenRequestMarshal
		TokenInfo    *api.TokenMarshal `json:"token"`
		RedeemableIn uint64            `json:"redeemableIn"`
	}{
		UnwrapTokenRequestMarshal: u.UnwrapTokenRequest.ToMarshalJson(),
		RedeemableIn:              u.RedeemableIn,
	}
	if u.TokenInfo != nil {
		aux.TokenInfo = u.TokenInfo.ToTokenMarshal()
	}
	return json.Marshal(aux)
}

// UnmarshalJSON decodes the wire form produced by MarshalJSON. Amount
// strings that are not valid base-10 integers decode to 0 rather than
// producing an error.
func (u *UnwrapTokenRequest) UnmarshalJSON(data []byte) error {
	aux := &struct {
		*definition.UnwrapTokenRequestMarshal
		TokenInfo    *api.TokenMarshal `json:"token"`
		RedeemableIn uint64            `json:"redeemableIn"`
	}{
		UnwrapTokenRequestMarshal: &definition.UnwrapTokenRequestMarshal{},
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	u.UnwrapTokenRequest = &definition.UnwrapTokenRequest{
		RegistrationMomentumHeight: aux.RegistrationMomentumHeight,
		NetworkClass:               aux.NetworkClass,
		ChainId:                    aux.ChainId,
		TransactionHash:            aux.TransactionHash,
		LogIndex:                   aux.LogIndex,
		ToAddress:                  aux.ToAddress,
		TokenAddress:               aux.TokenAddress,
		TokenStandard:              aux.TokenStandard,
		Amount:                     common.StringToBigInt(aux.Amount),
		Signature:                  aux.Signature,
		Redeemed:                   aux.Redeemed,
		Revoked:                    aux.Revoked,
	}

	if aux.TokenInfo != nil {
		u.TokenInfo = aux.TokenInfo.FromTokenMarshal()
	}
	u.RedeemableIn = aux.RedeemableIn
	return nil
}

// UnwrapTokenRequestList is one page of unwrap requests. Count is the
// total number of requests that matched the query, not the number of
// entries in List.
type UnwrapTokenRequestList struct {
	Count int                   `json:"count"`
	List  []*UnwrapTokenRequest `json:"list"`
}

// GetUnwrapTokenRequestByHashAndLog returns the unwrap request
// registered for the given deposit transaction hash and event log index
// on the source network, augmented with its ZTS token info and the
// momentums left until it can be redeemed. It produces an error for an
// unknown request and also when the request's token pair has been
// removed from the network's configuration, since the redeem delay can
// no longer be computed.
//
// JSON-RPC: embedded.bridge.getUnwrapTokenRequestByHashAndLog
func (a *BridgeApi) GetUnwrapTokenRequestByHashAndLog(txHash types.Hash, logIndex uint32) (*UnwrapTokenRequest, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}
	request, err := definition.GetUnwrapTokenRequestByTxHashAndLog(context.Storage(), txHash, logIndex)
	if err != nil {
		return nil, err
	}
	token, err := a.getToken(request.TokenStandard)
	if err != nil {
		return nil, err
	}
	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	tokenPair, err := implementation.CheckNetworkAndPairExist(context, request.NetworkClass, request.ChainId, request.TokenAddress)
	if err != nil {
		return nil, err
	}
	if tokenPair == nil {
		return nil, errors.New("token pair not found")
	}
	redeemableIn := a.getRedeemableIn(*request, *tokenPair, *momentum)
	unwrapRequest := &UnwrapTokenRequest{request, token, redeemableIn}

	return unwrapRequest, nil
}

type unwrapPairKey struct {
	networkClass uint32
	chainId      uint32
	tokenAddress string
}

// filterActiveUnwrapRequests drops requests whose token pair (or whole
// network) has been removed, so Count and pagination only see listable
// entries. CheckNetworkAndPairExist signals a missing pair via
// ErrTokenNotFound / ErrUnknownNetwork rather than a nil pair, so those
// sentinels mean "skip" while any other error is a genuine storage failure.
// Pair lookups are cached per (networkClass, chainId, tokenAddress); the
// cache is returned so page building can reuse it, and holds a non-nil pair
// for every returned request.
func filterActiveUnwrapRequests(context vm_context.AccountVmContext, requests []*definition.UnwrapTokenRequest) ([]*definition.UnwrapTokenRequest, map[unwrapPairKey]*definition.TokenPair, error) {
	active := make([]*definition.UnwrapTokenRequest, 0, len(requests))
	pairs := make(map[unwrapPairKey]*definition.TokenPair)
	for _, request := range requests {
		key := unwrapPairKey{request.NetworkClass, request.ChainId, request.TokenAddress}
		pair, ok := pairs[key]
		if !ok {
			var err error
			pair, err = implementation.CheckNetworkAndPairExist(context, request.NetworkClass, request.ChainId, request.TokenAddress)
			if err == constants.ErrTokenNotFound || err == constants.ErrUnknownNetwork {
				pair, err = nil, nil
			}
			if err != nil {
				return nil, nil, err
			}
			pairs[key] = pair
		}
		if pair == nil {
			continue
		}
		active = append(active, request)
	}
	return active, pairs, nil
}

// GetAllUnwrapTokenRequests pages over every unwrap request, sorted
// newest first (descending registration momentum height); the sort is
// stable, so requests registered at the same height keep their
// storage-key order — ascending source transaction hash and then log
// index. Requests whose token pair or network has since been removed
// from the configuration are filtered out before counting and paging,
// so Count is the number of requests that still resolve.
//
// JSON-RPC: embedded.bridge.getAllUnwrapTokenRequests
func (a *BridgeApi) GetAllUnwrapTokenRequests(pageIndex, pageSize uint32) (*UnwrapTokenRequestList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}

	requests, err := definition.GetUnwrapTokenRequests(context.Storage())
	if err != nil {
		return nil, err
	}
	requests, pairs, err := filterActiveUnwrapRequests(context, requests)
	if err != nil {
		return nil, err
	}

	sort.SliceStable(requests, func(i, j int) bool {
		return requests[i].RegistrationMomentumHeight > requests[j].RegistrationMomentumHeight
	})

	result := &UnwrapTokenRequestList{
		Count: len(requests),
		List:  make([]*UnwrapTokenRequest, 0),
	}

	start, end := api.GetRange(pageIndex, pageSize, uint32(len(requests)))
	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	for i := start; i < end; i++ {
		token, err := a.getToken(requests[i].TokenStandard)
		if err != nil {
			return nil, err
		}
		tokenPair := pairs[unwrapPairKey{requests[i].NetworkClass, requests[i].ChainId, requests[i].TokenAddress}]
		redeemableIn := a.getRedeemableIn(*requests[i], *tokenPair, *momentum)
		result.List = append(result.List, &UnwrapTokenRequest{requests[i], token, redeemableIn})
	}
	return result, nil
}

// GetAllUnwrapTokenRequestsByToAddress pages over the unwrap requests
// whose ToAddress is the NoM address toAddress, including ones already
// redeemed or revoked, or every request when toAddress is empty. As in
// GetAllUnwrapTokenRequests, requests whose network or token pair has
// since been removed are filtered out before counting and paging. Either way the result is sorted newest first
// (descending registration momentum height) with a stable sort, so
// requests registered at the same height keep their storage-key order
// (ascending source transaction hash and then log index). Count is the
// number of matching requests.
//
// JSON-RPC: embedded.bridge.getAllUnwrapTokenRequestsByToAddress
func (a *BridgeApi) GetAllUnwrapTokenRequestsByToAddress(toAddress string, pageIndex, pageSize uint32) (*UnwrapTokenRequestList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}

	requests, err := definition.GetUnwrapTokenRequests(context.Storage())
	if err != nil {
		return nil, err
	}

	result := &UnwrapTokenRequestList{
		List: make([]*UnwrapTokenRequest, 0),
	}
	specificRequests := make([]*definition.UnwrapTokenRequest, 0)
	if toAddress == "" {
		specificRequests = append(specificRequests, requests...)
	} else {
		for _, request := range requests {
			if request.ToAddress.String() == toAddress {
				specificRequests = append(specificRequests, request)
			}
		}
	}
	specificRequests, pairs, err := filterActiveUnwrapRequests(context, specificRequests)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(specificRequests, func(i, j int) bool {
		return specificRequests[i].RegistrationMomentumHeight > specificRequests[j].RegistrationMomentumHeight
	})
	result.Count = len(specificRequests)
	start, end := api.GetRange(pageIndex, pageSize, uint32(len(specificRequests)))
	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	for i := start; i < end; i++ {
		token, err := a.getToken(specificRequests[i].TokenStandard)
		if err != nil {
			return nil, err
		}
		tokenPair := pairs[unwrapPairKey{specificRequests[i].NetworkClass, specificRequests[i].ChainId, specificRequests[i].TokenAddress}]
		redeemableIn := a.getRedeemableIn(*specificRequests[i], *tokenPair, *momentum)
		result.List = append(result.List, &UnwrapTokenRequest{specificRequests[i], token, redeemableIn})
	}
	return result, nil
}

// GetFeeTokenPair returns the wrap fees the bridge has accumulated for
// the given ZTS token, in smallest units of that token. A token with no
// recorded fees yields an accumulated fee of 0, not an error.
//
// JSON-RPC: embedded.bridge.getFeeTokenPair
func (a *BridgeApi) GetFeeTokenPair(zts types.ZenonTokenStandard) (*definition.ZtsFeesInfo, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}
	return definition.GetZtsFeesInfoVariable(context.Storage(), zts)
}
