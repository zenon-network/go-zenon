# PTLC security notes

This contract moves funds based on stored consensus state and signature verification. Treat malformed storage, malformed signatures, and cross-context replay risk as consensus-security issues.

## Domain separation

Unlock signatures are domain-separated with a purpose/version string, chain identifier, contract address, point type, PTLC id, and destination.

This prevents a signature intended for one PTLC context from accidentally verifying on another Zenon chain, in another contract, or for another point type. The destination is signed so `ProxyUnlock` cannot redirect funds.

## Stored state validation

Loaded PTLC state is validated before reclaim or unlock logic uses it:

- known point type
- point-lock length for the point type
- positive amount
- positive expiration time

Consensus code must not rely only on the public `Create` path when funds are about to move. If stored state is malformed, the contract rejects the operation.

## Stable errors

The contract returns PTLC-level errors instead of raw crypto-library errors:

- unknown point type: `ErrInvalidPointType`
- malformed point lock: `ErrInvalidPointLock`
- malformed or invalid signature: `ErrInvalidPointSignature`

Stable errors keep consensus behavior independent of dependency error strings and wrapping details.

## Storage keys

PTLC storage keys must be exactly one prefix byte plus a hash. Empty, short, long, or wrong-prefix keys are rejected before parsing.

## ProxyUnlock bearer-proof semantics

`ProxyUnlock` is intentionally caller-agnostic. Anyone who has a valid signature for `id` and `destination` can submit it. This is safe only because the destination is included in the signed message.

Wallets and higher-level protocols should treat a `ProxyUnlock` signature as a bearer proof for that specific destination.

## Limitations

This implementation verifies ordinary ED25519 and BIP340 signatures. It does not specify adaptor signatures, scalar extraction, or a complete cross-chain PTLC swap protocol.

Protocols that rely on adaptor-signature properties must document:

- what secret is revealed
- how the public point relates to the secret scalar
- how signatures on another chain are completed or adapted
- which chain/network contexts are bound outside this contract

## Dependency review

BIP340 support requires `github.com/btcsuite/btcd/btcec/v2/schnorr`. Avoid unrelated dependency upgrades in PTLC changes, and review `go.mod` and `go.sum` separately from contract logic.
