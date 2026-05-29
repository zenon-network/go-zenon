# PTLC security notes

This contract moves funds based on stored consensus state and signature verification. Treat malformed storage, malformed signatures, and cross-context replay risk as consensus-security issues.

## Domain separation

Unlock signatures are domain-separated with a purpose/version string, chain identifier, contract address, point type, PTLC id, and destination.

This prevents a signature intended for one PTLC context from accidentally verifying on another Zenon chain, in another contract, or for another point type. The destination is signed so `ProxyUnlock` cannot redirect funds.

The same binding also means an unlock witness is not portable swap material. A signature observed for one destination or PTLC id cannot be reused for another destination or PTLC id, and it is not equivalent to an HTLC preimage.

## Stored state validation

Loaded PTLC state is fully validated before unlock logic uses it:

- known point type
- point-lock length for the point type
- parseable BIP340 x-only public key when `PointTypeBIP340` is used
- positive amount
- positive expiration time

Consensus code must not rely only on the public `Create` path when funds are about to move. If stored state is malformed, unlock rejects the operation.

Reclaim intentionally validates only reclaim-relevant fields: the entry must exist, carry a positive amount, carry a positive expiration time, belong to the caller, and be expired. Point type and point lock are not needed to return expired funds to the original locker, so malformed point fields do not turn an otherwise reclaimable entry into a permanent lock.

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

Consensus does not distinguish user, embedded, or zero-like destination intent for `ProxyUnlock`; it verifies the signature over exactly the submitted destination. Wallets should apply stricter destination policy before asking users to sign.

## Logging

PTLC logs record signature hashes, not full submitted signatures. Unlock signatures are public witnesses after submission, but logging only hashes reduces accidental witness reuse in off-chain tooling.

## Limitations

This implementation verifies ordinary ED25519 and BIP340 signatures. It does not specify adaptor signatures, scalar extraction, or a complete cross-chain PTLC swap protocol.

In particular, the on-chain witness is `Sign(pointPrivateKey, PTLCUnlockMessage(chainIdentifier, pointType, id, destination))`. It is destination-bound, PTLC-id-bound, contract-bound, and chain-bound. Higher-level swap protocols must not treat it as a shared plaintext preimage like an HTLC witness. If an adaptor-signature protocol expects a fixed destination or fixed signing transcript, wallets must pin those exact fields before signing; a fresh ordinary signature over a different destination should be treated as a protocol abort, not as reusable secret revelation.

Protocols that rely on adaptor-signature properties must document:

- what secret is revealed
- how the public point relates to the secret scalar
- how signatures on another chain are completed or adapted
- which chain/network contexts are bound outside this contract

## Dependency review

BIP340 support requires `github.com/btcsuite/btcd/btcec/v2/schnorr`. This dependency is consensus-critical after PTLC activation, so upgrades require dedicated BIP340 vector review. Avoid unrelated dependency upgrades in PTLC changes, and review `go.mod` and `go.sum` separately from contract logic.
