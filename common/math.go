package common

import (
	"math/big"
)

var (
	Big0      = big.NewInt(0)
	Big1      = big.NewInt(1)
	Big2      = big.NewInt(2)
	Big32     = big.NewInt(32)
	Big64     = big.NewInt(64)
	Big100    = big.NewInt(100)
	Big255    = big.NewInt(255)
	Big256    = big.NewInt(256)
	BigP255   = new(big.Int).Exp(Big2, Big255, nil)
	BigP255m1 = new(big.Int).Sub(BigP255, big.NewInt(1))
	BigP256   = new(big.Int).Exp(Big2, Big256, nil)
	BigP256m1 = new(big.Int).Sub(BigP256, big.NewInt(1))
)

func MinInt64(x, y int64) int64 {
	if x < y {
		return x
	}
	return y
}

func MaxInt64(x, y int64) int64 {
	if x > y {
		return x
	}
	return y
}
