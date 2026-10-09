# PTLC signing

PTLC unlock signatures must be generated over the domain-separated unlock message. Signatures over the old bare `Hash(id || destination)` message are invalid.

## Unlock message

The message is:

```go
crypto.Hash(common.JoinBytes(
    []byte("zenon-ptlc-unlock:v1"),
    common.Uint64ToBytes(chainIdentifier),
    types.PtlcContract.Bytes(),
    []byte{pointType},
    id.Bytes(),
    destination.Bytes(),
))
```

Fields:

- `zenon-ptlc-unlock:v1`: purpose and version string.
- `chainIdentifier`: Zenon chain identifier from the frontier momentum.
- `types.PtlcContract`: embedded contract address.
- `pointType`: stored signature scheme.
- `id`: hash of the PTLC create block.
- `destination`: address that receives funds if the signature verifies.

The chain identifier is consensus state, not a wallet preference. A signature produced for another Zenon chain id is invalid on this chain.

Wire encoding before hashing:

| Offset | Size | Field | Encoding |
| ---: | ---: | --- | --- |
| 0 | 20 | domain | ASCII bytes for `zenon-ptlc-unlock:v1` |
| 20 | 8 | chain identifier | unsigned 64-bit big-endian integer from `common.Uint64ToBytes` |
| 28 | 20 | contract address | raw `types.PtlcContract.Bytes()` |
| 48 | 1 | point type | single unsigned byte |
| 49 | 32 | PTLC id | raw `types.Hash.Bytes()` of the create block hash |
| 81 | 20 | destination | raw `types.Address.Bytes()` |

The hash preimage is exactly 101 bytes. The unlock message is `crypto.Hash`, Zenon's SHA3-256 helper, over that preimage.

## ED25519 mode

`PointTypeED25519` stores a 32-byte ED25519 public key and expects a 64-byte ED25519 signature over the unlock message.

Use this mode when the point lock is a Zenon wallet ED25519 public key. `Unlock` signs the caller destination; `ProxyUnlock` signs the explicit destination.

## BIP340 mode

`PointTypeBIP340` stores a 32-byte BIP340 x-only public key and expects a 64-byte Schnorr signature over the unlock message.

Malformed BIP340 signatures are rejected with `ErrInvalidPointSignature`. Malformed BIP340 public keys are rejected with `ErrInvalidPointLock`.

`Create` validates that a BIP340 point lock is a parseable x-only public key before storing the PTLC. This avoids creating a BIP340 lock that can never be unlocked and can only be reclaimed after expiration.

## Proxy unlock

For `ProxyUnlock`, the caller and destination can differ. The signature must still be valid for the destination argument:

```txt
signature = Sign(pointLockPrivateKey, PTLCUnlockMessage(chainIdentifier, pointType, id, destination))
```

A valid signature for destination A cannot unlock funds to destination B.

## Point mode

`PointTypeSecp256k1Point` stores a 33-byte compressed secp256k1 point `T` and expects
the 32-byte scalar `t` with `t·G = T` as the witness. The scalar must be canonical:
nonzero and below the group order, so that one secret has exactly one encoding. No
message is signed; the entry's fixed destination does the binding that the unlock
message does for the key types.

## Adaptor pre-signatures over the unlock message

A swap on key locks ([Security](SECURITY.md#a-swap-on-key-locks-needs-the-fixed-destination))
uses a BIP340 signature that is made in two steps. The contract sees only the
finished signature and checks it as any other; the two steps are the client's.

With the lock's secret key `d` (negated if its public key has odd y, as BIP340 signs),
a nonce `k`, the adaptor point `T` and `m` the unlock message of the entry:

```txt
R  = k*G + T                      negate k if R has odd y
e  = tagged_hash("BIP0340/challenge", x(R) || x(P) || m)
s' = k + e*d                      the pre-signature is (R compressed, s'): 65 bytes
s  = s' + t   (s' - t if R has odd y)     the signature is (x(R), s): 64 bytes
t  = s - s'   (s' - s if R has odd y)     what the maker of s' learns from s
```

The receiver of a pre-signature checks `s'*G == (R - T) + e*P` (`(T - R) + e*P` for
odd `R`) before relying on it. `m` names the entry and the destination, so a
pre-signature can be made only after the entry exists, and is good for one entry
and one address. The nonce must not be used for two different challenges; deriving
it from the key, `m`, `T` and fresh randomness ensures that.

The construction is the Schnorr adaptor signature of Blockstream's scriptless-scripts
notes (`md/atomic-swap.md`), with BIP340's even-y rule applied to `R`. Test
implementations are in `vm/embedded/tests/ptlc_keyswap_test.go` and
`testnet/ptlc/ptlc_point_test.go`; neither is part of the node.
