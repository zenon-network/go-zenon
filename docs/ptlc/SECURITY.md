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

## Point locks and the fixed destination

A scalar is a bearer secret. A send block that carries it is public one momentum
before the contract acts on it, so a point lock without a fixed destination would be
claimable by whoever reads the scalar first. `Create` therefore refuses a point lock
with the zero destination, and an unlock of an entry with a fixed destination pays
only that address. Key locks may use the same restriction; with the zero destination
the signature's destination binding is the only protection, as before.

The scalar check is one fixed-base scalar multiplication per unlock, charged as
`EmbeddedWWithdraw` like every other unlock.

## A refused unlock still publishes its witness

The contract judges an unlock when it receives it, against the timestamp of the
momentum it runs in. The caller's send block is on the chain, with its witness in
the clear, before that: a call sent just before `expirationTime` and received just
after it is refused with `ErrExpired`, and its witness is public all the same. For a
point lock the witness is the scalar itself; for a key lock made from an adaptor
pre-signature it gives the adaptor secret to whoever holds the pre-signature. Either
way the counterparty can reclaim its own entry at its expiry and use the secret on the
other leg. A client must not send an unlock with less than a few momentums left on
the chain's clock; the tooling in zenon-ptlc refuses with under 60 seconds.

## Who locks first in a swap

Two entries that one secret opens are a swap only if the party holding the secret
locks first and its entry expires last. The other party locks once that entry is on
the chain, with an expiry earlier by at least the time it needs to claim after the
secret appears. In the other order the holder can claim the counterparty's entry
without ever funding its own, or wait out its own entry, take it back, and claim the
counterparty's afterwards. The contract cannot tell the two orders apart: each call
is valid in both. `TestPtlc_pointLock_swapOrder_wrongOrderLosesBob` and
`TestPtlc_pointLock_swapOrder_lateRevealCostsTheRevealer` in
`vm/embedded/tests/ptlc_point_test.go` run both cases on the mock chain.

## ED25519 locks are not decoded

`Create` checks an ED25519 lock for its length and nothing else, and an unlock is
verified with Go's `crypto/ed25519`, the function that checks account blocks. That
function accepts a public key of small order, and under such a key a signature needs
no secret: with the identity as the key (`01` and 31 zero bytes), the signature `01`
and 63 zero bytes verifies for every message, so anyone opens the entry, to any
destination. Thirty-two bytes that are no point make an entry no signature opens; its
creator reclaims it at the expiry. The two secp256k1 types are decoded at `Create` and
have neither case.

This is left as it is on purpose. The contract takes the keys the chain's own accounts
take, as Zcash's ZIP-215 does, and adds no rule of its own; libsodium, ed25519-dalek's
`verify_strict` and Oasis Core refuse such keys. The check is the client's: one that
makes an ED25519 lock, or relies on one somebody else made, must decode the key and
refuse it when it is no point or when its eightfold is the identity. The tooling in
zenon-ptlc does (`ptlc.create` in the CLI, `lockProblem` in the lab's console), and its
scenarios page runs the forgery as `weak-key`.

## A swap on key locks needs the fixed destination

Two BIP340 entries make a swap with single-signer adaptor signatures, and no key held
by two parties, when each entry names its counterparty as its destination. This is the
protocol the Limitations section above asks a higher layer to document:

- The secret is a scalar `t`, held by one party, with the point `T = t*G` known to
  both. Neither is ever on this chain.
- Each party locks to a key of its own, made for the swap, with the other party as the
  entry's destination. Each gives the other an adaptor pre-signature on `T` by that key
  over its own entry's unlock message: the nonce is `R = k*G + T` and the scalar is
  `s' = k + e*d`, so `s' + t` is a BIP340 signature (`s' - t` when `R` has odd y).
- The holder of `t` completes the counterparty's pre-signature and unlocks with it. The
  counterparty made that pre-signature, so the published signature gives it
  `t = s - s'`, with which it completes the other one.
- Both unlock messages are this contract's, so the chain, contract, entry and
  destination bindings above apply to both legs.

The destination is what makes one key enough. A lock's key can sign at any time, but
with a fixed destination every signature it makes pays the counterparty. With the zero
destination the same key signs the entry back to its owner, after the owner has
claimed the other leg or before: `Create` accepts such an entry, so the party locking
second must read the first entry and refuse one that does not name it.

The order and the timing rules of the two sections above hold unchanged: the holder
of `t` locks first and expires last, and a claim refused as too late still publishes
the completed signature, which gives the counterparty `t`. `ptlc_keyswap_test.go` in
`vm/embedded/tests` runs the swap in the four parity cases of key and nonce, both
ways of abandoning it, the late claim, and the swap without the fixed destination.
