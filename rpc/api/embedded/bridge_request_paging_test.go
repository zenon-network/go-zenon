package embedded

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/account"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// The wrap and unwrap request getters page over global collections. They
// used to decode every stored request before slicing out the page; they now
// decode only what the page or the filter needs. The getters below are the
// implementations before that change, kept verbatim as the oracle: every
// page and every Count the new getters return must equal theirs.

var (
	pagingEvmAddresses = []string{
		"0xb794f5ea0ba39494ce839613fffba74279579268",
		"0x5fbdb2315678afecb367f032d93f642f64180aa3",
		"0xe7f1725e7734ce288f8367e1bb143e90bb3f0512",
	}
	pagingTokenAddress = "0x9fe46736679d2d9a65f0992f2272de9f3c7fa6e0"
	pagingNoMAddresses = []types.Address{g.User1.Address, g.User2.Address, g.User3.Address}
)

type pagingNetwork struct {
	networkClass, chainId uint32
}

// Two configured networks, and one the requests name that is not.
var pagingNetworks = []pagingNetwork{{2, 123}, {2, 456}, {3, 7}}

// stagePagingFixture fills storage with the orchestrator configuration,
// two networks with one token pair each, and nWrap wrap and nUnwrap unwrap
// requests spread over several pages. Heights repeat, so the order of
// requests at one height matters; unwrap keys (transaction hash, log
// index) are unrelated to their heights, so the sort moves them; some
// unwrap requests name the unconfigured network or a token without a pair
// and are filtered out; and some requests carry another destination's
// bytes in a different field, so the byte check that skips undecoded
// requests passes values the destination comparison then rejects.
func stagePagingFixture(t *testing.T, storage db.DB, nWrap, nUnwrap int) {
	t.Helper()
	info := &definition.OrchestratorInfo{WindowSize: 10, KeyGenThreshold: 1, ConfirmationsToFinality: 20, EstimatedMomentumTime: 10}
	if err := info.Save(storage); err != nil {
		t.Fatal(err)
	}
	for i, network := range pagingNetworks[:2] {
		encoded, err := definition.EncodeNetworkInfo(&definition.NetworkInfo{
			NetworkClass:    network.networkClass,
			Id:              network.chainId,
			Name:            fmt.Sprintf("network %d", i),
			ContractAddress: "0x0000000000000000000000000000000000000001",
			Metadata:        "{}",
			TokenPairs: []definition.TokenPair{{
				TokenStandard: types.ZnnTokenStandard,
				TokenAddress:  pagingTokenAddress,
				Bridgeable:    true,
				Redeemable:    true,
				MinAmount:     big.NewInt(1),
				RedeemDelay:   uint32(5 + 10*i),
				Metadata:      "{}",
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := encoded.Save(storage); err != nil {
			t.Fatal(err)
		}
	}

	tokens := []types.ZenonTokenStandard{types.ZnnTokenStandard, types.QsrTokenStandard, types.ZenonTokenStandard{1, 2, 3}}
	for i := 0; i < nWrap; i++ {
		network := pagingNetworks[i%len(pagingNetworks)]
		r := &definition.WrapTokenRequest{
			NetworkClass:           network.networkClass,
			ChainId:                network.chainId,
			Id:                     types.NewHash([]byte(fmt.Sprintf("wrap %d", i))),
			ToAddress:              pagingEvmAddresses[(i/2)%len(pagingEvmAddresses)],
			TokenStandard:          tokens[i%len(tokens)],
			TokenAddress:           pagingTokenAddress,
			Amount:                 big.NewInt(int64(1000 + i)),
			Fee:                    big.NewInt(int64(i)),
			CreationMomentumHeight: uint64(1 + i/4),
		}
		if i%5 == 0 {
			// another destination's bytes, in a field that is not ToAddress
			r.TokenAddress = pagingEvmAddresses[(i/2+1)%len(pagingEvmAddresses)]
		}
		if i%3 != 1 {
			r.Signature = fmt.Sprintf("signature %d", i)
		}
		if err := r.Save(storage); err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < nUnwrap; i++ {
		network := pagingNetworks[i%len(pagingNetworks)]
		r := &definition.UnwrapTokenRequest{
			RegistrationMomentumHeight: uint64((i * 7) % 13),
			NetworkClass:               network.networkClass,
			ChainId:                    network.chainId,
			TransactionHash:            types.NewHash([]byte(fmt.Sprintf("unwrap %d", i/2))),
			LogIndex:                   uint32(i % 2),
			ToAddress:                  pagingNoMAddresses[(i/3)%len(pagingNoMAddresses)],
			TokenAddress:               pagingTokenAddress,
			TokenStandard:              tokens[i%len(tokens)],
			Amount:                     big.NewInt(int64(2000 + i)),
			Redeemed:                   uint8(i % 2),
			Revoked:                    uint8((i / 2) % 2),
		}
		if i%7 == 3 {
			// a token without a pair on the network: filtered out
			r.TokenAddress = pagingEvmAddresses[0]
		}
		if i%4 == 0 {
			// another destination's raw bytes, in a field that is not ToAddress
			r.Signature = string(pagingNoMAddresses[(i/3+1)%len(pagingNoMAddresses)].Bytes())
		}
		if err := r.Save(storage); err != nil {
			t.Fatal(err)
		}
	}
}

// newPagingStorage is contract storage that reads a missing key as empty,
// as the frontier account store does, so lookups of an unconfigured
// network behave as on a node.
func newPagingStorage() db.DB {
	return account.NewAccountStore(types.BridgeContract, db.NewMemDB()).Storage()
}

func newPagingBridgeApi(t *testing.T, storage db.DB) *BridgeApi {
	t.Helper()
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	t.Cleanup(z.StopPanic)
	z.InsertMomentumsTo(40)
	cz := newCountingZenon(z)
	cz.chain.replaced[types.BridgeContract] = storage
	return NewBridgeApi(cz)
}

// pagingPages lists the (pageIndex, pageSize) pairs that walk every page of
// a collection of n entries at several page sizes, including empty pages,
// pages past the end and an index whose product with the size wraps
// uint32.
func pagingPages(n int) [][2]uint32 {
	var pages [][2]uint32
	for _, size := range []uint32{0, 1, 2, 3, 7, 10, 16, uint32(n), api.RpcMaxPageSize} {
		last := uint32(2)
		if size != 0 {
			last = uint32(n)/size + 2
		}
		for index := uint32(0); index <= last; index++ {
			pages = append(pages, [2]uint32{index, size})
		}
		pages = append(pages, [2]uint32{math.MaxUint32, size})
	}
	return pages
}

func requireSameResult(t *testing.T, what string, got interface{}, gotErr error, want interface{}, wantErr error) {
	t.Helper()
	if gotErr != nil || wantErr != nil {
		t.Fatalf("%s: error %v, oracle error %v", what, gotErr, wantErr)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("%s:\n got %s\nwant %s", what, gotJSON, wantJSON)
	}
}

// Every page and Count of every wrap and unwrap request getter, for every
// filter the fixture exercises, equals what the previous implementation
// returns.
func TestBridgeRequestPagesMatchFullDecodeOracle(t *testing.T) {
	const nWrap, nUnwrap = 75, 80
	storage := newPagingStorage()
	stagePagingFixture(t, storage, nWrap, nUnwrap)
	b := newPagingBridgeApi(t, storage)

	// The fixture must exercise what the getters do: several pages, ties,
	// filtering and a sort that reorders storage.
	all, err := b.legacyGetAllUnwrapTokenRequests(0, api.RpcMaxPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if all.Count == nUnwrap || all.Count < 3*10 {
		t.Fatalf("%d of %d unwrap requests listed; the fixture must filter some and keep several pages", all.Count, nUnwrap)
	}
	unwrapKeys := make([]string, 0, len(all.List))
	for _, r := range all.List {
		unwrapKeys = append(unwrapKeys, string(r.Key()))
	}
	if sort.StringsAreSorted(unwrapKeys) {
		t.Fatal("the sort does not reorder storage; the fixture does not exercise it")
	}
	unsigned, err := b.legacyGetAllUnsignedWrapTokenRequests(0, api.RpcMaxPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if unsigned.Count == 0 || unsigned.Count == nWrap {
		t.Fatalf("%d of %d wrap requests unsigned", unsigned.Count, nWrap)
	}

	wrapAddresses := append([]string{"", strings.ToUpper(pagingEvmAddresses[1][2:]), "0x" + strings.ToUpper(pagingEvmAddresses[2][2:]), "0x", "0x0000000000000000000000000000000000000000", pagingTokenAddress}, pagingEvmAddresses...)
	unwrapAddresses := []string{"", "nope", strings.ToUpper(pagingNoMAddresses[0].String()), types.ZeroAddress.String(), types.BridgeContract.String(), g.User4.Address.String()}
	for _, address := range pagingNoMAddresses {
		unwrapAddresses = append(unwrapAddresses, address.String())
	}

	for _, page := range pagingPages(nWrap) {
		index, size := page[0], page[1]
		at := fmt.Sprintf("page %d of size %d", index, size)

		got, gotErr := b.GetAllWrapTokenRequests(index, size)
		want, wantErr := b.legacyGetAllWrapTokenRequests(index, size)
		requireSameResult(t, "GetAllWrapTokenRequests "+at, got, gotErr, want, wantErr)

		got, gotErr = b.GetAllUnsignedWrapTokenRequests(index, size)
		want, wantErr = b.legacyGetAllUnsignedWrapTokenRequests(index, size)
		requireSameResult(t, "GetAllUnsignedWrapTokenRequests "+at, got, gotErr, want, wantErr)

		for _, address := range wrapAddresses {
			got, gotErr = b.GetAllWrapTokenRequestsByToAddress(address, index, size)
			want, wantErr = b.legacyGetAllWrapTokenRequestsByToAddress(address, index, size)
			requireSameResult(t, fmt.Sprintf("GetAllWrapTokenRequestsByToAddress(%q) %s", address, at), got, gotErr, want, wantErr)

			for _, network := range append(pagingNetworks, pagingNetwork{9, 9}) {
				got, gotErr = b.GetAllWrapTokenRequestsByToAddressNetworkClassAndChainId(address, network.networkClass, network.chainId, index, size)
				want, wantErr = b.legacyGetAllWrapTokenRequestsByToAddressNetworkClassAndChainId(address, network.networkClass, network.chainId, index, size)
				requireSameResult(t, fmt.Sprintf("GetAllWrapTokenRequestsByToAddressNetworkClassAndChainId(%q, %d, %d) %s", address, network.networkClass, network.chainId, at), got, gotErr, want, wantErr)
			}
		}
	}

	for _, page := range pagingPages(nUnwrap) {
		index, size := page[0], page[1]
		at := fmt.Sprintf("page %d of size %d", index, size)

		got, gotErr := b.GetAllUnwrapTokenRequests(index, size)
		want, wantErr := b.legacyGetAllUnwrapTokenRequests(index, size)
		requireSameResult(t, "GetAllUnwrapTokenRequests "+at, got, gotErr, want, wantErr)

		for _, address := range unwrapAddresses {
			got, gotErr = b.GetAllUnwrapTokenRequestsByToAddress(address, index, size)
			want, wantErr = b.legacyGetAllUnwrapTokenRequestsByToAddress(address, index, size)
			requireSameResult(t, fmt.Sprintf("GetAllUnwrapTokenRequestsByToAddress(%q) %s", address, at), got, gotErr, want, wantErr)
		}
	}
}

// The getters decode only what the page or the filter needs. A stored
// entry that does not decode fails any getter that decodes it, so placing
// one where a getter must not look shows the getter left it alone: the
// unfiltered wrap page counts it by key without decoding it, and the
// destination filters skip it because it does not hold the destination's
// bytes. The previous implementation decoded it on every call. Getters
// that must decode every entry (the signature, network and sort paths)
// still fail.
func TestBridgeRequestPagesDecodeOnlyWhatTheyNeed(t *testing.T) {
	storage := newPagingStorage()
	stagePagingFixture(t, storage, 20, 20)
	// Wrap keys hold MaxInt64 minus the height in decimal, so nine nines
	// sort after every real request: the undecodable entry is the oldest.
	undecodableWrapKey := common.JoinBytes([]byte{1}, []byte("9999999999999999999"), make([]byte, types.HashSize))
	undecodableUnwrapKey := common.JoinBytes([]byte{2}, make([]byte, types.HashSize), make([]byte, 4))
	for _, key := range [][]byte{undecodableWrapKey, undecodableUnwrapKey} {
		if err := storage.Put(key, []byte{0xff}); err != nil {
			t.Fatal(err)
		}
	}
	b := newPagingBridgeApi(t, storage)

	if _, err := b.legacyGetAllWrapTokenRequests(0, 1); err == nil {
		t.Fatal("the previous implementation decoded every request; the fixture must make that fail")
	}

	list, err := b.GetAllWrapTokenRequests(0, 1)
	if err != nil {
		t.Fatalf("first page decoded past the page: %v", err)
	}
	if list.Count != 21 || len(list.List) != 1 {
		t.Fatalf("count %d with %d on the page, want 21 with 1", list.Count, len(list.List))
	}
	if _, err := b.GetAllWrapTokenRequests(20, 1); err == nil {
		t.Fatal("the page holding the undecodable entry did not fail")
	}
	wrapList, err := b.GetAllWrapTokenRequestsByToAddress(pagingEvmAddresses[0], 0, 1)
	if err != nil || wrapList.Count == 0 {
		t.Fatalf("by destination: count %v, error %v", wrapList, err)
	}
	if _, err := b.GetAllWrapTokenRequestsByToAddressNetworkClassAndChainId(pagingEvmAddresses[0], 2, 123, 0, 1); err != nil {
		t.Fatalf("by network and destination: %v", err)
	}
	unwrapList, err := b.GetAllUnwrapTokenRequestsByToAddress(pagingNoMAddresses[0].String(), 0, 1)
	if err != nil || unwrapList.Count == 0 {
		t.Fatalf("unwrap by destination: count %v, error %v", unwrapList, err)
	}

	if _, err := b.GetAllUnsignedWrapTokenRequests(0, 1); err == nil {
		t.Fatal("unsigned requests need every signature decoded")
	}
	if _, err := b.GetAllWrapTokenRequestsByToAddressNetworkClassAndChainId("", 2, 123, 0, 1); err == nil {
		t.Fatal("the network filter without a destination needs every request decoded")
	}
	if _, err := b.GetAllUnwrapTokenRequests(0, 1); err == nil {
		t.Fatal("the unwrap sort needs every request decoded")
	}
}

// A request key the getters could not recover the request's identity from
// still fails every getter, even where the entry is not decoded: the
// previous implementation failed (wrap) or panicked (unwrap) on it, and a
// bounded page must not count it silently.
func TestBridgeRequestPagesRejectMalformedKeys(t *testing.T) {
	// A well-formed packed value of each kind, to store under a bad key.
	source := newPagingStorage()
	stagePagingFixture(t, source, 1, 1)
	var wrapValue, unwrapValue []byte
	if err := definition.ScanWrapTokenRequests(source, func(_, value []byte) error {
		wrapValue = append([]byte(nil), value...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := definition.ScanUnwrapTokenRequests(source, func(_, value []byte) error {
		unwrapValue = append([]byte(nil), value...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	for _, short := range []int{0, 5, types.HashSize - 1} {
		storage := newPagingStorage()
		stagePagingFixture(t, storage, 20, 0)
		// sorts after every real request, so it is never on the first page
		key := common.JoinBytes([]byte{1}, []byte("9999999999999999999"), make([]byte, types.HashSize-1))[:20+short]
		if err := storage.Put(key, wrapValue); err != nil {
			t.Fatal(err)
		}
		b := newPagingBridgeApi(t, storage)
		if _, err := b.legacyGetAllWrapTokenRequests(0, 1); err == nil {
			t.Fatalf("id of %d bytes: the previous implementation accepted it", short)
		}
		if _, err := b.GetAllWrapTokenRequests(0, 1); err == nil {
			t.Fatalf("id of %d bytes: counted without failing", short)
		}
		if _, err := b.GetAllWrapTokenRequestsByToAddress(pagingEvmAddresses[0], 0, 1); err == nil {
			t.Fatalf("id of %d bytes: skipped by the destination filter without failing", short)
		}
	}

	for _, length := range []int{1, 33, 36} {
		storage := newPagingStorage()
		stagePagingFixture(t, storage, 0, 20)
		key := common.JoinBytes([]byte{2}, make([]byte, 36))[:length]
		if err := storage.Put(key, unwrapValue); err != nil {
			t.Fatal(err)
		}
		b := newPagingBridgeApi(t, storage)
		if _, err := b.GetAllUnwrapTokenRequestsByToAddress(g.User5.Address.String(), 0, 1); err == nil {
			t.Fatalf("unwrap key of %d bytes: skipped by the destination filter without failing", length)
		}
	}
}

// legacy getters: the implementations before bounded paging.

func (a *BridgeApi) legacyGetAllWrapTokenRequests(pageIndex, pageSize uint32) (*WrapTokenRequestList, error) {
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

func (a *BridgeApi) legacyGetAllWrapTokenRequestsByToAddress(toAddress string, pageIndex, pageSize uint32) (*WrapTokenRequestList, error) {
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

func (a *BridgeApi) legacyGetAllWrapTokenRequestsByToAddressNetworkClassAndChainId(toAddress string, networkClass, chainId uint32, pageIndex, pageSize uint32) (*WrapTokenRequestList, error) {
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

func (a *BridgeApi) legacyGetAllUnsignedWrapTokenRequests(pageIndex, pageSize uint32) (*WrapTokenRequestList, error) {
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

func (a *BridgeApi) legacyGetAllUnwrapTokenRequests(pageIndex, pageSize uint32) (*UnwrapTokenRequestList, error) {
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

func (a *BridgeApi) legacyGetAllUnwrapTokenRequestsByToAddress(toAddress string, pageIndex, pageSize uint32) (*UnwrapTokenRequestList, error) {
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
