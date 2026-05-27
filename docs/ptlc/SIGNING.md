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

## Proxy unlock

For `ProxyUnlock`, the caller and destination can differ. The signature must still be valid for the destination argument:

```txt
signature = Sign(pointLockPrivateKey, PTLCUnlockMessage(chainIdentifier, pointType, id, destination))
```

A valid signature for destination A cannot unlock funds to destination B.
