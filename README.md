# Zenon Node

Reference Golang implementation of the Alphanet - Network of Momentum Phase 0.

## Building from source

Building `znnd` requires both a Go (version 1.16 or later) and a C compiler. You can install them using your favourite package manager. Once the dependencies are installed, please run:

```shell
make znnd
```

## Running `znnd`

Since version `0.0.2`, `znnd` is configured with the Alphanet Genesis and default seeders.

`--host` sets the P2P listen host (`Net.ListenHost`) and does not configure RPC; use `--http-addr` for HTTP JSON-RPC and `--ws-addr` for WebSocket RPC. On earlier versions `--host X` set the HTTP-RPC host instead: replace that use with `--http-addr X`, and pass `--host X` only when P2P should bind to `X`.

Use [znn-controller](https://github.com/zenon-network/znn_controller_dart) to configure your full node. For more information please consult the [Wiki](https://github.com/zenon-network/znn-wiki).

By default the HTTP (`35997`) and WebSocket (`35998`) JSON-RPC servers listen on `127.0.0.1` only. The HTTP server sends no CORS headers, so browsers do not let pages from other origins read its responses. The WebSocket server accepts connections that carry no `Origin` header (wallets, CLI tools, SDKs) and browser pages served from `http://localhost` or `http://<machine hostname>` on any port; it rejects every other origin, including `http://127.0.0.1:<port>` and `https://localhost`. Neither check is authentication: any process that can connect to the port can call the API. To serve other hosts, set `RPC.HTTPHost` / `RPC.WSHost` in `config.json` (or pass `--http-addr` / `--ws-addr`), and list the browser origins that need access in `RPC.HTTPCors` / `RPC.WSOrigins`. At startup `znnd` prints a warning in its status output and logs one with the same content to `<data>/log/zenon.log`, per protocol on each listener bound to a non-loopback address, per wildcard origin list, and when `RPC.HTTPVirtualHosts` contains `*` (a page that rebinds a DNS name to the node's address is then same-origin with it); put a firewall or a proxy in front of a public endpoint.

Each RPC listener caps the TCP connections it holds open at once: `RPC.MaxHTTPConnections` for the HTTP listener and `RPC.MaxWSConnections` for the WebSocket listener, 1024 each by default. In `config.json` an absent key or `0` selects the default and a negative value removes the cap. At the cap the listener stops accepting and new connections wait in the operating system's accept queue until one closes; that queue is bounded too, and a client that waits longer than its own timeout gives up. The caps bound the file descriptors the RPC servers take from the rest of the node, provided the process's descriptor limit leaves room for both caps on top of what the database and p2p use. An established WebSocket connection stays open for as long as its client keeps it, so a node that serves more than 1024 WebSocket clients at once needs a higher `RPC.MaxWSConnections` (and a matching file descriptor limit). When both protocols are configured on one port, `RPC.MaxHTTPConnections` bounds that listener, WebSocket connections included, and `RPC.MaxWSConnections` is not used. Each cap is shared by all clients. `RPC.MaxWSConnectionsPerIP` (off by default) limits the WebSocket connections of one remote address and refuses the excess with HTTP 429; a node that clients reach directly should set it, for example to `8`, because without it one address can keep enough WebSocket connections open to fill the cap or the server-wide subscription limit. It uses the address of the TCP peer and does not read forwarded headers: behind a reverse proxy every client has the proxy's address and the value would limit all clients together, so leave it off there and limit connections per client at the proxy.

## Local devnet

This branch includes a dockerized five-node local devnet with three
producing pillars, one public RPC ingress node, and one non-pillar observer
node for relay/sync coverage.

```shell
make devnet-up      # start the local docker devnet
make devnet-down    # stop and wipe local devnet state
make devnet-keys    # regenerate committed devnet configs/keys
```

The public endpoints are `http://localhost:35997` for HTTP JSON-RPC and
`ws://localhost:35998` for WebSocket RPC. Pillar 1 also exposes HTTP RPC at
`http://localhost:35991` for producer-path debugging. The local explorer is
served at `http://localhost:36000` and is preconfigured to use the public
devnet RPC endpoint.

More detail: [docker/devnet/README.md](docker/devnet/README.md).
