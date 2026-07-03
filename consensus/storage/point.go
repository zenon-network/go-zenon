package storage

import (
	"encoding/json"
	"math/big"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	"github.com/zenon-network/go-zenon/common/types"
)

// ProducerDetail is one pillar's statistics within a Point: the
// number of momentums the pillar was expected to produce in the
// point's range (its elected slots), the number it actually
// produced, and the delegated weight backing it — the weight of its
// election for a period point, the average over the merged period
// points for an epoch point.
type ProducerDetail struct {
	ExpectedNum uint32
	FactualNum  uint32
	Weight      *big.Int
}

// Copy returns a deep copy of the detail, with its own weight.
func (detail ProducerDetail) Copy() *ProducerDetail {
	return &ProducerDetail{
		ExpectedNum: detail.ExpectedNum,
		FactualNum:  detail.FactualNum,
		Weight:      new(big.Int).Set(detail.Weight),
	}
}

// Merge adds c's production counts and weight into detail.
func (detail *ProducerDetail) Merge(c *ProducerDetail) {
	detail.ExpectedNum = detail.ExpectedNum + c.ExpectedNum
	detail.FactualNum = detail.FactualNum + c.FactualNum
	detail.Weight.Add(detail.Weight, c.Weight)
}

// AddNum adds the given expected and factual production counts to
// the detail, leaving the weight untouched.
func (detail *ProducerDetail) AddNum(ExpectedNum uint32, FactualNum uint32) {
	detail.ExpectedNum = detail.ExpectedNum + ExpectedNum
	detail.FactualNum = detail.FactualNum + FactualNum
}

// Point aggregates pillar statistics over a contiguous range of
// momentums — one election tick for period points, one 24-hour epoch
// for epoch points — and is stored in the consensus database under
// its tick height. EndHash pins the chain state the point was
// computed from: the points system discards a stored point whose end
// momentum no longer matches the chain and regenerates it.
type Point struct {
	// Last hash that is not in this point
	PrevHash types.Hash
	// Last hash that is in this point
	EndHash types.Hash
	// Pillars holds the per-pillar statistics, keyed by pillar name.
	Pillars map[string]*ProducerDetail
	// TotalWeight is the delegated weight summed over all pillars.
	TotalWeight *big.Int
}

// Json returns the point as JSON, for logs and debugging.
func (p *Point) Json() string {
	bytes, _ := json.Marshal(p)
	return string(bytes)
}

// Marshal encodes the point as a ConsensusPointProto protobuf
// message, the format stored in the consensus database.
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

// Unmarshal decodes the point from a ConsensusPointProto protobuf
// message, replacing the receiver's contents.
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

// LeftAppend merges left — the point of the immediately preceding
// range, so left.EndHash must equal p.PrevHash — into p: the range
// is extended back to left.PrevHash and production counts and
// weights are summed per pillar. Compound points are built by
// left-appending the lower points of their range from last to first.
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

// IsEmpty reports whether the point covers no momentums, i.e. its
// range starts and ends at the same hash.
func (p *Point) IsEmpty() bool {
	return p.EndHash == p.PrevHash
}

// NewEmptyPoint returns a point with no pillar details whose range
// starts and ends at proofHash, ready to be extended with LeftAppend
// or filled with per-pillar statistics.
func NewEmptyPoint(proofHash types.Hash) *Point {
	return &Point{
		PrevHash:    proofHash,
		EndHash:     proofHash,
		Pillars:     make(map[string]*ProducerDetail),
		TotalWeight: big.NewInt(0),
	}
}
