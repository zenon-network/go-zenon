package storage

import (
	"math/big"

	"google.golang.org/protobuf/proto"

	"github.com/zenon-network/go-zenon/common/types"
)

type ElectionData struct {
	Producers   []types.Address
	Delegations []*types.PillarDelegation
}

func (d *ElectionData) Marshal() ([]byte, error) {
	pb := &ElectionDataProto{}
	pb.Delegations = make([]*PillarDelegationProto, 0, len(d.Delegations))
	for _, el := range d.Delegations {
		pb.Delegations = append(pb.Delegations, &PillarDelegationProto{
			Name:             el.Name,
			ProducingAddress: el.Producing.Bytes(),
			Weight:           el.Weight.Bytes()})
	}

	pb.Producers = make([][]byte, 0, len(d.Producers))
	for _, el := range d.Producers {
		pb.Producers = append(pb.Producers, el.Bytes())
	}

	buf, err := proto.Marshal(pb)
	if err != nil {
		return nil, err
	}
	return buf, nil
}
func (d *ElectionData) Unmarshal(buf []byte) error {
	pb := &ElectionDataProto{}
	if err := proto.Unmarshal(buf, pb); err != nil {
		return err
	}

	d.Delegations = make([]*types.PillarDelegation, 0, len(pb.Delegations))
	for _, p := range pb.Delegations {
		addr, err := types.BytesToAddress(p.ProducingAddress)
		if err != nil {
			return err
		}
		d.Delegations = append(d.Delegations, &types.PillarDelegation{
			Weight:    big.NewInt(0).SetBytes(p.Weight),
			Name:      p.Name,
			Producing: addr},
		)
	}

	d.Producers = make([]types.Address, 0, len(pb.Producers))
	for _, p := range pb.Producers {
		addr, err := types.BytesToAddress(p)
		if err != nil {
			return err
		}
		d.Producers = append(d.Producers, addr)
	}

	return nil
}

func GenElectionData(producers []types.Address, delegations []*types.PillarDelegation) *ElectionData {
	return &ElectionData{
		Producers:   producers,
		Delegations: delegations,
	}
}
