package node

import "net"

// warnRPCExposure logs, once after the RPC servers are started, every live
// listener that reaches beyond the local machine and every wildcard browser
// origin list installed on it, so an operator running a public endpoint has
// made that choice knowingly. It reads the servers' actual state rather than
// the configuration, so it reports exactly what is bound. The shipped
// defaults are silent.
func (node *Node) warnRPCExposure() {
	for _, server := range []*httpServer{node.http, node.ws} {
		addr, cors, origins := server.exposure()
		if addr == nil {
			continue
		}
		public := !addr.IP.IsLoopback()
		if cors != nil {
			if public {
				log.Warn("HTTP-RPC listens on a non-loopback address and is reachable from other hosts", "endpoint", addr)
			}
			if hasWildcard(cors) {
				log.Warn("HTTP-RPC accepts any browser origin", "cors", "*")
			}
		}
		if origins != nil {
			if public {
				log.Warn("WS-RPC listens on a non-loopback address and is reachable from other hosts", "endpoint", addr)
			}
			if hasWildcard(origins) {
				log.Warn("WS-RPC accepts any browser origin", "origins", "*")
			}
		}
	}
}

// exposure returns the bound address (nil when not listening) and, for each
// protocol served on it, its origin allow-list. A protocol that is not
// enabled on this server yields a nil slice; an enabled one with no
// configured origins yields an empty, non-nil slice.
func (h *httpServer) exposure() (addr *net.TCPAddr, cors []string, origins []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listener == nil {
		return nil, nil, nil
	}
	addr, _ = h.listener.Addr().(*net.TCPAddr)
	if addr == nil {
		return nil, nil, nil
	}
	if h.rpcAllowed() {
		cors = append([]string{}, h.httpConfig.CorsAllowedOrigins...)
	}
	if h.wsAllowed() {
		origins = append([]string{}, h.wsConfig.Origins...)
	}
	return addr, cors, origins
}

func hasWildcard(list []string) bool {
	for _, item := range list {
		if item == "*" {
			return true
		}
	}
	return false
}
