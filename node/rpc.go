package node

// configureRPC is a helper method to configure all the various RPC endpoints during node
// startup. It's not meant to be called at any time afterwards as it makes certain
// assumptions about the state of the node.
func (node *Node) startRPC() error {
	// A protocol runs only when its enable flag is set and a host is
	// configured: the flag is the policy, the host says where to listen.
	httpEnabled := node.config.RPC.EnableHTTP && node.config.RPC.HTTPHost != ""
	wsEnabled := node.config.RPC.EnableWS && node.config.RPC.WSHost != ""
	if !node.config.RPC.EnableHTTP && node.config.RPC.HTTPHost != "" {
		log.Info("HTTP-RPC server disabled by configuration", "ignored-host", node.config.RPC.HTTPHost)
	}
	if !node.config.RPC.EnableWS && node.config.RPC.WSHost != "" {
		log.Info("WS-RPC server disabled by configuration", "ignored-host", node.config.RPC.WSHost)
	}

	// Configure HTTP.
	if httpEnabled {
		config := httpConfig{
			CorsAllowedOrigins: node.config.RPC.HTTPCors,
			Vhosts:             node.config.RPC.HTTPVirtualHosts,
			Modules:            node.config.RPC.Endpoints,
			prefix:             "",
		}
		if err := node.http.setListenAddr(node.config.RPC.HTTPHost, node.config.RPC.HTTPPort); err != nil {
			return err
		}
		if err := node.http.enableRPC(node.rpcAPIs, config); err != nil {
			return err
		}
	}

	// Configure WebSocket.
	if wsEnabled {
		server := node.wsServerForPort(httpEnabled, node.config.RPC.WSPort)
		config := wsConfig{
			Modules: node.config.RPC.Endpoints,
			Origins: node.config.RPC.WSOrigins,
			prefix:  "",
		}
		if err := server.setListenAddr(node.config.RPC.WSHost, node.config.RPC.WSPort); err != nil {
			return err
		}
		if err := server.enableWS(node.rpcAPIs, config); err != nil {
			return err
		}
	}

	if err := node.http.start(); err != nil {
		return err
	}
	return node.ws.start()
}

// wsServerForPort returns the server WebSocket should be enabled on: the HTTP
// server when it is enabled and listens on the same port, otherwise the
// dedicated WebSocket server.
func (node *Node) wsServerForPort(httpEnabled bool, port int) *httpServer {
	if httpEnabled && node.http.port == port {
		return node.http
	}
	return node.ws
}

func (node *Node) stopRPC() {
	node.http.stop()
	node.ws.stop()
}
