# PTLC signing

PTLC unlock signatures must be generated over the domain-separated unlock message. Signatures over the old bare `Hash(id || destination)` message are invalid.

## Unlock message

The message is:

```go
crypto.Hash(common.JoinBytes(
    []byte("zenon-ptlc-unlock:v1"),
    types.PtlcContract.Bytes(),
    []byte{pointType},
    id.Bytes(),
    destination.Bytes(),
))
```

Fields:

- `zenon-ptlc-unlock:v1`: purpose and version string.
- `types.PtlcContract`: embedded contract address.
- `pointType`: stored signature scheme.
- `id`: hash of the PTLC create block.
- `destination`: address that receives funds if the signature verifies.

If a consensus-visible network or chain id is added later, introduce a new signing version and bind that id in the message.

## ED25519 mode

`PointTypeED25519` stores a 32-byte ED25519 public key and expects a 64-byte ED25519 signature over the unlock message.

Use this mode when the point lock is a Zenon wallet ED25519 public key. `Unlock` signs the caller destination; `ProxyUnlock` signs the explicit destination.

## BIP340 mode

`PointTypeBIP340` stores a 32-byte BIP340 x-only public key and expects a 64-byte Schnorr signature over the unlock message.

Malformed BIP340 signatures are rejected with `ErrInvalidPointSignature`. Malformed BIP340 public keys are rejected with `ErrInvalidPointLock`.

## Proxy unlock

For `ProxyUnlock`, the caller and destination can differ. The signature must still be valid for the destination argument:

```txt
signature = Sign(pointLockPrivateKey, PTLCUnlockMessage(pointType, id, destination))
```

A valid signature for destination A cannot unlock funds to destination B.
