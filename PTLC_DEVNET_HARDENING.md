# PTLC Local Devnet Hardening Changes

This note summarizes the local devnet hardening changes made to improve the reliability of PTLC testing through the dedicated RPC node.

## Summary

The devnet was changed from a mostly bootstrap-dependent topology into a small mesh. The dedicated RPC node now has a stable p2p identity and multiple peer paths to reach block-producing pillars. A new non-pillar observer node was added to participate in p2p relay and sync without producing momentums.

After these changes, the full PTLC test suite passed using the dedicated RPC endpoint as the default.

## Topology Changes

- Added a new non-pillar `observer` service in `docker-compose.yml`.
- The observer container is named `znnd-devnet-observer`.
- The observer uses static IP `172.30.0.14`.
- The observer does not produce momentums; it only participates in p2p relay and sync.
- Added an `observer-data` Docker volume.
- Updated the `rpc` service to depend on `observer` as well as `pillar`, so the observer is part of the local network before RPC starts.

## Keygen And Config Generation

- Added a `relaySpec` concept in `cmd/devnet-keygen/main.go` for non-pillar nodes.
- Defined two relay nodes:
  - `rpc` at `172.30.0.11`
  - `observer` at `172.30.0.14`
- Set both relay nodes to use `MinPeers=2`.
- Updated key generation to create stable `network-private-key` files for both `rpc` and `observer`.
- Updated key generation to build static enode URLs for all devnet nodes, not just pillars.
- Added generic relay config generation for `rpc` and `observer`.

## Peering Changes

- `rpc` now seeds all three pillars plus `observer`.
- `observer` now seeds all three pillars plus `rpc`.
- `pillar2` now seeds `pillar1` and `pillar3`.
- `pillar3` now seeds `pillar1` and `pillar2`.

This gives transactions submitted to the dedicated RPC node multiple paths to reach producing pillars.

## Generated Files

The following generated devnet files were added:

- `docker/devnet/rpc/network-private-key`
- `docker/devnet/observer/config.json`
- `docker/devnet/observer/network-private-key`

## Entrypoint Cleanup

- Updated `docker/devnet/entrypoint.sh` so it treats any `/devnet/<role>` directory as a valid role.
- This allows new roles such as `observer` to work without hard-coding every possible role name in the error message.

## Test Default Change

- The PTLC testnet suite now defaults to the dedicated RPC node:

```sh
make testnet-ptlc
```

- The default RPC endpoint is:

```text
http://localhost:35997
```

- The pillar RPC endpoint is still available as a fallback or comparison target:

```sh
PTLC_TESTNET_RPC=http://localhost:35991 make testnet-ptlc
```

## Test Results

With the hardened topology, the full PTLC test suite passed through the dedicated RPC node:

```text
ok github.com/zenon-network/go-zenon/testnet/ptlc 622.453s
```

The suite writes tester-friendly artifacts under:

```text
test-results/ptlc/<timestamp>/
```

Each run includes:

- `go-test.log`: full verbose Go test output.
- `summary.md`: suite status, RPC endpoint, each test case, what it covers, duration, and package result.

## Why This Helps

Before this change, the dedicated RPC node mostly depended on one bootstrap path through `pillar1`. That made transaction propagation occasionally slow or flaky.

After this change, the dedicated RPC node has:

- a stable p2p identity,
- multiple pillar seed peers,
- an additional non-pillar observer relay peer,
- and better cross-peering between producing pillars.

In plain English: the local devnet now behaves more like a small real network. Transactions submitted to the RPC node have more than one route to reach producers, and the RPC node has more than one route to stay synced.
