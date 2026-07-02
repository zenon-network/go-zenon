package embedded

import (
	"encoding/json"
	"github.com/inconshreveable/log15"
	"github.com/pkg/errors"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"reflect"
	"sort"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
	"github.com/zenon-network/go-zenon/zenon"
)

// BridgeApi implements the "embedded.bridge" JSON-RPC namespace, which
// reads the state of the bridge embedded contract as of the frontier
// momentum. The bridge moves ZTS tokens between NoM and external
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
	}{}
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

func (a *BridgeApi) getRedeemableIn(unwrapTokenRequest definition.UnwrapTokenRequest, tokenPair definition.TokenPair, momentum nom.Momentum) uint64 {
	var redeemableIn uint64
	if momentum.Height-unwrapTokenRequest.RegistrationMomentumHeight >= uint64(tokenPair.RedeemDelay) {
		redeemableIn = 0
	} else {
		redeemableIn = unwrapTokenRequest.RegistrationMomentumHeight + uint64(tokenPair.RedeemDelay) - momentum.Height
	}
	return redeemableIn
}

func (a *BridgeApi) getConfirmationsToFinality(wrapTokenRequest definition.WrapTokenRequest, confirmationsToFinality uint32, momentum nom.Momentum) (uint64, error) {
	var actualConfirmationsToFinality uint64
	if momentum.Height-wrapTokenRequest.CreationMomentumHeight >= uint64(confirmationsToFinality) {
		actualConfirmationsToFinality = 0
	} else {
		actualConfirmationsToFinality = wrapTokenRequest.CreationMomentumHeight + uint64(confirmationsToFinality) - momentum.Height
	}
	return actualConfirmationsToFinality, nil
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
	confirmationsToFinality, err := a.getConfirmationsToFinality(*wrapTokenRequest, orchestratorInfo.ConfirmationsToFinality, *momentum)
	if err != nil {
		return nil, err
	}

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
// ZTS token no longer exists is returned with nil token info; one whose
// token lookup fails outright is silently dropped from the page.
//
// JSON-RPC: embedded.bridge.getAllWrapTokenRequests
func (a *BridgeApi) GetAllWrapTokenRequests(pageIndex, pageSize uint32) (*WrapTokenRequestList, error) {
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
			continue
		}
		confirmationsToFinality, err := a.getConfirmationsToFinality(*requests[i], orchestratorInfo.ConfirmationsToFinality, *momentum)
		if err != nil {
			continue
		}
		wrapReqest := &WrapTokenRequest{requests[i], token, confirmationsToFinality}
		result.List = append(result.List, wrapReqest)
	}
	return result, nil
}

// GetAllWrapTokenRequestsByToAddress pages over the wrap requests whose
// destination address equals toAddress, newest first. The comparison is
// an exact string match and requests store the destination lowercased,
// so a mixed-case toAddress matches nothing; an empty toAddress matches
// every request. Count is the number of matching requests.
//
// JSON-RPC: embedded.bridge.getAllWrapTokenRequestsByToAddress
func (a *BridgeApi) GetAllWrapTokenRequestsByToAddress(toAddress string, pageIndex, pageSize uint32) (*WrapTokenRequestList, error) {
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
			continue
		}
		confirmationsToFinality, err := a.getConfirmationsToFinality(*specificRequests[i], orchestratorInfo.ConfirmationsToFinality, *momentum)
		if err != nil {
			continue
		}
		wrapRequest := &WrapTokenRequest{specificRequests[i], token, confirmationsToFinality}
		result.List = append(result.List, wrapRequest)
	}
	return result, nil
}

// GetAllWrapTokenRequestsByToAddressNetworkClassAndChainId pages over
// the wrap requests headed to the network identified by networkClass
// and chainId, newest first, optionally narrowed to a destination
// address: an empty toAddress matches every request of the network,
// otherwise the same exact, lowercase string match as
// GetAllWrapTokenRequestsByToAddress applies. Count is the number of
// matching requests.
//
// JSON-RPC: embedded.bridge.getAllWrapTokenRequestsByToAddressNetworkClassAndChainId
func (a *BridgeApi) GetAllWrapTokenRequestsByToAddressNetworkClassAndChainId(toAddress string, networkClass, chainId uint32, pageIndex, pageSize uint32) (*WrapTokenRequestList, error) {
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
			continue
		}
		confirmationsToFinality, err := a.getConfirmationsToFinality(*specificRequests[i], orchestratorInfo.ConfirmationsToFinality, *momentum)
		if err != nil {
			continue
		}
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
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}

	requests, err := definition.GetWrapTokenRequests(context.Storage())
	if err != nil {
		return nil, err
	}
	var unsignedRequests []*WrapTokenRequest

	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	orchestratorInfo, err := definition.GetOrchestratorInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	for _, request := range requests {
		if request.Signature == "" {
			token, err := a.getToken(request.TokenStandard)
			if err != nil {
				continue
			}
			confirmationsToFinality, err := a.getConfirmationsToFinality(*request, orchestratorInfo.ConfirmationsToFinality, *momentum)
			if err != nil {
				continue
			}
			wrapRequest := &WrapTokenRequest{request, token, confirmationsToFinality}
			unsignedRequests = append(unsignedRequests, wrapRequest)
		}
	}

	for i, j := 0, len(unsignedRequests)-1; i < j; i, j = i+1, j-1 {
		unsignedRequests[i], unsignedRequests[j] = unsignedRequests[j], unsignedRequests[i]
	}

	result := &WrapTokenRequestList{
		Count: len(unsignedRequests),
		List:  make([]*WrapTokenRequest, len(unsignedRequests)),
	}

	start, end := api.GetRange(pageIndex, pageSize, uint32(len(unsignedRequests)))
	result.List = unsignedRequests[start:end]
	return result, nil
}

// UnwrapTokenRequest is a contract unwrap request augmented with
// RPC-only context. The embedded request records a transfer entering
// NoM, registered by an orchestrator-signed UnwrapToken call and
// identified by the transaction hash and log index of the deposit event
// on the source network: ToAddress is the NoM address that may redeem
// it, Amount the amount in smallest units of TokenStandard, and the
// Redeemed and Revoked flags are 1 once the request has been redeemed
// or revoked by the administrator. TokenInfo is the ZTS token registry
// entry (nil if the token no longer exists) and RedeemableIn the number
// of momentums still to elapse before Redeem is accepted, 0 once the
// request has aged the token pair's redeem delay from
// RegistrationMomentumHeight.
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
	}{}
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

// GetAllUnwrapTokenRequests pages over every unwrap request, ordered by
// its storage key, ascending source transaction hash and then log
// index, which bears no relation to registration time. Count is the
// total number of requests. The whole call fails if any request on the
// page references a token pair that has been removed from its network's
// configuration.
//
// JSON-RPC: embedded.bridge.getAllUnwrapTokenRequests
func (a *BridgeApi) GetAllUnwrapTokenRequests(pageIndex, pageSize uint32) (*UnwrapTokenRequestList, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.BridgeContract)
	if err != nil {
		return nil, err
	}

	requests, err := definition.GetUnwrapTokenRequests(context.Storage())
	if err != nil {
		return nil, err
	}

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
			continue
		}
		tokenPair, err := implementation.CheckNetworkAndPairExist(context, requests[i].NetworkClass, requests[i].ChainId, requests[i].TokenAddress)
		if err != nil {
			return nil, err
		}
		if tokenPair == nil {
			return nil, errors.New("token pair not found")
		}
		redeemableIn := a.getRedeemableIn(*requests[i], *tokenPair, *momentum)
		result.List = append(result.List, &UnwrapTokenRequest{requests[i], token, redeemableIn})
	}
	return result, nil
}

// GetAllUnwrapTokenRequestsByToAddress pages over the unwrap requests
// redeemable by the NoM address toAddress. With a non-empty toAddress
// the matching requests are sorted newest first (descending
// registration momentum height; the comparator has no tie-break, so
// requests registered at the same height appear in unspecified order).
// An empty toAddress matches every request but skips the sort, leaving
// the storage-key order of GetAllUnwrapTokenRequests. Count is the
// number of matching requests.
//
// JSON-RPC: embedded.bridge.getAllUnwrapTokenRequestsByToAddress
func (a *BridgeApi) GetAllUnwrapTokenRequestsByToAddress(toAddress string, pageIndex, pageSize uint32) (*UnwrapTokenRequestList, error) {
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
		sort.Slice(specificRequests[:], func(i, j int) bool {
			return specificRequests[i].RegistrationMomentumHeight > specificRequests[j].RegistrationMomentumHeight
		})

	}
	result.Count = len(specificRequests)
	start, end := api.GetRange(pageIndex, pageSize, uint32(len(specificRequests)))
	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	for i := start; i < end; i++ {
		token, err := a.getToken(specificRequests[i].TokenStandard)
		if err != nil {
			continue
		}
		tokenPair, err := implementation.CheckNetworkAndPairExist(context, specificRequests[i].NetworkClass, specificRequests[i].ChainId, specificRequests[i].TokenAddress)
		if err != nil {
			return nil, err
		}
		if tokenPair == nil {
			return nil, errors.New("token pair not found")
		}
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
