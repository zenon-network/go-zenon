# PTLC embedded contract

The PTLC contract is a signature time-locked embedded contract. It stores locked funds, a point/signature type, and a point lock. Before expiration, a valid signature for the stored point lock can unlock funds to the signed destination. At or after expiration, the original locker can reclaim the funds.

This implementation is best understood as a PTLC-compatible signature lock primitive. It verifies ordinary ED25519 or BIP340 signatures. It does not, by itself, specify a full cross-chain adaptor-signature protocol, validate an HTLC-style plaintext preimage, or prove that revealing a signature reveals an adaptor secret. Any higher-level PTLC or swap protocol must document that separately.

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

The PTLC unlock witness is bound to the chain id, PTLC contract address, point type, PTLC id, and destination. That protects against replay and proxy redirection, but it also means the witness is not a portable shared secret. Higher-level swap protocols must pin their own off-chain terms, including destinations and signing transcripts, before treating a funded leg as safely claimable.

## Spork

The PTLC contract is available only after `types.PtlcSpork` is enforced. The PTLC contract map includes the prior HTLC contract map, so existing embedded contracts remain available after PTLC activation.

Operational rollout must activate sporks in chronological order. `PtlcSpork` assumes the prior HTLC and bridge/liquidity sporks have already been activated.

## Testing

The branch includes two PTLC-focused test workflows.

Live RPC testnet suite:

```sh
make testnet-ptlc
```

This resets the dockerized local devnet, waits for the dedicated RPC node, runs `./testnet/ptlc`, writes a human-readable report to `test-results/ptlc/<timestamp>/summary.md`, and then tears the devnet down. The default endpoint is the dedicated RPC node at `http://localhost:35997`.

The live suite includes a two-party swap choreography simulation. Alice and Bob exchange predefined off-chain terms, Alice locks ZNN for Bob, Bob verifies that lock before locking QSR for Alice, Alice unlocks Bob's leg, Bob observes the unlock material, and Bob unlocks Alice's leg. A companion abort test covers the case where Alice funds first, Bob refuses to fund, and Alice reclaims after expiration. These tests model protocol choreography over the embedded signature-lock primitive; they do not turn the embedded contract into a complete adaptor-secret enforcement layer.

Fuzz and adversarial suite:

```sh
make ptlc-fuzz
```

This runs the PTLC unit/adversarial tests and the live Go fuzz targets, then writes a report to `test-results/ptlc-fuzz/<timestamp>/summary.md`. The report lists each test, what it covers, each fuzz target, execution counts, interesting inputs, and raw log locations.

The generated `test-results/` directory is intentionally ignored because logs and summaries include local absolute paths.

## Related docs

- [Signing](SIGNING.md)
- [Security](SECURITY.md)
- [Release notes](RELEASE_NOTES.md)
