# Zenon Node

Reference Golang implementation of the Alphanet - Network of Momentum Phase 0.

## Building from source

Building `znnd` requires both a Go (version 1.16 or later) and a C compiler. You can install them using your favourite
package manager. Once the dependencies are installed, please run:

```shell
make znnd
```

## Running `znnd`

Since this is version `0.0.1`, `znnd` is not configured with a genesis file or default seeders.
Use [znn-controller](https://github.com/zenon-network/znn_controller_dart)
to configure your full node. For more information please consult the [Wiki](https://github.com/zenon-network/znn-wiki).
