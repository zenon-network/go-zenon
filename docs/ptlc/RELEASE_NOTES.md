# PTLC release notes

## Operator and integrator framing

This release adds a spork-gated signature time-lock embedded contract that can serve as a PTLC-compatible primitive.

It does not add a complete adaptor-signature PTLC swap protocol, Bitcoin-side adaptor-signature flow, scalar extraction rule, wallet UX, or cross-chain swap implementation. Any release announcement, wallet integration, bridge integration, or swap UI should describe the feature as a signature time-lock primitive until a higher-level protocol is specified and tested.

## Activation note

Activate sporks in chronological order. `PtlcSpork` assumes the prior HTLC and bridge/liquidity sporks have already been activated.

## Signing compatibility

Wallets and SDKs must sign the exact domain-separated message in [Signing](SIGNING.md). Signatures over the old `Hash(id || destination)` format are invalid.

## Point type and destination (zenon-ptlc, 2026-10)

`Create` takes a fourth argument, `destination`, and a third point type,
`PointTypeSecp256k1Point`, opened by the scalar behind the point. `Create`'s ABI id
changes to `22cec8ee`; `Unlock`, `ProxyUnlock` and `Reclaim` are unchanged. The RPC
`getById` returns `amount` as a decimal string and includes `destination`.
