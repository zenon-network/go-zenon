package protocol_test

import (
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/hex"
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// Minimal Ed25519 group arithmetic over math/big, following the RFC 8032
// reference construction. It exists only so this test can mint a second valid
// signature for one message without adding a dependency; the result is
// checked with crypto/ed25519.Verify before it is used.
var (
	ed25519P = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))
	ed25519L = func() *big.Int {
		l, _ := new(big.Int).SetString("7237005577332262213973186563042994240857116359379907606001950938285454250989", 10)
		return l
	}()
	ed25519D = func() *big.Int {
		inv := new(big.Int).ModInverse(big.NewInt(121666), ed25519P)
		d := new(big.Int).Mul(big.NewInt(-121665), inv)
		return d.Mod(d, ed25519P)
	}()
	ed25519B = func() *edPoint {
		y := new(big.Int).Mul(big.NewInt(4), new(big.Int).ModInverse(big.NewInt(5), ed25519P))
		y.Mod(y, ed25519P)
		return &edPoint{x: edRecoverX(y, 0), y: y}
	}()
)

type edPoint struct{ x, y *big.Int }

func edMod(v *big.Int) *big.Int { return new(big.Int).Mod(v, ed25519P) }

// edRecoverX returns the x coordinate with the given parity for a y coordinate
// on the curve, using the exponentiation from RFC 8032 section 5.1.3.
func edRecoverX(y *big.Int, sign uint) *big.Int {
	yy := edMod(new(big.Int).Mul(y, y))
	u := edMod(new(big.Int).Sub(yy, big.NewInt(1)))
	v := edMod(new(big.Int).Add(new(big.Int).Mul(ed25519D, yy), big.NewInt(1)))
	xx := edMod(new(big.Int).Mul(u, new(big.Int).ModInverse(v, ed25519P)))
	exp := new(big.Int).Rsh(new(big.Int).Add(ed25519P, big.NewInt(3)), 3)
	x := new(big.Int).Exp(xx, exp, ed25519P)
	if edMod(new(big.Int).Mul(x, x)).Cmp(xx) != 0 {
		sqrtM1 := new(big.Int).Exp(big.NewInt(2), new(big.Int).Rsh(new(big.Int).Sub(ed25519P, big.NewInt(1)), 2), ed25519P)
		x = edMod(new(big.Int).Mul(x, sqrtM1))
	}
	if x.Bit(0) != sign {
		x = edMod(new(big.Int).Neg(x))
	}
	return x
}

func edAdd(p, q *edPoint) *edPoint {
	x1x2 := new(big.Int).Mul(p.x, q.x)
	y1y2 := new(big.Int).Mul(p.y, q.y)
	x1y2 := new(big.Int).Mul(p.x, q.y)
	x2y1 := new(big.Int).Mul(q.x, p.y)
	dxxyy := edMod(new(big.Int).Mul(ed25519D, new(big.Int).Mul(x1x2, y1y2)))
	x := new(big.Int).Mul(new(big.Int).Add(x1y2, x2y1), new(big.Int).ModInverse(edMod(new(big.Int).Add(big.NewInt(1), dxxyy)), ed25519P))
	y := new(big.Int).Mul(new(big.Int).Add(y1y2, x1x2), new(big.Int).ModInverse(edMod(new(big.Int).Sub(big.NewInt(1), dxxyy)), ed25519P))
	return &edPoint{x: edMod(x), y: edMod(y)}
}

func edScalarMult(scalar *big.Int, p *edPoint) *edPoint {
	result := &edPoint{x: big.NewInt(0), y: big.NewInt(1)}
	for i := scalar.BitLen() - 1; i >= 0; i-- {
		result = edAdd(result, result)
		if scalar.Bit(i) == 1 {
			result = edAdd(result, p)
		}
	}
	return result
}

func edEncode(p *edPoint) []byte {
	out := make([]byte, 32)
	p.y.FillBytes(out)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	out[31] |= byte(p.x.Bit(0)) << 7
	return out
}

func edLittleEndian(b []byte) *big.Int {
	reversed := make([]byte, len(b))
	for i := range b {
		reversed[len(b)-1-i] = b[i]
	}
	return new(big.Int).SetBytes(reversed)
}

func edEncodeScalar(v *big.Int) []byte {
	out := make([]byte, 32)
	new(big.Int).Mod(v, ed25519L).FillBytes(out)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// alternateSign produces a valid Ed25519 signature for message that differs from
// the deterministic RFC 8032 signature, by deriving the nonce from an extra
// domain string. Both signatures verify under the same public key.
func alternateSign(keyPair *wallet.KeyPair, message []byte) []byte {
	digest := sha512.Sum512(keyPair.Private.Seed())
	clamped := make([]byte, 32)
	copy(clamped, digest[:32])
	clamped[0] &= 248
	clamped[31] &= 127
	clamped[31] |= 64
	secret := edLittleEndian(clamped)
	prefix := digest[32:]

	nonceHash := sha512.New()
	nonceHash.Write([]byte("alternate-nonce"))
	nonceHash.Write(prefix)
	nonceHash.Write(message)
	nonce := new(big.Int).Mod(edLittleEndian(nonceHash.Sum(nil)), ed25519L)
	commitment := edEncode(edScalarMult(nonce, ed25519B))

	challengeHash := sha512.New()
	challengeHash.Write(commitment)
	challengeHash.Write(keyPair.Public)
	challengeHash.Write(message)
	challenge := new(big.Int).Mod(edLittleEndian(challengeHash.Sum(nil)), ed25519L)

	response := new(big.Int).Add(nonce, new(big.Int).Mul(challenge, secret))
	return append(commitment, edEncodeScalar(response)...)
}

func TestInsertChain_MomentumBytesReplacePoolCopyOfSameBlock(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	bridge := protocol.NewChainBridge(z.Chain(), z.Consensus(), z.Verifier(), supervisor)

	// User1 sends one raw ZNN unit to User2 and the send gets committed.
	send := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(1),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	// User2 receives it. This copy, with the deterministic signature, is the one
	// the momentum is built from.
	momentumCopy, err := supervisor.GenerateFromTemplate(&nom.AccountBlock{
		BlockType:     nom.BlockTypeUserReceive,
		Address:       g.User2.Address,
		FromBlockHash: send.Hash,
	}, g.User2.Signer)
	common.FailIfErr(t, err)
	z.Broadcaster().CreateAccountBlock(momentumCopy)
	z.InsertNewMomentum()

	store := z.Chain().GetFrontierMomentumStore()
	momentum, err := store.GetFrontierMomentum()
	common.FailIfErr(t, err)
	detailed, err := store.PrefetchMomentum(momentum)
	common.FailIfErr(t, err)
	common.Expect(t, len(detailed.AccountBlocks), 1)
	common.Expect(t, detailed.AccountBlocks[0].Identifier(), momentumCopy.Block.Identifier())

	// Roll back so the momentum can be replayed against a pool that holds a
	// byte-different copy of the same block.
	insert := z.Chain().AcquireInsert("test rollback")
	common.FailIfErr(t, z.Chain().RollbackTo(insert, momentum.Previous()))
	insert.Unlock()

	poolCopy := momentumCopy.Block.Copy()
	poolCopy.Signature = alternateSign(g.User2, poolCopy.Hash.Bytes())
	if !ed25519.Verify(g.User2.Public, poolCopy.Hash.Bytes(), poolCopy.Signature) {
		t.Fatal("alternate signature does not verify")
	}
	if string(poolCopy.Signature) == string(momentumCopy.Block.Signature) {
		t.Fatal("alternate signature is identical to the deterministic one")
	}
	common.FailIfErr(t, bridge.AddAccountBlocks([]*nom.AccountBlock{poolCopy}))

	_, err = bridge.InsertChain([]*nom.DetailedMomentum{detailed})
	common.FailIfErr(t, err)

	frontier := z.Chain().GetFrontierMomentumStore()
	common.Expect(t, frontier.Identifier(), momentum.Identifier())
	committed, err := frontier.GetAccountBlock(momentumCopy.Block.Header())
	common.FailIfErr(t, err)
	common.ExpectBytes(t, committed.Signature, "0x"+hex.EncodeToString(momentumCopy.Block.Signature))
}
