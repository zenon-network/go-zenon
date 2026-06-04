# Zenon Node

Reference Golang implementation of the Alphanet - Network of Momentum Phase 0.

## Building from source

Building `znnd` requires both a Go (version 1.16 or later) and a C compiler. You can install them using your favourite package manager. Once the dependencies are installed, please run:

```shell
make znnd
```

## Running `znnd`

Since version `0.0.2`, `znnd` is configured with the Alphanet Genesis and default seeders.

Use [znn-controller](https://github.com/zenon-network/znn_controller_dart) to configure your full node. For more information please consult the [Wiki](https://github.com/zenon-network/znn-wiki).

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
