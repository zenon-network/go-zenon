package embedded

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// saveSporkState restores the spork globals a test changes, so later tests
// in the process start from the same activation state.
func saveSporkState(t *testing.T) {
	t.Helper()
	savedDP := types.DynamicPlasmaSpork.SporkId
	savedAccel := types.AcceleratorSpork.SporkId
	savedBridge := types.BridgeAndLiquiditySpork.SporkId
	savedHtlc := types.HtlcSpork.SporkId
	savedMap := make(map[types.Hash]bool, len(types.ImplementedSporksMap))
	for k, v := range types.ImplementedSporksMap {
		savedMap[k] = v
	}
	t.Cleanup(func() {
		types.DynamicPlasmaSpork.SporkId = savedDP
		types.AcceleratorSpork.SporkId = savedAccel
		types.BridgeAndLiquiditySpork.SporkId = savedBridge
		types.HtlcSpork.SporkId = savedHtlc
		types.ImplementedSporksMap = savedMap
	})
}

// countingDB counts point lookups and prefix scans on a contract's storage.
type countingDB struct {
	db.DB
	count func()
}

func (c *countingDB) Get(key []byte) ([]byte, error) {
	c.count()
	return c.DB.Get(key)
}

func (c *countingDB) NewIterator(prefix []byte) db.StorageIterator {
	c.count()
	return c.DB.NewIterator(prefix)
}

// countingAccount is a contract's frontier account store whose storage
// lookups are counted, and whose storage may have been replaced.
type countingAccount struct {
	store.Account
	storage db.DB
}

func (c *countingAccount) Storage() db.DB { return c.storage }

// countingChain hands the API a frontier account store per contract whose
// storage lookups are counted by contract address, so a test of a public
// method can tell how much of each contract's storage the call touched. A
// contract's storage can be replaced by an in-memory one to stage state
// that has no cheap on-chain fixture.
type countingChain struct {
	chain.Chain
	mu       sync.Mutex
	lookups  map[types.Address]int
	replaced map[types.Address]db.DB
}

func (c *countingChain) GetFrontierAccountStore(address types.Address) store.Account {
	account := c.Chain.GetFrontierAccountStore(address)
	storage := account.Storage()
	if replaced, ok := c.replaced[address]; ok {
		storage = replaced
	}
	count := func() {
		c.mu.Lock()
		c.lookups[address]++
		c.mu.Unlock()
	}
	return &countingAccount{account, &countingDB{storage, count}}
}

func (c *countingChain) lookupsOn(address types.Address) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lookups[address]
}

func (c *countingChain) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lookups = map[types.Address]int{}
}

// countingZenon is a mock node whose chain counts storage lookups.
type countingZenon struct {
	mock.MockZenon
	chain *countingChain
}

func (z *countingZenon) Chain() chain.Chain { return z.chain }

func newCountingZenon(z mock.MockZenon) *countingZenon {
	return &countingZenon{z, &countingChain{
		Chain:    z.Chain(),
		lookups:  map[types.Address]int{},
		replaced: map[types.Address]db.DB{},
	}}
}

func activateAcceleratorSpork(t *testing.T, z mock.MockZenon) {
	t.Helper()
	sporkAPI := NewSporkApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-accelerator", "activate spork for accelerator"),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	sporkList, err := sporkAPI.GetAll(0, 10)
	if err != nil || len(sporkList.List) == 0 {
		t.Fatalf("spork not created: %v", err)
	}
	id := sporkList.List[0].Id
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data:      definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName, id),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.AcceleratorSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(20)
}

// call sends a contract call and cements it and the contract's reply.
func call(t *testing.T, z mock.MockZenon, block *nom.AccountBlock) {
	t.Helper()
	c := z.CallContract(block)
	z.InsertNewMomentum() // cements the send block
	z.InsertNewMomentum() // cements the contract's receive block
	c.Error(t, nil)
}

func createProjects(t *testing.T, z mock.MockZenon, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		call(t, z, &nom.AccountBlock{
			Address:       g.User1.Address,
			ToAddress:     types.AcceleratorContract,
			TokenStandard: types.ZnnTokenStandard,
			Amount:        constants.ProjectCreationAmount,
			Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
				fmt.Sprintf("Project %d", i), "description", "test.com", big.NewInt(100), big.NewInt(1000)),
		})
	}
}

func projectIds(list *ProjectList) []types.Hash {
	ids := make([]types.Hash, 0, len(list.List))
	for _, p := range list.List {
		ids = append(ids, p.Id)
	}
	return ids
}

func requireIds(t *testing.T, what string, got, want []types.Hash) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d ids, want %d", what, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: id %d is %s, want %s", what, i, got[i], want[i])
		}
	}
}

// GetAll lists every project (the sort needs them all) but only looks up
// votes and phases for the projects on the requested page. The page is the
// same slice of the same order as before: by last update, newest first,
// with the vote breakdown and the phases of every project on it.
func TestAcceleratorGetAllEnrichesOnlyThePage(t *testing.T) {
	saveSporkState(t)
	types.ImplementedSporksMap[types.AcceleratorSpork.SporkId] = true
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	activateAcceleratorSpork(t, z)
	const projects = 6
	createProjects(t, z, projects)

	cz := newCountingZenon(z)
	a := NewAcceleratorApi(cz)
	getAll := func(pageIndex, pageSize uint32) *ProjectList {
		t.Helper()
		list, err := a.GetAll(pageIndex, pageSize)
		if err != nil {
			t.Fatal(err)
		}
		if list.Count != projects {
			t.Fatalf("count %d, want %d", list.Count, projects)
		}
		return list
	}

	// Creation order, newest first, is the order before any update.
	created := projectIds(getAll(0, projects))
	if len(created) != projects {
		t.Fatalf("%d projects listed, want %d", len(created), projects)
	}

	// Two pillars accept one project in the middle of the list; the
	// contract's automatic update makes it active, and its owner adds a
	// phase. Both move its last update forward, so it leads the list; the
	// others keep their creation order.
	accepted := created[3]
	for _, pillar := range []types.Address{g.Pillar1.Address, g.Pillar2.Address} {
		call(t, z, &nom.AccountBlock{
			Address:   pillar,
			ToAddress: types.AcceleratorContract,
			Data:      definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName, accepted, definition.VoteYes),
		})
	}
	z.InsertMomentumsTo(60*6 + 2)
	call(t, z, &nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.AddPhaseMethodName,
			accepted, "Phase 1", "description", "phase.com", big.NewInt(10), big.NewInt(10)),
	})
	want := append([]types.Hash{accepted}, created[:3]...)
	want = append(want, created[4:]...)

	full := getAll(0, projects)
	requireIds(t, "full page", projectIds(full), want)
	lead := full.List[0]
	if lead.Status != definition.ActiveStatus || lead.Votes == nil || lead.Votes.Yes != 2 || lead.Votes.Total != 2 {
		t.Fatalf("accepted project: status %d votes %+v", lead.Status, lead.Votes)
	}
	if len(lead.Phases) != 1 || lead.Phases[0] == nil || lead.Phases[0].Phase.Name != "Phase 1" || lead.Phases[0].Votes == nil {
		t.Fatalf("accepted project phases: %+v", lead.Phases)
	}
	for _, p := range full.List[1:] {
		if p.Status != definition.VotingStatus || p.Votes == nil || p.Votes.Total != 0 || len(p.Phases) != 0 {
			t.Fatalf("project %s: status %d votes %+v phases %d", p.Id, p.Status, p.Votes, len(p.Phases))
		}
	}

	// Pages are slices of that order.
	requireIds(t, "page 0 of 4", projectIds(getAll(0, 4)), want[:4])
	requireIds(t, "page 1 of 4", projectIds(getAll(1, 4)), want[4:])
	requireIds(t, "page past the end", projectIds(getAll(2, 4)), nil)

	// Lookups on the accelerator contract grow with the page: an empty page
	// costs the listing alone, a one-project page costs the listing plus
	// that project's votes and phases, and no more than the average share
	// of the full page.
	lookups := func(pageIndex, pageSize uint32) int {
		cz.chain.reset()
		getAll(pageIndex, pageSize)
		return cz.chain.lookupsOn(types.AcceleratorContract)
	}
	fullCost := lookups(0, projects)
	one := lookups(projects-1, 1) // a project with no phase
	empty := lookups(projects, 1)
	if empty >= one || one >= fullCost {
		t.Fatalf("lookups: empty page %d, one item %d, full page %d; expected them to grow with the page", empty, one, fullCost)
	}
	if one-empty > (fullCost-empty)/projects {
		t.Fatalf("one-item page cost %d lookups beyond the listing, %d per project on average", one-empty, (fullCost-empty)/projects)
	}
}

// stageWrapRequests writes n wrap requests to storage with distinct
// creation heights in id order, every third one signed, and returns the
// ids of the unsigned ones oldest first.
func stageWrapRequests(t *testing.T, storage db.DB, n int) []types.Hash {
	t.Helper()
	info := &definition.OrchestratorInfo{WindowSize: 10, KeyGenThreshold: 1, ConfirmationsToFinality: 20, EstimatedMomentumTime: 10}
	if err := info.Save(storage); err != nil {
		t.Fatal(err)
	}
	var unsigned []types.Hash
	for i := 0; i < n; i++ {
		r := &definition.WrapTokenRequest{
			NetworkClass:           2,
			ChainId:                123,
			Id:                     types.NewHash([]byte{byte(i)}),
			ToAddress:              "0xb794f5ea0ba39494ce839613fffba74279579268",
			TokenStandard:          types.ZnnTokenStandard,
			TokenAddress:           "0x5fbdb2315678afecb367f032d93f642f64180aa3",
			Amount:                 big.NewInt(int64(i + 1)),
			Fee:                    big.NewInt(0),
			CreationMomentumHeight: uint64(3*i + 1),
		}
		if i%3 == 0 {
			r.Signature = "signed"
		} else {
			unsigned = append(unsigned, r.Id)
		}
		if err := r.Save(storage); err != nil {
			t.Fatal(err)
		}
	}
	return unsigned
}

func wrapRequestIds(list *WrapTokenRequestList) []types.Hash {
	ids := make([]types.Hash, 0, len(list.List))
	for _, r := range list.List {
		ids = append(ids, r.Id)
	}
	return ids
}

// Storage lists wrap requests newest first; the unsigned page has always
// been served oldest first, and the selection keeps that order for every
// page while allocating the page only.
func TestUnsignedWrapRequestsAreOldestFirst(t *testing.T) {
	storage := db.NewMemDB()
	unsigned := stageWrapRequests(t, storage, 10)
	requests, err := definition.GetWrapTokenRequests(storage)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 10 {
		t.Fatalf("%d requests read back, want 10", len(requests))
	}
	for i := 1; i < len(requests); i++ {
		if requests[i-1].CreationMomentumHeight <= requests[i].CreationMomentumHeight {
			t.Fatalf("storage order is not newest first at %d: heights %d, %d", i, requests[i-1].CreationMomentumHeight, requests[i].CreationMomentumHeight)
		}
	}

	// The selection before this change: keep the unsigned ones in storage
	// order, reverse, slice.
	legacy := func(pageIndex, pageSize uint32) ([]types.Hash, int) {
		var ids []types.Hash
		for _, r := range requests {
			if r.Signature == "" {
				ids = append(ids, r.Id)
			}
		}
		for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
			ids[i], ids[j] = ids[j], ids[i]
		}
		start, end := api.GetRange(pageIndex, pageSize, uint32(len(ids)))
		return ids[start:end], len(ids)
	}

	full, count := selectUnsignedWrapRequests(requests, 0, 10)
	if count != len(unsigned) {
		t.Fatalf("count %d, want %d", count, len(unsigned))
	}
	got := make([]types.Hash, 0, len(full))
	for _, r := range full {
		got = append(got, r.Id)
	}
	requireIds(t, "all unsigned", got, unsigned)

	for pageSize := uint32(0); pageSize <= 8; pageSize++ {
		for pageIndex := uint32(0); pageIndex <= 8; pageIndex++ {
			page, count := selectUnsignedWrapRequests(requests, pageIndex, pageSize)
			wantIds, wantCount := legacy(pageIndex, pageSize)
			if count != wantCount {
				t.Fatalf("page %d of %d: count %d, want %d", pageIndex, pageSize, count, wantCount)
			}
			ids := make([]types.Hash, 0, len(page))
			for _, r := range page {
				ids = append(ids, r.Id)
			}
			requireIds(t, fmt.Sprintf("page %d of %d", pageIndex, pageSize), ids, wantIds)
			if cap(page) > len(page) {
				t.Fatalf("page %d of %d: %d allocated for %d requests", pageIndex, pageSize, cap(page), len(page))
			}
		}
	}
}

// The public method enriches (token lookup, confirmations to finality)
// only the requests on the page: none for an empty page, one token lookup
// per request otherwise, and an empty page serializes as an empty list.
func TestBridgeUnsignedWrapRequestsEnrichOnlyThePage(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	z.InsertMomentumsTo(40)

	storage := db.NewMemDB()
	unsigned := stageWrapRequests(t, storage, 10)
	cz := newCountingZenon(z)
	cz.chain.replaced[types.BridgeContract] = storage
	b := NewBridgeApi(cz)

	get := func(pageIndex, pageSize uint32) (*WrapTokenRequestList, int) {
		t.Helper()
		cz.chain.reset()
		list, err := b.GetAllUnsignedWrapTokenRequests(pageIndex, pageSize)
		if err != nil {
			t.Fatal(err)
		}
		if list.Count != len(unsigned) {
			t.Fatalf("count %d, want %d", list.Count, len(unsigned))
		}
		return list, cz.chain.lookupsOn(types.TokenContract)
	}

	// Each request on a page carries its token and its confirmations to
	// finality: with the frontier at 40 and 20 confirmations required, a
	// request created at height h needs h+20-40 more when that is positive.
	requireEnriched := func(list *WrapTokenRequestList) {
		t.Helper()
		for _, r := range list.List {
			if r.TokenInfo == nil || r.TokenInfo.ZenonTokenStandard != types.ZnnTokenStandard {
				t.Fatalf("request %s: token %+v", r.Id, r.TokenInfo)
			}
			var want uint64
			if r.CreationMomentumHeight+20 > 40 {
				want = r.CreationMomentumHeight + 20 - 40
			}
			if r.ConfirmationsToFinality != want {
				t.Fatalf("request %s at height %d: %d confirmations to finality, want %d", r.Id, r.CreationMomentumHeight, r.ConfirmationsToFinality, want)
			}
		}
	}

	list, tokenLookups := get(0, 2)
	requireIds(t, "page 0 of 2", wrapRequestIds(list), unsigned[:2])
	if tokenLookups != 2 {
		t.Fatalf("page of 2: %d token lookups, want 2", tokenLookups)
	}
	requireEnriched(list)
	// Heights 4 and 7 are final; the last page's 22 and 25 are not yet.
	if list.List[0].ConfirmationsToFinality != 0 {
		t.Fatalf("request at height 4: %d confirmations to finality, want 0", list.List[0].ConfirmationsToFinality)
	}

	list, tokenLookups = get(1, 4)
	requireIds(t, "page 1 of 4", wrapRequestIds(list), unsigned[4:])
	if tokenLookups != len(unsigned)-4 {
		t.Fatalf("page 1 of 4: %d token lookups, want %d", tokenLookups, len(unsigned)-4)
	}
	requireEnriched(list)
	if got := list.List[0].ConfirmationsToFinality; got != 2 {
		t.Fatalf("request at height 22: %d confirmations to finality, want 2", got)
	}

	list, tokenLookups = get(3, 2)
	if len(list.List) != 0 || tokenLookups != 0 {
		t.Fatalf("page past the end: %d items, %d token lookups", len(list.List), tokenLookups)
	}
	encoded, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"list":[]`) {
		t.Fatalf("empty page encodes as %s", encoded)
	}

	list, tokenLookups = get(0, uint32(len(unsigned)))
	requireIds(t, "full page", wrapRequestIds(list), unsigned)
	if tokenLookups != len(unsigned) {
		t.Fatalf("full page: %d token lookups, want %d", tokenLookups, len(unsigned))
	}
	requireEnriched(list)
}
