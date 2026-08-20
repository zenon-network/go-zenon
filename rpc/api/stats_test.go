package api

import "testing"

type fakeAddr struct {
	network string
	addr    string
}

func (a fakeAddr) Network() string { return a.network }
func (a fakeAddr) String() string  { return a.addr }

func TestRemoteHost(t *testing.T) {
	cases := []struct {
		name string
		addr fakeAddr
		want string
	}{
		{
			name: "legacy IPv4 host:port",
			addr: fakeAddr{network: "tcp", addr: "192.168.1.10:35995"},
			want: "192.168.1.10",
		},
		{
			name: "legacy IPv6 host:port",
			addr: fakeAddr{network: "tcp", addr: "[2001:db8::1]:35995"},
			want: "2001:db8::1",
		},
		{
			name: "legacy address with no port",
			addr: fakeAddr{network: "tcp", addr: "192.168.1.10"},
			want: "192.168.1.10",
		},
		{
			name: "libp2p ip4 multiaddr",
			addr: fakeAddr{network: "libp2p", addr: "/ip4/203.0.113.5/tcp/35995"},
			want: "203.0.113.5",
		},
		{
			name: "libp2p ip6 multiaddr",
			addr: fakeAddr{network: "libp2p", addr: "/ip6/2001:db8::1/tcp/35995"},
			want: "2001:db8::1",
		},
		{
			name: "libp2p multiaddr with no recognized address component",
			addr: fakeAddr{network: "libp2p", addr: "/tcp/35995"},
			want: "/tcp/35995",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := remoteHost(tc.addr)
			if got != tc.want {
				t.Errorf("remoteHost(%q) = %q, want %q", tc.addr.String(), got, tc.want)
			}
		})
	}
}
