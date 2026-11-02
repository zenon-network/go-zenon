package node

import (
	"fmt"
	"net"
	"strings"

	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

// rpcExposureWarning is one thing an operator running an RPC endpoint should
// know about: the message, and the listener or list it refers to.
type rpcExposureWarning struct {
	msg      string
	key, val string
}

// String renders the warning as one line for the console.
func (w rpcExposureWarning) String() string {
	return fmt.Sprintf("%s (%s %s)", w.msg, w.key, w.val)
}

// rpcExposureWarnings lists, for every live RPC listener, what reaches beyond
// the local machine and which browser checks are disabled: one entry per
// protocol per non-loopback bind, one per origin list that admits browser
// origins on any host, and one for a wildcard HTTP virtual-host list. It reads the servers' actual state rather
// than the configuration, so it reports exactly what is bound. The shipped
// defaults produce none.
func (node *Node) rpcExposureWarnings() []rpcExposureWarning {
	var warnings []rpcExposureWarning
	for _, server := range []*httpServer{node.http, node.ws} {
		e := server.exposure()
		if e.addr == nil {
			continue
		}
		public := !e.addr.IP.IsLoopback()
		if e.httpOn {
			if public {
				warnings = append(warnings, rpcExposureWarning{"HTTP-RPC listens on a non-loopback address and is reachable from other hosts", "endpoint", e.addr.String()})
			}
			if hasWildcard(e.cors) {
				warnings = append(warnings, rpcExposureWarning{"HTTP-RPC accepts any browser origin", "cors", "*"})
			} else if pattern, ok := firstMatch(e.cors, corsPatternWildcardsHost); ok {
				warnings = append(warnings, rpcExposureWarning{"HTTP-RPC accepts browser origins on every host a wildcard CORS pattern matches", "cors", pattern})
			}
			if hasWildcard(e.vhosts) {
				warnings = append(warnings, rpcExposureWarning{"HTTP-RPC answers any Host header, so a page that rebinds a DNS name to this address is same-origin with it", "vhosts", "*"})
			}
		}
		if e.wsOn {
			if public {
				warnings = append(warnings, rpcExposureWarning{"WS-RPC listens on a non-loopback address and is reachable from other hosts", "endpoint", e.addr.String()})
			}
			if hasWildcard(e.origins) {
				warnings = append(warnings, rpcExposureWarning{"WS-RPC accepts any browser origin", "origins", "*"})
			} else if rule, ok := firstMatch(e.origins, rpc.OriginRuleAdmitsAnyHost); ok {
				warnings = append(warnings, rpcExposureWarning{"WS-RPC accepts browser origins on any host: the origin rule names no host", "origins", rule})
			}
		}
	}
	return warnings
}

// warnRPCExposure logs every exposure warning once, after the RPC servers
// are started, so an operator running a public endpoint has made that choice
// knowingly. The same lines are available to the console through
// RPCExposureWarnings.
func (node *Node) warnRPCExposure() {
	for _, w := range node.rpcExposureWarnings() {
		log.Warn(w.msg, w.key, w.val)
	}
}

// RPCExposureWarnings returns the exposure warnings as console lines, in the
// order they are logged, for the startup status output. The log file is not
// the console, and an operator watching the terminal should see them too.
func (node *Node) RPCExposureWarnings() []string {
	warnings := node.rpcExposureWarnings()
	lines := make([]string, 0, len(warnings))
	for _, w := range warnings {
		lines = append(lines, w.String())
	}
	return lines
}

// listenerExposure is what one server exposes: its bound address, which
// protocols it serves, and the browser allow-lists installed for them.
type listenerExposure struct {
	addr         *net.TCPAddr
	httpOn, wsOn bool
	cors, vhosts []string
	origins      []string
}

// exposure reads the server's live state under its lock. addr is nil when the
// server is not listening; httpOn and wsOn say which protocols are served on
// it, and the lists are copies of the installed configuration for those.
func (h *httpServer) exposure() listenerExposure {
	h.mu.Lock()
	defer h.mu.Unlock()
	var e listenerExposure
	if h.listener == nil {
		return e
	}
	e.addr, _ = h.listener.Addr().(*net.TCPAddr)
	if e.addr == nil {
		return e
	}
	if h.rpcAllowed() {
		e.httpOn = true
		e.cors = append([]string{}, h.httpConfig.CorsAllowedOrigins...)
		e.vhosts = append([]string{}, h.httpConfig.Vhosts...)
	}
	if h.wsAllowed() {
		e.wsOn = true
		e.origins = append([]string{}, h.wsConfig.Origins...)
	}
	return e
}

func hasWildcard(list []string) bool {
	for _, item := range list {
		if item == "*" {
			return true
		}
	}
	return false
}

// firstMatch returns the first item in list for which match is true.
func firstMatch(list []string, match func(string) bool) (string, bool) {
	for _, item := range list {
		if match(item) {
			return item, true
		}
	}
	return "", false
}

// corsPatternWildcardsHost reports whether a CORS origin pattern, as the
// rs/cors middleware matches it, leaves any part of the origin's host to
// the wildcard. The middleware treats "*" as allow-all and splits any other
// pattern at its first '*' into a prefix and a suffix that the origin must
// start and end with, so "http://*", "http://localhost*" and
// "https://*.com" all match hosts of the sender's choosing. A subdomain
// pattern ("https://*.wallet.example") is reported too: whether every host
// under that domain is the operator's is not something the pattern shows.
// Only a wildcard confined to the scheme ("*://wallet.example") or to the
// port ("http://wallet.example:*") leaves the host literal.
func corsPatternWildcardsHost(pattern string) bool {
	pattern = strings.ToLower(pattern)
	if pattern == "*" {
		return true
	}
	i := strings.IndexByte(pattern, '*')
	if i < 0 {
		return false
	}
	prefix, suffix := pattern[:i], pattern[i+1:]
	if strings.Contains(suffix, "://") {
		return false // in the scheme: a literal host follows
	}
	j := strings.Index(prefix, "://")
	if j < 0 {
		return true
	}
	host := prefix[j+3:]
	if k := strings.LastIndexByte(host, ']'); k >= 0 {
		host = host[k+1:] // past a bracketed IPv6 literal
	} else if strings.HasPrefix(host, "[") {
		return true // inside an IPv6 literal
	}
	return !strings.Contains(host, ":") // after the host's ':' it is the port
}
