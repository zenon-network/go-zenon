# PTLC release notes

## Operator and integrator framing

This release adds a spork-gated signature time-lock embedded contract that can serve as a PTLC-compatible primitive.

It does not add a Bitcoin-side adaptor-signature flow, wallet UX, or a cross-chain swap implementation, and the contract enforces no swap protocol. Since the additions below, two swap protocols between two entries of this contract are specified in [Security](SECURITY.md) and tested on the mock chain and the devnet. They are client protocols: the contract accepts every call of a swap done wrongly as readily as one done right. A release announcement, wallet integration, bridge integration, or swap UI should describe the contract as a time-lock primitive and name the client rules a swap depends on.

## Activation note

Activate sporks in chronological order. `PtlcSpork` assumes the prior HTLC and bridge/liquidity sporks have already been activated.

## Signing compatibility

Wallets and SDKs must sign the exact domain-separated message in [Signing](SIGNING.md). Signatures over the old `Hash(id || destination)` format are invalid.

## Point type and destination (zenon-ptlc, 2026-10)

`Create` takes a fourth argument, `destination`, and a third point type,
`PointTypeSecp256k1Point`, opened by the scalar behind the point. `Create`'s ABI id
changes to `22cec8ee`; `Unlock`, `ProxyUnlock` and `Reclaim` are unchanged. The RPC
`getById` returns `amount` as a decimal string and includes `destination`.

New errors: `ErrInvalidPointScalar` for a point-lock witness that is not the canonical
scalar behind the point, and `ErrInvalidDestination` for a point lock that names no
destination or any entry that names an embedded contract. An entry with a fixed
destination answers an unlock to any other address with `ErrPermissionDenied`.

This is the only change to contract code since the hardening commits. Everything
listed below is tests, tooling and documentation.

## Swaps, and what a client must do (2026-10)

Two swaps between two entries are specified and tested: one on point locks with a
tweak, one on BIP340 key locks with adaptor pre-signatures and nothing of the secret
on the chain. Neither needs a key held by two parties. See the table in the
[README](README.md#swaps) and the sections of [Security](SECURITY.md) it points to.

Integrators building either swap must implement four checks that the contract does
not make:

- The party holding the secret locks first and its entry expires last.
- The holder does not send a claim near the other entry's expiry; a refused claim's
  witness is public. The tooling this was developed with refuses under 60 seconds.
- The party locking second reads the first entry from the chain and refuses a key
  lock that does not name it as destination.
- A client that makes or relies on an ED25519 lock decodes the key and refuses one
  that is not a point or is of small order. `Create` does not.

## History and credit

| When | Who | What |
|---|---|---|
| 2023-05 | georgezgeorgez | The contract: `Create`, `Unlock`, `ProxyUnlock`, `Reclaim`, the ED25519 and BIP340 point types, the spork, the RPC, and the first test suite (zenon-network/go-zenon PR #13) |
| 2026-05 | 0x3639 | The hardening: fail-closed unlock, the domain-separated unlock message, stored-state validation, BIP340 keys checked at `Create`, stable errors, the adversarial and fuzz tests, the dockerized devnet and the live RPC suite, and these documents |
| 2026-10 | so1_sanctum | The secp256k1 point type and the fixed destination; the two swap protocols and their tests; the security notes on swap order, late claims and ED25519 keys; unit, fuzz and live tests for all of it |

The commits of each are kept as theirs. PR #13's two commits were ported from their
2023 base onto master, and each of the nine hardening commits follows them with the
files it had in `0x3639/go-zenon`, branch `codex/ptlc-testnet-local`, so the first of
the nine shows the hardening over PR #13 rather than the whole contract. Every ported
commit names the commit it came from.

## Devnet names, ports and subnet (2026-10)

`docker-compose.yml` reads the devnet's container-name prefix, image, subnet and host
ports from the environment, with the previous values as defaults, so that the live
suite can run on a host where `znnd-devnet-*`, ports 35997 and 35998 or
`172.30.0.0/24` are in use. See [docker/devnet](../../docker/devnet/README.md).
