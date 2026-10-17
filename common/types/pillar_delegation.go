package types

import (
	"fmt"
	"math/big"
)

// PillarDelegation is a pillar's delegated weight as seen by the
// consensus election: the pillar name, the address its momentums are
// produced from, and the total delegated weight backing it.
type PillarDelegation struct {
	Name      string
	Producing Address
	Weight    *big.Int
}

// PillarDelegationDetail extends PillarDelegation with the per-address
// breakdown of the delegations that make up the weight.
type PillarDelegationDetail struct {
	PillarDelegation
	Backers map[Address]*big.Int
}

// String renders the delegation as "name@weight" for logging.
func (v *PillarDelegation) String() string {
	return fmt.Sprintf("%v@%v", v.Name, v.Weight)
}

// Merge adds oth's weight and per-backer amounts into the receiver,
// modifying it in place; oth is left unchanged.
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

// SortPDDByWeight implements sort.Interface over
// PillarDelegationDetail slices: descending by weight, with ties
// broken by ascending name, so the order is deterministic.
type SortPDDByWeight []*PillarDelegationDetail

// Len implements sort.Interface.
func (a SortPDDByWeight) Len() int { return len(a) }

// Swap implements sort.Interface.
func (a SortPDDByWeight) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

// Less orders by descending weight, then ascending name.
func (a SortPDDByWeight) Less(i, j int) bool {
	r := a[j].Weight.Cmp(a[i].Weight)
	if r == 0 {
		return a[i].Name < a[j].Name
	} else {
		return r < 0
	}
}

// SortPDByWeight implements sort.Interface over PillarDelegation
// slices: descending by weight, with ties broken by ascending name,
// so the order is deterministic.
type SortPDByWeight []*PillarDelegation

// Len implements sort.Interface.
func (a SortPDByWeight) Len() int { return len(a) }

// Swap implements sort.Interface.
func (a SortPDByWeight) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

// Less orders by descending weight, then ascending name.
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
