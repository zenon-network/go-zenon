# PTLC release notes

## Operator and integrator framing

This release adds a spork-gated signature time-lock embedded contract that can serve as a PTLC-compatible primitive.

It does not add a complete adaptor-signature PTLC swap protocol, Bitcoin-side adaptor-signature flow, scalar extraction rule, wallet UX, or cross-chain swap implementation. Any release announcement, wallet integration, bridge integration, or swap UI should describe the feature as a signature time-lock primitive until a higher-level protocol is specified and tested.

## Activation note

Activate sporks in chronological order. `PtlcSpork` assumes the prior HTLC and bridge/liquidity sporks have already been activated.

## Signing compatibility

Wallets and SDKs must sign the exact domain-separated message in [Signing](SIGNING.md). Signatures over the old `Hash(id || destination)` format are invalid.
