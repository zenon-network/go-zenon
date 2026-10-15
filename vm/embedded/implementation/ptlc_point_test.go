package implementation

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"

	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

var secp256k1Order = []byte{
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xfe,
	0xba, 0xae, 0xdc, 0xe6, 0xaf, 0x48, 0xa0, 0x3b, 0xbf, 0xd2, 0x5e, 0x8c, 0xd0, 0x36, 0x41, 0x41,
}

func pointScalar(k uint32) []byte {
	var s btcec.ModNScalar
	s.SetInt(k)
	b := s.Bytes()
	return b[:]
}

func pointAmount() *big.Int { return big.NewInt(1) }

func offCurveX(t *testing.T) []byte {
	t.Helper()
	for x := byte(1); x != 0; x++ {
		lock := make([]byte, 33)
		lock[0], lock[32] = 0x02, x
		if _, err := btcec.ParsePubKey(lock); err != nil {
			return lock
		}
	}
	t.Fatal("no small x off the curve")
	return nil
}

func pointOf(scalar []byte) []byte {
	var s btcec.ModNScalar
	s.SetByteSlice(scalar)
	var p btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&s, &p)
	p.ToAffine()
	return btcec.NewPublicKey(&p.X, &p.Y).SerializeCompressed()
}

func TestPtlc_PointScalarRules(t *testing.T) {
	secret := pointScalar(123456789)
	lock := pointOf(secret)

	var neg btcec.ModNScalar
	neg.SetByteSlice(secret)
	neg.Negate()
	negBytes := neg.Bytes()

	plusOrder := make([]byte, 32)
	carry := 0
	for i := 31; i >= 0; i-- {
		sum := int(secret[i]) + int(secp256k1Order[i]) + carry
		plusOrder[i] = byte(sum)
		carry = sum >> 8
	}
	if carry != 0 {
		t.Fatal("test scalar too large to add the order to")
	}

	for name, c := range map[string]struct {
		witness []byte
		want    error
	}{
		"the scalar":                   {secret, nil},
		"another scalar":               {pointScalar(123456788), constants.ErrInvalidPointScalar},
		"its negation":                 {negBytes[:], constants.ErrInvalidPointScalar},
		"zero":                         {make([]byte, 32), constants.ErrInvalidPointScalar},
		"the group order":              {secp256k1Order, constants.ErrInvalidPointScalar},
		"the scalar plus the order":    {plusOrder, constants.ErrInvalidPointScalar},
		"31 bytes":                     {secret[1:], constants.ErrInvalidPointScalar},
		"33 bytes":                     {append([]byte{0}, secret...), constants.ErrInvalidPointScalar},
		"nothing":                      {nil, constants.ErrInvalidPointScalar},
		"a 64-byte signature's length": {bytes.Repeat([]byte{1}, 64), constants.ErrInvalidPointScalar},
	} {
		if err := verifyPointScalar(lock, c.witness); err != c.want {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}

	info := &definition.PtlcInfo{
		Id: types.NewHash([]byte("point")), TimeLocked: User1.Address, TokenStandard: types.ZnnTokenStandard,
		Amount: pointAmount(), ExpirationTime: 1000000000,
		PointType: definition.PointTypeSecp256k1Point, PointLock: lock, Destination: User1.Address,
	}
	for _, chain := range []uint64{1, 69} {
		for _, destination := range []types.Address{User1.Address, types.PlasmaContract} {
			if err := verifyPtlcWitness(info, chain, types.NewHash([]byte("other")), destination, secret); err != nil {
				t.Errorf("chain %d destination %s: the scalar was refused: %v", chain, destination, err)
			}
		}
	}
	if err := verifyPtlcWitness(info, 1, info.Id, User1.Address, bytes.Repeat([]byte{1}, 64)); err != constants.ErrInvalidPointScalar {
		t.Errorf("a signature-sized witness on a point lock: got %v, want ErrInvalidPointScalar", err)
	}
}

func TestPtlc_PointLockEncoding(t *testing.T) {
	lock := pointOf(pointScalar(7))
	pub, err := btcec.ParsePubKey(lock)
	if err != nil {
		t.Fatal(err)
	}
	otherPrefix := append([]byte{}, lock...)
	otherPrefix[0] ^= 1
	hybrid := append([]byte{}, lock...)
	hybrid[0] = 0x06

	offCurve := offCurveX(t)

	for name, c := range map[string]struct {
		lock []byte
		want error
	}{
		"compressed":                    {lock, nil},
		"the same x, the other y":       {otherPrefix, nil},
		"uncompressed, 65 bytes":        {pub.SerializeUncompressed(), constants.ErrInvalidPointLock},
		"x only, 32 bytes":              {lock[1:], constants.ErrInvalidPointLock},
		"33 bytes with prefix 04":       {append([]byte{0x04}, lock[1:]...), constants.ErrInvalidPointLock},
		"33 bytes with a hybrid prefix": {hybrid, constants.ErrInvalidPointLock},
		"33 bytes with prefix 00":       {append([]byte{0x00}, lock[1:]...), constants.ErrInvalidPointLock},
		"an x not on the curve":         {offCurve, constants.ErrInvalidPointLock},
		"an x at the field prime":       {append([]byte{0x02}, bytes.Repeat([]byte{0xff}, 32)...), constants.ErrInvalidPointLock},
		"nothing":                       {nil, constants.ErrInvalidPointLock},
	} {
		if err := checkPointLock(definition.PointTypeSecp256k1Point, c.lock); err != c.want {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}
}

func TestPtlc_DestinationRules(t *testing.T) {
	point := pointOf(pointScalar(7))
	bip340 := point[1:]
	for name, c := range map[string]struct {
		pointType   uint8
		lock        []byte
		destination types.Address
		want        error
	}{
		"point lock, no destination":                      {definition.PointTypeSecp256k1Point, point, types.ZeroAddress, constants.ErrInvalidDestination},
		"point lock, an account":                          {definition.PointTypeSecp256k1Point, point, User1.Address, nil},
		"point lock, an embedded contract":                {definition.PointTypeSecp256k1Point, point, types.PtlcContract, constants.ErrInvalidDestination},
		"BIP340 lock, no destination":                     {definition.PointTypeBIP340, bip340, types.ZeroAddress, nil},
		"BIP340 lock, an account":                         {definition.PointTypeBIP340, bip340, User1.Address, nil},
		"BIP340 lock, an embedded contract":               {definition.PointTypeBIP340, bip340, types.HtlcContract, constants.ErrInvalidDestination},
		"ED25519 lock, no destination":                    {definition.PointTypeED25519, User1.Public, types.ZeroAddress, nil},
		"ED25519 lock, an account":                        {definition.PointTypeED25519, User1.Public, User1.Address, nil},
		"ED25519 lock, an embedded contract":              {definition.PointTypeED25519, User1.Public, types.PlasmaContract, constants.ErrInvalidDestination},
		"BIP340 lock, an unregistered contract":           {definition.PointTypeBIP340, bip340, types.Address{types.ContractAddrByte, 1}, constants.ErrInvalidDestination},
		"point lock, the first reserved type":             {definition.PointTypeSecp256k1Point, point, types.Address{types.ContractAddrByte + 1, 1}, constants.ErrInvalidDestination},
		"point lock, the last reserved type":              {definition.PointTypeSecp256k1Point, point, types.Address{0xff, 1}, constants.ErrInvalidDestination},
		"BIP340 lock, the first reserved type":            {definition.PointTypeBIP340, bip340, types.Address{types.ContractAddrByte + 1, 1}, constants.ErrInvalidDestination},
		"BIP340 lock, the last reserved type":             {definition.PointTypeBIP340, bip340, types.Address{0xff, 1}, constants.ErrInvalidDestination},
		"ED25519 lock, the first reserved type":           {definition.PointTypeED25519, User1.Public, types.Address{types.ContractAddrByte + 1, 1}, constants.ErrInvalidDestination},
		"ED25519 lock, the last reserved type":            {definition.PointTypeED25519, User1.Public, types.Address{0xff, 1}, constants.ErrInvalidDestination},
		"a bad lock is reported before a bad destination": {definition.PointTypeSecp256k1Point, point[1:], types.ZeroAddress, constants.ErrInvalidPointLock},
	} {
		err := checkPtlc(definition.CreatePtlcParam{ExpirationTime: 1000000000, PointType: c.pointType, PointLock: c.lock, Destination: c.destination})
		if err != c.want {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
		stored := checkStoredPtlcInfo(&definition.PtlcInfo{
			Amount: pointAmount(), ExpirationTime: 1000000000,
			PointType: c.pointType, PointLock: c.lock, Destination: c.destination,
		})
		if stored != c.want {
			t.Errorf("%s, as stored state: got %v, want %v", name, stored, c.want)
		}
	}
}

func FuzzPtlcPointScalarWitness(f *testing.F) {
	secret := pointScalar(987654321)
	lock := pointOf(secret)
	f.Add(secret)
	f.Add(make([]byte, 32))
	f.Add(secp256k1Order)
	f.Add(bytes.Repeat([]byte{0xff}, 32))
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{1}, 64))

	f.Fuzz(func(t *testing.T, witness []byte) {
		err := verifyPointScalar(lock, witness)
		switch {
		case bytes.Equal(witness, secret):
			if err != nil {
				t.Fatalf("the scalar was refused: %v", err)
			}
		case err == nil:
			t.Fatalf("%x opened a lock it is not the scalar of", witness)
		case err != constants.ErrInvalidPointScalar:
			t.Fatalf("unexpected error %v", err)
		}
	})
}

func FuzzPtlcPointLockEncoding(f *testing.F) {
	f.Add(pointOf(pointScalar(7)))
	f.Add(append([]byte{0x04}, bytes.Repeat([]byte{1}, 32)...))
	f.Add(append([]byte{0x02}, bytes.Repeat([]byte{0xff}, 32)...))
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{2}, 65))

	f.Fuzz(func(t *testing.T, lock []byte) {
		err := checkPointLock(definition.PointTypeSecp256k1Point, lock)
		pub, parseErr := btcec.ParsePubKey(lock)
		canonical := parseErr == nil && len(lock) == 33 && bytes.Equal(pub.SerializeCompressed(), lock)
		switch {
		case canonical && err != nil:
			t.Fatalf("a compressed point was refused: %x: %v", lock, err)
		case !canonical && err == nil:
			t.Fatalf("accepted a lock that is not a compressed point: %x", lock)
		case err != nil && err != constants.ErrInvalidPointLock:
			t.Fatalf("unexpected error %v", err)
		}
	})
}
