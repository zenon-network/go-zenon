package protocol

import (
	"errors"
	"sync"
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

type rollbackTestChain struct {
	chain.Chain
	rollbackErr      error
	cacheRollbackErr error
	rollbackCalls    int
	cacheCalls       int
}

func (c *rollbackTestChain) RollbackTo(sync.Locker, types.HashHeight) error {
	c.rollbackCalls++
	return c.rollbackErr
}

func (c *rollbackTestChain) RollbackCacheTo(sync.Locker, types.HashHeight) error {
	c.cacheCalls++
	return c.cacheRollbackErr
}

func TestRollbackSideChainCacheFailureIsUnrecoverable(t *testing.T) {
	testChain := &rollbackTestChain{cacheRollbackErr: errors.New("cache rollback failed")}
	bridge := chainBridge{chain: testChain}

	err := bridge.rollbackSideChain(&sync.Mutex{}, types.HashHeight{Height: 1})

	var uncertain *chain.ErrCanonicalStateUncertain
	common.ExpectTrue(t, errors.As(err, &uncertain))
	common.ExpectTrue(t, uncertain.Cause != nil)
	common.ExpectTrue(t, uncertain.RollbackErr != nil)
	common.Expect(t, testChain.rollbackCalls, 1)
	common.Expect(t, testChain.cacheCalls, 1)
}

func TestRollbackSideChainCanonicalFailureSkipsCache(t *testing.T) {
	rollbackErr := errors.New("canonical rollback failed")
	testChain := &rollbackTestChain{rollbackErr: rollbackErr}
	bridge := chainBridge{chain: testChain}

	err := bridge.rollbackSideChain(&sync.Mutex{}, types.HashHeight{Height: 1})

	common.ExpectTrue(t, errors.Is(err, rollbackErr))
	common.Expect(t, testChain.rollbackCalls, 1)
	common.Expect(t, testChain.cacheCalls, 0)
}

func TestRollbackSideChainSuccess(t *testing.T) {
	testChain := &rollbackTestChain{}
	bridge := chainBridge{chain: testChain}

	err := bridge.rollbackSideChain(&sync.Mutex{}, types.HashHeight{Height: 1})

	common.FailIfErr(t, err)
	common.Expect(t, testChain.rollbackCalls, 1)
	common.Expect(t, testChain.cacheCalls, 1)
}
