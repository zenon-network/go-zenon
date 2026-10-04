package node

import (
	"fmt"
	"net"
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
// protocol per non-loopback bind, one per wildcard origin list, and one for a
// wildcard HTTP virtual-host list. It reads the servers' actual state rather
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
