package types

import (
	"fmt"
	"math/big"
)

type PillarDelegation struct {
	Name      string
	Producing Address
	Weight    *big.Int
}
type PillarDelegationDetail struct {
	PillarDelegation
	Backers map[Address]*big.Int
}

// Used for logging purposes
func (v *PillarDelegation) String() string {
	return fmt.Sprintf("%v@%v", v.Name, v.Weight)
}

// Add all values together into this object
func (pdd *PillarDelegationDetail) Merge(oth *PillarDelegationDetail) {
	pdd.Weight.Add(pdd.Weight, oth.Weight)
	for addr, amount := range oth.Backers {
		cAmount, ok := pdd.Backers[addr]
		if !ok {
			pdd.Backers[addr] = new(big.Int).Set(amount)
		} else {
			cAmount.Add(cAmount, amount)
		}
	}
}

// Reduce all values by dividing them by count
func (pdd *PillarDelegationDetail) Reduce(count int64) {
	countBig := big.NewInt(count)
	pdd.Weight.Quo(pdd.Weight, countBig)
	for _, amount := range pdd.Backers {
		amount.Quo(amount, countBig)
	}
}

type SortPDDByWeight []*PillarDelegationDetail

func (a SortPDDByWeight) Len() int      { return len(a) }
func (a SortPDDByWeight) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a SortPDDByWeight) Less(i, j int) bool {
	r := a[j].Weight.Cmp(a[i].Weight)
	if r == 0 {
		return a[i].Name < a[j].Name
	} else {
		return r < 0
	}
}

type SortPDByWeight []*PillarDelegation

func (a SortPDByWeight) Len() int      { return len(a) }
func (a SortPDByWeight) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a SortPDByWeight) Less(i, j int) bool {
	r := a[j].Weight.Cmp(a[i].Weight)
	if r == 0 {
		return a[i].Name < a[j].Name
	} else {
		return r < 0
	}
}

// ToPillarDelegation converts delegationDetail to delegation to save memory by dropping backers
func ToPillarDelegation(details []*PillarDelegationDetail) []*PillarDelegation {
	result := make([]*PillarDelegation, len(details))
	for i, detail := range details {
		result[i] = &PillarDelegation{
			Name:      detail.Name,
			Producing: detail.Producing,
			Weight:    new(big.Int).Set(detail.Weight),
		}
	}
	return result
}
