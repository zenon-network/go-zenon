package abi

import (
	"math/big"
	"sync"
)

const poolLimit = 256

// IntPool is a pool of big integers that can be reused for all
// big.Int operations, avoiding allocations in decoding hot paths.
// Obtain one from PoolOfIntPools.
type IntPool struct {
	pool *stack
}

func newIntPool() *IntPool {
	return &IntPool{pool: newStack()}
}

// Get retrieves a big int from the pool, allocating one if the pool
// is empty. Note, the returned int's value is arbitrary and will not
// be zeroed!
func (p *IntPool) Get() *big.Int {
	if p.pool.len() > 0 {
		return p.pool.pop()
	}
	return new(big.Int)
}

// GetZero retrieves a big int from the pool, setting it to zero or
// allocating a new one if the pool is empty.
func (p *IntPool) GetZero() *big.Int {
	if p.pool.len() > 0 {
		return p.pool.pop().SetUint64(0)
	}
	return new(big.Int)
}

// Put returns allocated big ints to the pool to be later reused by
// Get calls. Note, the values are saved as is; neither Put nor Get
// zeroes the ints out! Once the pool holds more than 256 integers
// further values are dropped.
func (p *IntPool) Put(is ...*big.Int) {
	if len(p.pool.data) > poolLimit {
		return
	}
	for _, i := range is {
		p.pool.push(i)
	}
}

// intPoolPool manages a pool of intPools.
type intPoolPool struct {
	pools []*IntPool
	lock  sync.Mutex
}

const poolDefaultCap = 25

// PoolOfIntPools is the process-wide reservoir of IntPools: Get
// borrows a pool (allocating one when none is free) and Put returns
// it for reuse by other decoders.
var PoolOfIntPools = &intPoolPool{
	pools: make([]*IntPool, 0, poolDefaultCap),
}

// get is looking for an available pool to return.
func (ipp *intPoolPool) Get() *IntPool {
	ipp.lock.Lock()
	defer ipp.lock.Unlock()

	if len(PoolOfIntPools.pools) > 0 {
		ip := ipp.pools[len(ipp.pools)-1]
		ipp.pools = ipp.pools[:len(ipp.pools)-1]
		return ip
	}
	return newIntPool()
}

// put a pool that has been allocated with get.
func (ipp *intPoolPool) Put(ip *IntPool) {
	ipp.lock.Lock()
	defer ipp.lock.Unlock()

	if len(ipp.pools) < cap(ipp.pools) {
		ipp.pools = append(ipp.pools, ip)
	}
}

type stack struct {
	data []*big.Int
}

func newStack() *stack {
	return &stack{data: make([]*big.Int, 0, 1024)}
}

func (st *stack) push(d *big.Int) {
	st.data = append(st.data, d)
}

func (st *stack) pop() (ret *big.Int) {
	ret = st.data[len(st.data)-1]
	st.data = st.data[:len(st.data)-1]
	return
}

func (st *stack) len() int {
	return len(st.data)
}
