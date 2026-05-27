# PR 13 PTLC audit

Audit target: `zenon-network/go-zenon#13`

PR head reviewed: `8ed1ca1e012a2c7a2e9ecc456fb82bdef75a4a18` (`Format codebase`)

Local audit worktree: `/private/tmp/go-zenon-pr13`

Base used for diff: local `upstream/master`, merge-base `58eaa81439a39197dd9080230580fb8b47bcd323`

## Verdict

This PR is a real implementation and it is not merge-ready.

The PR adds an actual embedded PTLC-like contract, ABI, RPC endpoint, spork gate, and a meaningful test suite. But the cryptographic contract is underspecified and currently behaves more like a signature time-locked contract than a fully specified PTLC/adaptor-signature primitive.

Recommended merge status: block until crypto/spec hardening, stable error handling, storage parser safety, documentation, dependency cleanup, and test additions are completed.

## What the PR adds

The PR adds `types.PtlcContract` and a new embedded contract with:

- `Create(expirationTime, pointType, pointLock)`
- `Unlock(id, signature)`
- `ProxyUnlock(id, destination, signature)`
- `Reclaim(id)`

It supports:

- `PointTypeED25519`
- `PointTypeBIP340`

The intended flow is:

1. A sender locks tokens in `PtlcContract`.
2. Contract storage records sender, token, amount, expiration time, point type, and point lock.
3. Before expiration, a valid signature unlocks funds to the bound destination.
4. At or after expiration, the original locker can reclaim.

## Findings

### Critical: unlock signatures are not domain-separated

Evidence: `vm/embedded/implementation/ptlc.go:195`

```go
unlockMessage := crypto.Hash(common.JoinBytes(id.Bytes(), destination.Bytes()))
```

This binds only the PTLC id and destination. It does not bind contract address, network/chain, method, version, point type, or a purpose string. That makes the signature context too broad for a consensus money-moving primitive.

The tests also sign the same bare message in multiple places, for example `vm/embedded/tests/ptlc_test.go:178`, `vm/embedded/tests/ptlc_test.go:259`, and `vm/embedded/tests/ptlc_test.go:945`.

Recommendation:

```go
unlockMessage := crypto.Hash(common.JoinBytes(
    []byte("zenon-ptlc-unlock:v1"),
    types.PtlcContract.Bytes(),
    []byte{ptlcInfo.PointType},
    id.Bytes(),
    destination.Bytes(),
))
```

Add a chain/network id if one is available in consensus context.

### Critical: unknown stored `PointType` can fall through to fund release

Evidence: `vm/embedded/implementation/ptlc.go:190` and `vm/embedded/implementation/ptlc.go:219`

```go
if len(signature) != int(definition.PointTypeSignatureSizes[ptlcInfo.PointType]) {
    return nil, constants.ErrInvalidPointSignature
}
...
} else {
    // shouldn't get here
}

common.DealWithErr(ptlcInfo.Delete(context.Storage()))
```

If stored state contains an unknown point type, the signature-size map returns zero. A zero-length signature can pass the size check, hit the empty `else`, and then the code deletes the entry and sends funds.

The public `Create` path rejects unknown point types, so this should not be reachable through normal creation. It is still unacceptable to let malformed loaded state proceed to a transfer.

Recommendation:

- Return `constants.ErrInvalidPointType` in the final `else`.
- Validate loaded PTLC state before time and signature checks.
- Validate `PointType`, point-lock length, amount, and expiration.

### High: BIP340 parser errors leak raw library errors

Evidence: `vm/embedded/implementation/ptlc.go:206` and `vm/embedded/implementation/ptlc.go:210`

```go
s, err := schnorr.ParseSignature(signature)
if err != nil {
    return nil, err
}
pk, err := schnorr.ParsePubKey(ptlcInfo.PointLock)
if err != nil {
    return nil, err
}
```

Consensus-facing contract code should return stable contract errors, not raw dependency errors. Malformed signatures should map to `constants.ErrInvalidPointSignature`; malformed stored public keys should map to `constants.ErrInvalidPointLock`.

Recommendation:

- Wrap BIP340 parse errors in stable PTLC errors.
- Also avoid raw ED25519 key-length errors by validating loaded PTLC state before verification.

### High: PTLC storage key parsing can panic on empty keys

Evidence: `vm/embedded/definition/ptlc.go:130`

```go
func isPtlcInfoKey(key []byte) bool {
    return key[0] == ptlcInfoKeyPrefix[0]
}
```

This panics on `len(key) == 0`. It also accepts any key with the right first byte until `unmarshalPtlcInfoKey` later calls `SetBytes`.

Current `GetPtlcInfo` builds keys internally, so this is not obviously reachable through the current RPC path. It is still unsafe parser code and should be fixed before more storage iteration/list APIs are added.

Recommendation:

```go
func isPtlcInfoKey(key []byte) bool {
    return len(key) == 1+types.HashSize && key[0] == ptlcInfoKeyPrefix[0]
}
```

Add malformed key tests for empty, short, long, wrong-prefix, and valid keys.

### High: this is not a complete PTLC/adaptor-signature specification

The implementation verifies ordinary ED25519 or BIP340 signatures against a stored public key. It does not document an adaptor-signature protocol, what secret is revealed, how the point lock maps to a scalar relation, or what cross-chain PTLC security property is intended.

That can still be useful, but the implementation should be documented and named honestly. A safer label would be `signature time-locked contract` or `PTLC-compatible signature lock primitive` unless the adaptor-signature protocol is specified.

### High: PTLC documentation is missing

There are no PTLC docs in the PR. `rg --files | rg -i '(^docs/|ptlc)'` finds only:

- `rpc/api/embedded/ptlc.go`
- `vm/embedded/definition/ptlc.go`
- `vm/embedded/implementation/ptlc.go`
- `vm/embedded/implementation/ptlc_test.go`
- `vm/embedded/tests/ptlc_test.go`

This is a blocker for a cryptographic primitive.

Required docs:

- Overview of what the primitive does and does not prove.
- Exact signing message.
- ED25519 mode.
- BIP340 mode.
- ProxyUnlock bearer-proof semantics.
- Difference from HTLC.
- Whether adaptor signatures are supported or merely expected at a higher layer.

### High: dependency churn is broader than the feature requires

Evidence: `go.mod:6`, `go.mod:7`, `go.mod:9`, `go.mod:21`, `go.mod:48`, `go.mod:49`, and `go.mod:50`.

The PR promotes `github.com/btcsuite/btcd/btcec/v2` to a direct dependency and changes `btcutil`, `go-ethereum`, `testify`, `x/sync`, `x/sys`, and protobuf versions. Some changes may be caused by `go mod tidy`, but the PR should keep consensus dependency churn as narrow as possible.

Recommendation:

- Keep only the dependency changes needed for BIP340.
- Justify `btcec/v2` and `schnorr`.
- Revert unrelated version bumps where possible.
- Re-run `go mod tidy` from a clean toolchain and review the diff.

### Medium: ProxyUnlock semantics need explicit documentation

Evidence: `vm/embedded/implementation/ptlc.go:274` and `vm/embedded/implementation/ptlc.go:307`

`ProxyUnlock` lets any caller submit `id`, `destination`, and a valid signature over the destination. That is a bearer-proof flow. It is safe only because the destination is signed, but wallet and UI developers need that documented clearly.

The ED25519 tests cover wrong signer and wrong destination. There is also a test comment mismatch: `vm/embedded/tests/ptlc_test.go:294` says "user3 proxy unlocks" but the account block at `vm/embedded/tests/ptlc_test.go:296` uses `g.User2.Address`.

Recommendation:

- Document that caller and recipient can differ.
- Fix the misleading test comment or make the test actually use `g.User3.Address`.
- Add the same proxy matrix for BIP340.

### Medium: spork behavior needs direct tests

Evidence: `vm/embedded/embedded.go:235`

```go
if context.IsPtlcSporkEnforced() {
    contractsMap = ptlcEmbedded
} else if context.IsHtlcSporkEnforced() {
    contractsMap = htlcEmbedded
}
```

The spork ordering appears intended: once PTLC is active, the PTLC map includes HTLC and prior contracts through `getPtlc() -> getHtlc() -> getBridgeAndLiquidity()`. But the tests do not directly prove:

- PTLC is unavailable before the PTLC spork.
- PTLC is available after the PTLC spork.
- Existing embedded contracts remain available after the PTLC spork.

Test helper note: `vm/embedded/tests/ptlc_test.go:43` mutates the global `types.PtlcSpork.SporkId` and `types.ImplementedSporksMap`. That works for these non-parallel tests, but it is brittle and should be isolated or restored.

## Test coverage observed

The PR has real PTLC tests. Covered behavior includes:

- zero amount rejected
- ED25519 create/unlock
- ED25519 proxy unlock wrong signer and wrong destination
- reclaim
- expiration boundary behavior
- reclaim access control
- nonexistent entry behavior
- deletion after unlock
- deletion after reclaim
- expired create rejected
- BIP340 unlock
- basic point type and point-lock length validation

Important missing tests:

- domain-separated signing
- old `Hash(id || destination)` signatures rejected after domain separation
- unknown stored `PointType` cannot unlock
- malformed BIP340 signature returns stable `ErrInvalidPointSignature`
- malformed BIP340 pubkey returns stable `ErrInvalidPointLock`
- malformed PTLC storage keys do not panic
- PTLC unavailable before PTLC spork
- PTLC available after PTLC spork
- existing contracts remain available after PTLC spork
- BIP340 proxy unlock matrix

## Verification performed

Formatting:

```text
gofmt -l vm/embedded/definition/ptlc.go vm/embedded/implementation/ptlc.go vm/embedded/implementation/ptlc_test.go vm/embedded/tests/ptlc_test.go rpc/api/embedded/ptlc.go
```

Result: no output, meaning the relevant PTLC files are gofmt-clean.

Focused tests:

```text
go test ./vm/embedded/implementation ./vm/embedded/tests ./rpc/api/embedded
```

Result:

```text
ok   github.com/zenon-network/go-zenon/vm/embedded/implementation 0.531s
ok   github.com/zenon-network/go-zenon/vm/embedded/tests          147.428s
?    github.com/zenon-network/go-zenon/rpc/api/embedded           [no test files]
```

Embedded package tests:

```text
go test ./vm/embedded/...
```

Result: pass.

Full repo tests:

```text
go test ./...
```

Result: pass after allowing dependency downloads. The only notable output was a macOS deprecation warning from `github.com/shirou/gopsutil/host`.

## Recommended fix list

1. Domain-separate the unlock message.
2. Update ED25519 and BIP340 tests to sign the new message.
3. Add tests proving old `Hash(id || destination)` signatures are rejected.
4. Return `constants.ErrInvalidPointType` for unknown stored `PointType`.
5. Validate loaded PTLC state before unlock and reclaim state use.
6. Wrap BIP340 parser errors in stable contract errors.
7. Harden PTLC storage key parsing against empty and malformed keys.
8. Add malformed key parser tests.
9. Add BIP340 proxy unlock tests for valid proxy, wrong destination, wrong signer, and wrong id.
10. Add spork tests for unavailable-before, available-after, and existing-contract compatibility.
11. Add PTLC docs for signing, security model, modes, ProxyUnlock, HTLC differences, and adaptor-signature scope.
12. Review and minimize `go.mod` and `go.sum` churn.

## Bottom line

I would not merge PR 13 as-is.

The implementation is worth salvaging, and the tests pass, but passing tests do not resolve the blocking issues: crypto domain separation, malformed-state hardening, documentation, dependency hygiene, and missing adversarial coverage.
