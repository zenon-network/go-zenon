package legacy

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/ecies"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"

	"github.com/zenon-network/go-zenon/p2p/discover"
)

// TestImportPublicKeyRejectsInvalidPoint pins that a key of the right length
// whose bytes do not decode to a point on the curve is reported as an error
// instead of being wrapped into a key with unset coordinates.
func TestImportPublicKeyRejectsInvalidPoint(t *testing.T) {
	for _, length := range []int{64, 65} {
		if _, err := importPublicKey(make([]byte, length)); err == nil {
			t.Fatalf("importPublicKey accepted a %d-byte input that is not a point on the curve", length)
		}
	}
}

// TestImportPublicKeyAcceptsValidPoint is the control: a real key round-trips.
func TestImportPublicKeyAcceptsValidPoint(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	full := crypto.FromECDSAPub(&key.PublicKey)
	for name, encoded := range map[string][]byte{"64-byte": full[1:], "65-byte": full} {
		pub, err := importPublicKey(encoded)
		if err != nil {
			t.Fatalf("importPublicKey rejected a valid %s key: %v", name, err)
		}
		if pub.X.Cmp(key.PublicKey.X) != 0 || pub.Y.Cmp(key.PublicKey.Y) != 0 {
			t.Fatalf("importPublicKey changed the %s key", name)
		}
	}
}

// TestEncHandshakeBothRolesAgree runs the production initiator and receiver
// against each other over a pipe and checks that they derive the same
// session secrets, so the stricter key import does not reject a valid peer
// on either side.
func TestEncHandshakeBothRolesAgree(t *testing.T) {
	initKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	respKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	initConn, respConn := net.Pipe()
	defer initConn.Close()
	defer respConn.Close()

	type result struct {
		s   secrets
		err error
	}
	initDone := make(chan result, 1)
	respDone := make(chan result, 1)
	go func() {
		s, err := initiatorEncHandshake(initConn, initKey, discover.PubkeyID(&respKey.PublicKey), nil)
		initDone <- result{s, err}
	}()
	go func() {
		s, err := receiverEncHandshake(respConn, respKey, nil)
		respDone <- result{s, err}
	}()

	wait := func(ch chan result, role string) secrets {
		select {
		case r := <-ch:
			if r.err != nil {
				t.Fatalf("%s handshake failed: %v", role, r.err)
			}
			return r.s
		case <-time.After(5 * time.Second):
			t.Fatalf("%s handshake did not return", role)
		}
		return secrets{}
	}
	is := wait(initDone, "initiator")
	rs := wait(respDone, "receiver")
	if !bytes.Equal(is.AES, rs.AES) || !bytes.Equal(is.MAC, rs.MAC) || !bytes.Equal(is.Token, rs.Token) {
		t.Fatal("initiator and receiver derived different session secrets")
	}
	if rs.RemoteID != discover.PubkeyID(&initKey.PublicKey) {
		t.Fatal("receiver recorded the wrong remote ID")
	}
	if is.RemoteID != discover.PubkeyID(&respKey.PublicKey) {
		t.Fatal("initiator recorded the wrong remote ID")
	}
	// Each side's egress MAC state must be the other side's ingress state.
	if !bytes.Equal(is.EgressMAC.Sum(nil), rs.IngressMAC.Sum(nil)) || !bytes.Equal(rs.EgressMAC.Sum(nil), is.IngressMAC.Sum(nil)) {
		t.Fatal("initiator and receiver MAC states are not mirrored")
	}
}

// TestInitiatorHandshakeRejectsInvalidResponderKey drives the dialing side of
// the encryption handshake against a responder whose authentication response
// carries an ephemeral key that is not a point on the curve. The handshake
// must end with an error from the response decoding, not a runtime panic.
func TestInitiatorHandshakeRejectsInvalidResponderKey(t *testing.T) {
	initKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	respKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	respID := discover.PubkeyID(&respKey.PublicKey)

	initConn, respConn := net.Pipe()
	defer initConn.Close()
	defer respConn.Close()

	responderErr := make(chan error, 1)
	go func() {
		defer respConn.Close()
		// Consume the initiator's auth message; its content does not matter
		// for this test because the response is built from the initiator's
		// static key, which the responder is assumed to know.
		if _, err := io.ReadFull(respConn, make([]byte, encAuthMsgLen)); err != nil {
			responderErr <- err
			return
		}
		// ephemeral-pubkey (unset coordinates) || nonce || token-flag
		resp := make([]byte, authRespLen)
		if _, err := rand.Read(resp[pubLen : pubLen+shaLen]); err != nil {
			responderErr <- err
			return
		}
		enc, err := ecies.Encrypt(rand.Reader, ecies.ImportECDSAPublic(&initKey.PublicKey), resp, nil, nil)
		if err != nil {
			responderErr <- err
			return
		}
		_, err = respConn.Write(enc)
		responderErr <- err
	}()

	done := make(chan error, 1)
	go func() {
		_, err := initiatorEncHandshake(initConn, initKey, respID, nil)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("initiator handshake succeeded with an invalid responder key")
		}
		if !strings.Contains(err.Error(), "not a point on the curve") {
			t.Fatalf("initiator handshake failed for another reason: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("initiator handshake did not return")
	}
	select {
	case err := <-responderErr:
		if err != nil {
			t.Fatalf("test responder failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("test responder did not finish")
	}
}

// TestECIESRejectsInvalidAndOutOfRangePoints verifies GO-2026-4511 /
// CVE-2026-26315 and GO-2026-4507 / CVE-2026-26314 across p2p/legacy's ECIES
// and RLPx handshake entry points:
//   - ecies.PrivateKey.GenerateShared directly exercises the CVE-2026-26315 /
//     CVE-2026-26314 fixes (rejecting off-curve, nil-coordinate, and
//     non-canonical x >= P / y >= P points without panicking).
//   - decodeAuthMsg with a validly ECIES-encrypted auth message carrying a
//     non-canonical remoteID (x >= P or y >= P) exercises the end-to-end RLPx
//     receiver path (remoteID.Pubkey -> ecdhShared -> GenerateShared).
//   - Decrypt with malformed ephemeral headers acts as a regression guard
//     ensuring invalid points continue to return ecies.ErrInvalidPublicKey.
func TestECIESRejectsInvalidAndOutOfRangePoints(t *testing.T) {
	prv, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	eciesPrv := ecies.ImportECDSA(prv)
	curve := secp256k1.S256()
	p := curve.Params().P

	// Canonical small-coordinate points on secp256k1 where adding P still fits
	// in 32 bytes.
	smallX, _ := new(big.Int).SetString("1", 16)
	smallXY, _ := new(big.Int).SetString("4218f20ae6c646b363db68605822fb14264ca8d2587fdd6fbc750d587e76a7ee", 16)
	smallYX, _ := new(big.Int).SetString("1fe1e5ef3fceb5c135ab7741333ce5a6e80d68167653f6b2b24bcbcfaaaff507", 16)
	smallY, _ := new(big.Int).SetString("1", 16)

	if !curve.IsOnCurve(smallX, smallXY) || !curve.IsOnCurve(smallYX, smallY) {
		t.Fatal("test fixture points are not on secp256k1")
	}

	// Control: valid ECIES round-trip and GenerateShared succeed.
	plain := []byte("valid-ecies-payload")
	validCT, err := ecies.Encrypt(rand.Reader, &eciesPrv.PublicKey, plain, nil, nil)
	if err != nil {
		t.Fatalf("ecies.Encrypt failed: %v", err)
	}
	gotPlain, err := Decrypt(prv, validCT)
	if err != nil || !bytes.Equal(gotPlain, plain) {
		t.Fatalf("Decrypt control failed: err=%v, match=%v", err, bytes.Equal(gotPlain, plain))
	}
	if _, err := eciesPrv.GenerateShared(&ecies.PublicKey{Curve: curve, X: smallX, Y: smallXY}, sskLen, sskLen); err != nil {
		t.Fatalf("GenerateShared control failed: %v", err)
	}

	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	invalidPoints := []struct {
		name string
		x, y *big.Int
	}{
		{"zero point (0, 0)", big.NewInt(0), big.NewInt(0)},
		{"off-curve (1, 2)", big.NewInt(1), big.NewInt(2)},
		{"non-canonical x = canonicalX + P", new(big.Int).Add(smallX, p), smallXY},
		{"non-canonical y = canonicalY + P", smallYX, new(big.Int).Add(smallY, p)},
		{"x == P", new(big.Int).Set(p), smallXY},
		{"y == P", smallYX, new(big.Int).Set(p)},
		{"x == 2^256-1, y == 2^256-1", maxUint256, maxUint256},
	}

	for _, tc := range invalidPoints {
		t.Run("GenerateShared/"+tc.name, func(t *testing.T) {
			pub := &ecies.PublicKey{Curve: curve, X: tc.x, Y: tc.y}
			if _, err := eciesPrv.GenerateShared(pub, sskLen, sskLen); !errors.Is(err, ecies.ErrInvalidPublicKey) {
				t.Fatalf("GenerateShared(%s): got err=%v, want %v", tc.name, err, ecies.ErrInvalidPublicKey)
			}
		})

		t.Run("Decrypt/"+tc.name, func(t *testing.T) {
			ct := make([]byte, encAuthMsgLen)
			ct[0] = 0x04
			tc.x.FillBytes(ct[1:33])
			tc.y.FillBytes(ct[33:65])
			if _, err := Decrypt(prv, ct); !errors.Is(err, ecies.ErrInvalidPublicKey) {
				t.Fatalf("Decrypt(%s): got err=%v, want %v", tc.name, err, ecies.ErrInvalidPublicKey)
			}
			if _, err := decodeAuthMsg(prv, nil, ct); err == nil {
				t.Fatalf("decodeAuthMsg(%s): unexpectedly succeeded", tc.name)
			}
		})
	}

	t.Run("GenerateShared/nil coordinates", func(t *testing.T) {
		for name, pub := range map[string]*ecies.PublicKey{
			"both nil": {Curve: curve, X: nil, Y: nil},
			"x nil":    {Curve: curve, X: nil, Y: smallXY},
			"y nil":    {Curve: curve, X: smallX, Y: nil},
		} {
			if _, err := eciesPrv.GenerateShared(pub, sskLen, sskLen); !errors.Is(err, ecies.ErrInvalidPublicKey) {
				t.Fatalf("GenerateShared(%s): got err=%v, want %v", name, err, ecies.ErrInvalidPublicKey)
			}
		}
	})

	t.Run("Decrypt/non-uncompressed prefix", func(t *testing.T) {
		for _, prefix := range []byte{0x00, 0x02, 0x03, 0x05} {
			ct := make([]byte, encAuthMsgLen)
			ct[0] = prefix
			if _, err := Decrypt(prv, ct); !errors.Is(err, ecies.ErrInvalidPublicKey) {
				t.Fatalf("Decrypt(prefix=0x%02x): got err=%v, want %v", prefix, err, ecies.ErrInvalidPublicKey)
			}
		}
	})

	// End-to-end RLPx receiver test: validly ECIES-encrypt an auth plaintext
	// whose initiator pubkey (remoteID) carries a non-canonical coordinate
	// (x = canonicalX + P or y = canonicalY + P). decodeAuthMsg must reject it
	// at remoteID.Pubkey() before reaching ecdhShared/GenerateShared.
	for _, tc := range []struct {
		name string
		x, y *big.Int
	}{
		{"non-canonical x = canonicalX + P", new(big.Int).Add(smallX, p), smallXY},
		{"non-canonical y = canonicalY + P", smallYX, new(big.Int).Add(smallY, p)},
	} {
		t.Run("decodeAuthMsg/valid-ecies-non-canonical-remoteID/"+tc.name, func(t *testing.T) {
			plainAuth := make([]byte, authMsgLen)
			tc.x.FillBytes(plainAuth[sigLen+shaLen : sigLen+shaLen+32])
			tc.y.FillBytes(plainAuth[sigLen+shaLen+32 : sigLen+shaLen+pubLen])
			encAuth, err := ecies.Encrypt(rand.Reader, &eciesPrv.PublicKey, plainAuth, nil, nil)
			if err != nil {
				t.Fatalf("ecies.Encrypt failed: %v", err)
			}
			_, err = decodeAuthMsg(prv, nil, encAuth)
			if err == nil {
				t.Fatalf("decodeAuthMsg accepted non-canonical remoteID (%s)", tc.name)
			}
			if !strings.Contains(err.Error(), "bad remoteID") {
				t.Fatalf("decodeAuthMsg failed for unexpected reason (%s): %v", tc.name, err)
			}
		})
	}
}
