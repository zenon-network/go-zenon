# PTLC embedded contract

The PTLC contract is a signature time-locked embedded contract. It stores locked funds, a point/signature type, and a point lock. Before expiration, a valid signature for the stored point lock can unlock funds to the signed destination. At or after expiration, the original locker can reclaim the funds.

This implementation is best understood as a PTLC-compatible lock primitive with three kinds of lock. Two are opened by an ordinary ED25519 or BIP340 signature, and one by the scalar behind a secp256k1 point. The contract does not, by itself, enforce a swap: it does not validate an HTLC-style plaintext preimage, and it cannot tell whether a signature it accepts was completed from an adaptor pre-signature. What it does give a swap is in [Swaps](#swaps) below, and the two protocols built and tested on it are documented in [Security](SECURITY.md). The cross-chain half of a swap, the transaction on the other chain, is outside this repository.

## Contract

Address:

```txt
types.PtlcContract
```

Methods:

```txt
Create(expirationTime, pointType, pointLock, destination)
Unlock(id, signature)
ProxyUnlock(id, destination, signature)
Reclaim(id)
```

Supported point types:

```txt
PointTypeED25519        32-byte key,  opened by a 64-byte signature over the unlock message
PointTypeBIP340         32-byte key,  opened by a 64-byte signature over the unlock message
PointTypeSecp256k1Point 33-byte compressed point T, opened by the 32-byte scalar t with t*G == T
```

The `signature` argument of `Unlock` and `ProxyUnlock` carries the witness: a
signature for the key types, the scalar for the point type. The point type is the
lock Lightning's PTLC design describes; this VM can multiply a point, so it offers
the lock directly instead of through an adaptor signature.

## Destination

`Create` names the only address an unlock may pay. The zero address means any
address the witness binds, and is allowed for the key types only. A point lock must
name a destination: its witness is a bare scalar that binds nothing, and anyone who
read it from the claim's send block could otherwise submit a `ProxyUnlock` to
themselves first. An entry with a fixed destination refuses every unlock to another
address with `ErrPermissionDenied`, whoever submits it. An embedded contract is never a
valid fixed destination.

## Lifecycle

1. A user sends tokens to `types.PtlcContract` with `Create`.
2. The contract stores the creating address, token standard, amount, expiration time, point type, point lock, and destination.
3. Before expiration, a valid signature over the PTLC unlock message releases funds to the signed destination.
4. At or after expiration, only the original locker can reclaim.
5. Unlock and reclaim delete the stored PTLC entry before sending funds.

## Unlock destination

`Unlock` uses the caller address as the destination.

`ProxyUnlock` accepts an explicit destination. Any caller can submit a valid `ProxyUnlock` proof, but the funds still go to the destination covered by the signature. This makes `ProxyUnlock` a bearer-proof flow: possession of a valid signature for `id` and `destination` is enough to submit the unlock transaction.

The contract does not add a consensus restriction on destination class beyond the address encoded in the signed call. Wallets and higher-level protocols should reject zero, embedded, or otherwise unexpected destinations unless that exact destination is intentional for the protocol.

## HTLC comparison

HTLC unlocks prove knowledge of a preimage for a stored hash digest. PTLC unlocks prove possession of a valid signature for a stored public key, or knowledge of the scalar behind a stored point. For the two key types the signature is an ordinary ED25519 or BIP340 signature over the PTLC unlock message; adaptor-signature scalar revelation is not enforced by the embedded contract.

A key-type witness is bound to the chain id, PTLC contract address, point type, PTLC id, and destination. That protects against replay and proxy redirection, but it also means the witness is not a portable shared secret. Higher-level swap protocols must pin their own off-chain terms, including destinations and signing transcripts, before treating a funded leg as safely claimable.

A point-type witness is the closest to an HTLC preimage: the same scalar opens every entry locked to the same point, on this chain or any other that can check `t*G == T`. It binds nothing, which is why such an entry must name its destination.

## Swaps

Two entries make a swap when opening one gives the other party what it needs to open the second. The contract offers two ways to tie them, both tested here on the mock chain and on the devnet:

| | Point locks | Key locks |
|---|---|---|
| Lock | both entries to a point, `T` and `T + d*G` for a tweak `d` the parties agree off chain | each entry to a BIP340 key of its own maker |
| Destination | fixed, required by the contract | fixed, and the party locking second must check that it is |
| What the parties exchange | the point and the tweak | an adaptor pre-signature each way, over each entry's unlock message |
| What the claim publishes | the scalar `t` | an ordinary signature; `t` is recovered only by the party that made the pre-signature |
| What the chain shows | two unrelated points and two unrelated scalars | two keys and two signatures; neither `T` nor `t` |

Three rules hold for both and are the client's to keep, because every call is valid whether or not they are kept ([Security](SECURITY.md)):

1. The party holding the secret locks first, and its entry expires last.
2. The holder does not claim close to the other entry's expiry. A claim answered after it is refused and its witness is public all the same.
3. A key lock used in a swap must name the counterparty as its destination. One that names nobody can be signed back to its own maker.

No key is held by two parties in either swap, and no multi-party signing protocol runs. That is a consequence of the fixed destination: on a chain whose outputs cannot name a payee, the key-lock swap needs a 2-of-2 key.

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

The suite has twelve tests. Six cover the key types as the hardening work left them: create validation, the domain-separated ED25519 unlock, BIP340 proxy destination binding, expiry and reclaim, and the two below. Six cover what was added since:

| Test | What it holds the devnet to |
|---|---|
| `TestPtlcPointLockViaRPC` | a point lock with no destination, or with a lock that is not a compressed point, is not taken by the node; a wrong scalar is refused; the right scalar from an account the entry does not name is refused, direct and proxied; relayed by a third party it pays the named destination |
| `TestPtlcFixedDestinationKeyLockViaRPC` | an entry naming an embedded contract is not taken; a valid signature for an address the entry does not name is refused; the named destination is paid |
| `TestPtlcPointLockTweakedSwapViaRPC` | the swap on two point locks with a tweak, secret holder first and last to expire; the counterparty reads the scalar from the claim's send block |
| `TestPtlcKeySwapAdaptorViaRPC` | the swap on two BIP340 key locks with an adaptor pre-signature each way; an uncompleted pre-signature opens nothing; neither party can sign its own entry back; the secret is extracted from the published signature; neither the secret nor its point is in any block of the swap |
| `TestPtlcKeySwapOpenDestinationLosesViaRPC` | the same swap with the first entry naming nobody: its maker signs it back to herself and claims the other, and the counterparty's completed signature is refused |
| `TestPtlcLateClaimPublishesWitnessViaRPC` | a claim sent after the entry's expiry is refused, its witness is read from the refused call, and the counterparty reclaims its own entry and takes the other with it |

The devnet has two accounts with plasma, so where a test wants a third party to submit a call, the entry's creator stands in.

The suite takes about 38 minutes on a devnet whose momentums come every 10 to 17 seconds, most of it waiting for calls to be answered and for three expiries. Its default timeout is 60 minutes (`TESTNET_GO_TEST_FLAGS`).

The devnet's container names, host ports and subnet are read from the environment, so the suite can run on a host where the defaults are taken; see [docker/devnet](../../docker/devnet/README.md#running-beside-another-devnet).

The live suite includes a two-party swap choreography simulation. Alice and Bob exchange predefined off-chain terms, Alice locks ZNN for Bob, Bob verifies that lock before locking QSR for Alice, Alice unlocks Bob's leg, Bob observes the unlock material, and Bob unlocks Alice's leg. A companion abort test covers the case where Alice funds first, Bob refuses to fund, and Alice reclaims after expiration. These tests model protocol choreography over the embedded signature-lock primitive; they do not turn the embedded contract into a complete adaptor-secret enforcement layer.

Fuzz and adversarial suite:

```sh
make ptlc-fuzz
```

This runs the PTLC unit/adversarial tests and the live Go fuzz targets, then writes a report to `test-results/ptlc-fuzz/<timestamp>/summary.md`. The report lists each test, what it covers, each fuzz target, execution counts, interesting inputs, and raw log locations.

Seven fuzz targets run, each for `PTLC_FUZZ_TIME` (5 seconds by default): the four of the hardening work, `FuzzPtlc_CheckAndVerify` over all three point types, and two for the point type alone. `FuzzPtlcPointScalarWitness` holds that a point lock opens with the canonical scalar behind it and with no other bytes; `FuzzPtlcPointLockEncoding` that a lock is accepted exactly when it is a compressed point on the curve.

The unit and mock-chain filter (`TestPtlc_|FuzzPtlc`) now also covers the point type's scalar and encoding rules, the destination rules at create and in stored state, both swaps in the safe order, both swaps abandoned, the swap in the unsafe order, a late claim on each kind of lock, and the key swap whose first entry names nobody. The key swap runs in all four parity cases of lock key and nonce.

The generated `test-results/` directory is intentionally ignored because logs and summaries include local absolute paths.

### Last recorded run

On 2026-10-09 (UTC), on Windows 11 with Docker Desktop, Go 1.27 on the host and the node built in the image with Go 1.20, at the commit that added this section:

| Suite | Result |
|---|---|
| `go test ./vm/embedded/...` | pass |
| `make ptlc-fuzz` | PASS: 131 unit and mock-chain results (subtests counted), none failing; 7 fuzz targets, between 49,505 and 209,286 executions each in 5 seconds, no failing input |
| `make testnet-ptlc` | PASS: 12 of 12, the opt-in relay diagnostic skipped; 2,249.9 s; devnet at `ZNND_DEVNET_SUBNET=172.30.77` with the RPC node on host port 36997 |

A first run of the live suite on the same host failed its first test on a timeout, not on a contract answer: the devnet's nodes had stopped dialling at one or two peers and a published call was not in a momentum two minutes later. The peer setting described in [docker/devnet](../../docker/devnet/README.md) is the fix, and the run above is with it.

## Related docs

- [Signing](SIGNING.md)
- [Security](SECURITY.md)
- [Release notes](RELEASE_NOTES.md)
