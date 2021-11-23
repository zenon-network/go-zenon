package node

// configureRPC is a helper method to configure all the various RPC endpoints during node
// startup. It's not meant to be called at any time afterwards as it makes certain
// assumptions about the state of the node.
func (node *Node) startRPC() error {
	// Configure HTTP.
	if node.config.RPC.HTTPHost != "" {
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
	if node.config.RPC.WSHost != "" {
		server := node.wsServerForPort(node.config.RPC.WSPort)
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

func (node *Node) wsServerForPort(port int) *httpServer {
	if node.config.RPC.HTTPHost == "" || node.http.port == port {
		return node.http
	}
	return node.ws
}

func (node *Node) stopRPC() {
	node.http.stop()
	node.ws.stop()
}
