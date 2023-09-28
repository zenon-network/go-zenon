package storage

import (
	"encoding/json"
	"math/big"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	"github.com/zenon-network/go-zenon/common/types"
)

type ProducerDetail struct {
	ExpectedNum uint32
	FactualNum  uint32
	Weight      *big.Int
}

func (detail ProducerDetail) Copy() *ProducerDetail {
	return &ProducerDetail{
		ExpectedNum: detail.ExpectedNum,
		FactualNum:  detail.FactualNum,
		Weight:      new(big.Int).Set(detail.Weight),
	}
}
func (detail *ProducerDetail) Merge(c *ProducerDetail) {
	detail.ExpectedNum = detail.ExpectedNum + c.ExpectedNum
	detail.FactualNum = detail.FactualNum + c.FactualNum
	detail.Weight.Add(detail.Weight, c.Weight)
}
func (detail *ProducerDetail) AddNum(ExpectedNum uint32, FactualNum uint32) {
	detail.ExpectedNum = detail.ExpectedNum + ExpectedNum
	detail.FactualNum = detail.FactualNum + FactualNum
}

type Point struct {
	// Last hash that is not in this point
	PrevHash types.Hash
	// Last hash that is in this point
	EndHash     types.Hash
	Pillars     map[string]*ProducerDetail
	TotalWeight *big.Int
}

func (p *Point) Json() string {
	bytes, _ := json.Marshal(p)
	return string(bytes)
}
func (p *Point) Marshal() ([]byte, error) {
	pb := &ConsensusPointProto{}
	pb.EndHash = p.EndHash.Bytes()
	pb.PrevHash = p.PrevHash.Bytes()
	pb.TotalWeight = p.TotalWeight.Bytes()
	if len(p.Pillars) > 0 {
		pb.Content = make([]*ProducerDetailProto, 0, len(p.Pillars))
		for k, v := range p.Pillars {
			c := &ProducerDetailProto{}
			c.Name = k
			c.ExpectedNum = v.ExpectedNum
			c.FactualNum = v.FactualNum
			c.Weight = v.Weight.Bytes()
			pb.Content = append(pb.Content, c)
		}
	}
	buf, err := proto.Marshal(pb)
	if err != nil {
		return nil, err
	}
	return buf, nil
}
func (p *Point) Unmarshal(buf []byte) error {
	pb := &ConsensusPointProto{}

	if unmarshalErr := proto.Unmarshal(buf, pb); unmarshalErr != nil {
		return unmarshalErr
	}

	if len(pb.EndHash) > 0 {
		if err := p.EndHash.SetBytes(pb.EndHash); err != nil {
			return err
		}
	}

	if len(pb.PrevHash) > 0 {
		if err := p.PrevHash.SetBytes(pb.PrevHash); err != nil {
			return err
		}
	}

	p.TotalWeight = big.NewInt(0).SetBytes(pb.TotalWeight)
	p.Pillars = make(map[string]*ProducerDetail, len(pb.Content))
	for _, v := range pb.Content {
		p.Pillars[v.Name] = &ProducerDetail{ExpectedNum: v.ExpectedNum, FactualNum: v.FactualNum, Weight: big.NewInt(0).SetBytes(v.Weight)}
	}
	return nil
}

func (p *Point) LeftAppend(left *Point) error {
	if left.EndHash != p.PrevHash {
		return errors.Errorf("failed to merge consensus points. LeftPoint is [%v,%v) and RightPoint is [%v,%v)", left.PrevHash, left.EndHash, p.PrevHash, p.EndHash)
	}

	p.PrevHash = left.PrevHash
	p.TotalWeight.Add(p.TotalWeight, left.TotalWeight)

	for k, v := range left.Pillars {
		c, ok := p.Pillars[k]
		if !ok {
			p.Pillars[k] = v.Copy()
		} else {
			c.Merge(v)
		}
	}

	return nil
}
func (p *Point) IsEmpty() bool {
	return p.EndHash == p.PrevHash
}

func NewEmptyPoint(proofHash types.Hash) *Point {
	return &Point{
		PrevHash:    proofHash,
		EndHash:     proofHash,
		Pillars:     make(map[string]*ProducerDetail),
		TotalWeight: big.NewInt(0),
	}
}
