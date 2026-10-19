package legacy

import (
	"net"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

// TestStartCleansUpAfterListenFailure pins what a failed Start leaves behind.
// Start opens the discovery socket before it binds the TCP listener, so a
// listen failure used to return with the server marked running and the
// discovery socket open: the run loop that normally closes the discovery
// table on quit was never started, so Stop could clear the marker and close
// quit without releasing the socket, and a second Start refused a server it
// considered running. After a failed Start the discovery port must be free
// again, the server must not be marked running, and a Start on a free
// address must work.
func TestStartCleansUpAfterListenFailure(t *testing.T) {
	// Occupy a TCP port so the server's listener bind fails. The UDP port of
	// the same number is free, so discovery opens first and must be released.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	addr := occupied.Addr().String()

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{
		PrivateKey: key,
		Name:       "start-cleanup-test",
		MaxPeers:   1,
		Discovery:  true,
		ListenAddr: addr,
	}
	if err := srv.Start(); err == nil {
		srv.Stop()
		t.Fatal("Start must fail while the listen address is taken")
	}

	if srv.running.Load() {
		t.Fatal("server is marked running after a failed Start")
	}
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		t.Fatalf("discovery socket still held after a failed Start: %v", err)
	}
	pc.Close()

	// With the cause gone, the same server starts.
	occupied.Close()
	if err := srv.Start(); err != nil {
		t.Fatalf("Start after a failed Start: %v", err)
	}
	srv.Stop()
}
