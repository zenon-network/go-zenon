# PTLC embedded contract

The PTLC contract is a signature time-locked embedded contract. It stores locked funds, a point/signature type, and a point lock. Before expiration, a valid signature for the stored point lock can unlock funds to the signed destination. At or after expiration, the original locker can reclaim the funds.

This implementation is best understood as a PTLC-compatible signature lock primitive. It verifies ordinary ED25519 or BIP340 signatures. It does not, by itself, specify a full cross-chain adaptor-signature protocol or prove that revealing a signature reveals an adaptor secret. Any higher-level PTLC or swap protocol must document that separately.

## Contract

Address:

```txt
types.PtlcContract
```

Methods:

```txt
Create(expirationTime, pointType, pointLock)
Unlock(id, signature)
ProxyUnlock(id, destination, signature)
Reclaim(id)
```

Supported point types:

```txt
PointTypeED25519
PointTypeBIP340
```

## Lifecycle

1. A user sends tokens to `types.PtlcContract` with `Create`.
2. The contract stores the creating address, token standard, amount, expiration time, point type, and point lock.
3. Before expiration, a valid signature over the PTLC unlock message releases funds to the signed destination.
4. At or after expiration, only the original locker can reclaim.
5. Unlock and reclaim delete the stored PTLC entry before sending funds.

## Unlock destination

`Unlock` uses the caller address as the destination.

`ProxyUnlock` accepts an explicit destination. Any caller can submit a valid `ProxyUnlock` proof, but the funds still go to the destination covered by the signature. This makes `ProxyUnlock` a bearer-proof flow: possession of a valid signature for `id` and `destination` is enough to submit the unlock transaction.

The contract does not add a consensus restriction on destination class beyond the address encoded in the signed call. Wallets and higher-level protocols should reject zero, embedded, or otherwise unexpected destinations unless that exact destination is intentional for the protocol.

## HTLC comparison

HTLC unlocks prove knowledge of a preimage for a stored hash digest. PTLC unlocks prove possession of a valid signature for a stored public key/point lock. In this implementation, the signature is an ordinary ED25519 or BIP340 signature over the PTLC unlock message; adaptor-signature scalar revelation is not enforced by the embedded contract.

## Spork

The PTLC contract is available only after `types.PtlcSpork` is enforced. The PTLC contract map includes the prior HTLC contract map, so existing embedded contracts remain available after PTLC activation.

Operational rollout must activate sporks in chronological order. `PtlcSpork` assumes the prior HTLC and bridge/liquidity sporks have already been activated.

## Related docs

- [Signing](SIGNING.md)
- [Security](SECURITY.md)
