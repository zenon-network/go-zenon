package embedded

import (
	"fmt"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// countingDB counts point lookups and prefix scans on the contract storage
// so a test can tell how much of a collection a call touched beyond
// listing it.
type countingDB struct {
	db.DB
	gets int32
}

func (c *countingDB) Get(key []byte) ([]byte, error) {
	atomic.AddInt32(&c.gets, 1)
	return c.DB.Get(key)
}

func (c *countingDB) NewIterator(prefix []byte) db.StorageIterator {
	atomic.AddInt32(&c.gets, 1)
	return c.DB.NewIterator(prefix)
}

type countingContext struct {
	vm_context.AccountVmContext
	store *countingDB
}

func (c *countingContext) Storage() db.DB { return c.store }

func newCountingContext(t *testing.T, z mock.MockZenon, contract types.Address) *countingContext {
	t.Helper()
	_, context, err := api.GetFrontierContext(z.Chain(), contract)
	if err != nil {
		t.Fatal(err)
	}
	return &countingContext{context, &countingDB{DB: context.Storage()}}
}

func (c *countingContext) reset()       { atomic.StoreInt32(&c.store.gets, 0) }
func (c *countingContext) lookups() int { return int(atomic.LoadInt32(&c.store.gets)) }

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

func createProjects(t *testing.T, z mock.MockZenon, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		call := z.CallContract(&nom.AccountBlock{
			Address:       g.User1.Address,
			ToAddress:     types.AcceleratorContract,
			TokenStandard: types.ZnnTokenStandard,
			Amount:        constants.ProjectCreationAmount,
			Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
				fmt.Sprintf("Project %d", i), "description", "test.com", big.NewInt(100), big.NewInt(1000)),
		})
		z.InsertNewMomentum() // cements the send block
		z.InsertNewMomentum() // cements the contract's receive block
		call.Error(t, nil)
	}
}

// GetAll lists every project (the sort needs them all) but must only look
// up votes and phases for the projects on the requested page.
func TestAcceleratorGetAllEnrichesOnlyThePage(t *testing.T) {
	types.ImplementedSporksMap[types.AcceleratorSpork.SporkId] = true
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	activateAcceleratorSpork(t, z)
	const projects = 6
	createProjects(t, z, projects)

	a := NewAcceleratorApi(z)
	ctx := newCountingContext(t, z, types.AcceleratorContract)
	lookups := func(pageIndex, pageSize uint32) int {
		ctx.reset()
		list, err := a.getAll(ctx, pageIndex, pageSize)
		if err != nil {
			t.Fatal(err)
		}
		if list.Count != projects {
			t.Fatalf("count %d, want %d", list.Count, projects)
		}
		return ctx.lookups()
	}

	full := lookups(0, projects)
	one := lookups(0, 1)
	empty := lookups(projects, 1)
	if !(empty < one && one < full) {
		t.Fatalf("lookups: empty page %d, one item %d, full page %d; expected them to grow with the page", empty, one, full)
	}
	perProject := (full - empty) / projects
	if one-empty > perProject {
		t.Fatalf("one-item page cost %d lookups beyond the listing, %d per project", one-empty, perProject)
	}

	// Page contents are unchanged: the sort and the slicing still apply.
	list, err := a.getAll(ctx, 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.List) != 2 || list.List[0].Votes == nil {
		t.Fatalf("page 1 of size 4 over %d projects: %d items, votes present=%v", projects, len(list.List), list.List[0].Votes != nil)
	}
}

// The unsigned wrap request page is chosen before any request is enriched:
// newest first, counted over all unsigned requests, sliced to the page.
func TestSelectUnsignedWrapRequests(t *testing.T) {
	requests := make([]*definition.WrapTokenRequest, 0, 10)
	for i := 0; i < 10; i++ {
		r := &definition.WrapTokenRequest{Id: types.NewHash([]byte{byte(i)})}
		if i%3 == 0 {
			r.Signature = "signed"
		}
		requests = append(requests, r)
	}
	// unsigned: 1 2 4 5 7 8 -> newest first: 8 7 5 4 2 1
	page, count := selectUnsignedWrapRequests(requests, 1, 2)
	if count != 6 {
		t.Fatalf("count %d, want 6", count)
	}
	if len(page) != 2 || page[0].Id != types.NewHash([]byte{5}) || page[1].Id != types.NewHash([]byte{4}) {
		t.Fatalf("page 1 of size 2: %v", page)
	}
	if page, _ := selectUnsignedWrapRequests(requests, 3, 2); len(page) != 0 {
		t.Fatalf("page past the end returned %d items", len(page))
	}
	if page, _ := selectUnsignedWrapRequests(requests, 0, 6); len(page) != 6 || page[0].Id != types.NewHash([]byte{8}) || page[5].Id != types.NewHash([]byte{1}) {
		t.Fatalf("full page: %v", page)
	}
}
