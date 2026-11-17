package legacy

import (
	"bytes"
	"crypto/rand"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/ecies"

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
