#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RESULTS_DIR="${PTLC_FUZZ_RESULTS_DIR:-$ROOT_DIR/test-results/ptlc-fuzz}"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
RUN_DIR="$RESULTS_DIR/$TIMESTAMP"
UNIT_LOG="$RUN_DIR/unit-adversarial.log"
SUMMARY_FILE="$RUN_DIR/summary.md"
STATUS_FILE="$RUN_DIR/fuzz-status.tsv"

GOCACHE_DIR="${GOCACHE:-$ROOT_DIR/.gocache}"
UNIT_TIMEOUT="${PTLC_FUZZ_UNIT_TIMEOUT:-120s}"
FUZZ_TIME="${PTLC_FUZZ_TIME:-5s}"
FUZZ_TIMEOUT="${PTLC_FUZZ_TIMEOUT:-45s}"

FUZZ_TARGETS=(
	"FuzzPtlcValidateSendBlockNoPanic"
	"FuzzPtlcStoredInfoValidation"
	"FuzzPtlcED25519DomainMutationRejected"
	"FuzzPtlcBIP340DomainMutationRejected"
	"FuzzPtlc_CheckAndVerify"
	"FuzzPtlcPointScalarWitness"
	"FuzzPtlcPointLockEncoding"
)

mkdir -p "$RUN_DIR" "$GOCACHE_DIR"
: > "$STATUS_FILE"

echo "PTLC fuzz/adversarial suite"
echo "  results:      $RUN_DIR"
echo "  unit timeout: $UNIT_TIMEOUT"
echo "  fuzz time:    $FUZZ_TIME"
echo

set +e
(
	cd "$ROOT_DIR"
	GOCACHE="$GOCACHE_DIR" \
		go test ./vm/embedded/definition ./vm/embedded/implementation ./vm/embedded/tests \
			-run 'TestPtlc_|FuzzPtlc' \
			-timeout "$UNIT_TIMEOUT" \
			-count=1 \
			-v
) 2>&1 | tee "$UNIT_LOG"
UNIT_STATUS="${PIPESTATUS[0]}"
set -e

for target in "${FUZZ_TARGETS[@]}"; do
	log_file="$RUN_DIR/fuzz-$target.log"
	echo
	echo "Running $target for $FUZZ_TIME"

	set +e
	(
		cd "$ROOT_DIR"
		GOCACHE="$GOCACHE_DIR" \
			go test ./vm/embedded/implementation \
				-run '^$' \
				-fuzz "$target" \
				-fuzztime="$FUZZ_TIME" \
				-timeout "$FUZZ_TIMEOUT"
	) 2>&1 | tee "$log_file"
	fuzz_status="${PIPESTATUS[0]}"
	set -e

	if [ "$fuzz_status" -eq 0 ]; then
		result="PASS"
	else
		result="FAIL"
	fi
	printf '%s\t%s\t%s\n' "$target" "$result" "$log_file" >> "$STATUS_FILE"
done

SUITE_STATUS="PASS"
if [ "$UNIT_STATUS" -ne 0 ]; then
	SUITE_STATUS="FAIL"
fi
while IFS=$'\t' read -r _target result _log_file; do
	if [ "$result" != "PASS" ]; then
		SUITE_STATUS="FAIL"
	fi
done < "$STATUS_FILE"

{
	echo "# PTLC Fuzz and Adversarial Suite"
	echo
	echo "| Field | Value |"
	echo "|---|---|"
	echo "| Status | $SUITE_STATUS |"
	echo "| Date UTC | $TIMESTAMP |"
	echo "| Unit packages | \`./vm/embedded/definition ./vm/embedded/implementation ./vm/embedded/tests\` |"
	echo "| Unit filter | \`TestPtlc_|FuzzPtlc\` |"
	echo "| Unit timeout | \`$UNIT_TIMEOUT\` |"
	echo "| Fuzz package | \`./vm/embedded/implementation\` |"
	echo "| Fuzz time per target | \`$FUZZ_TIME\` |"
	echo "| Fuzz timeout per target | \`$FUZZ_TIMEOUT\` |"
	echo "| Unit/adversarial raw log | \`$UNIT_LOG\` |"
	echo
	echo "## Unit and Adversarial Results"
	echo
	awk '
		function description(test, base) {
			base = test;
			sub(/\/.*/, "", base);
			if (base == "TestPtlc_PointType") return "Rejects unknown point types";
			if (base == "TestPtlc_LockLength") return "Rejects point locks with invalid lengths";
			if (base == "TestPtlc_StoredInfoValidation") return "Validates persisted PTLC invariants before use";
			if (base == "TestPtlc_VerifySignatureStableErrors") return "Keeps signature and point-lock failures stable";
			if (base == "TestPtlc_VerifySignatureRejectsWrongDomainFields") return "Rejects signatures bound to the wrong chain, contract, id, destination, or point type";
			if (base == "TestPtlc_ValidateAmountGuards") return "Rejects invalid token amounts for create, unlock, proxy unlock, and reclaim";
			if (base == "TestPtlc_BIP340OfficialVectors") return "Checks BIP340 verification against official vectors";
			if (base == "TestPtlc_UnlockRejectsCorruptStoredPointType") return "Rejects corrupt stored PTLC entries before unlock";
			if (base == "TestPtlc_CreateExpirationBoundary") return "Allows create before expiration and rejects at/after expiration";
			if (base == "FuzzPtlcValidateSendBlockNoPanic") return "Seed corpus for malformed ABI/send-block validation";
			if (base == "FuzzPtlcStoredInfoValidation") return "Seed corpus for persisted PTLC invariant validation";
			if (base == "FuzzPtlcED25519DomainMutationRejected") return "Seed corpus for ED25519 domain mutation rejection";
			if (base == "FuzzPtlcBIP340DomainMutationRejected") return "Seed corpus for BIP340 domain mutation rejection";
			if (base == "FuzzPtlc_CheckAndVerify") return "Seed corpus for create and witness checks over all three point types returning only errors the contract defines";
			if (base == "FuzzPtlcPointScalarWitness") return "Seed corpus for a point lock opening with its canonical scalar and nothing else";
			if (base == "FuzzPtlcPointLockEncoding") return "Seed corpus for a point lock being accepted only as a compressed point on the curve";
			if (base == "TestPtlc_CreateRejectsInvalidBIP340Point") return "Rejects a BIP340 lock with no point above it at create";
			if (base == "TestPtlc_WitnessIsDestinationBound") return "A key-lock witness for one destination does not verify for another";
			if (base == "TestPtlc_WitnessNotPortableAcrossEntries") return "A key-lock witness for one entry does not verify for another";
			if (base == "TestPtlc_ReclaimIndependentOfPointFields") return "Reclaim needs only the reclaim-relevant stored fields";
			if (base == "TestPtlc_DoubleUnlockDeletesBeforeSecondWithdrawal") return "A second unlock finds the entry deleted";
			if (base == "TestPtlc_UnlockThenReclaimRace") return "Unlock and reclaim cannot both pay";
			if (base == "TestPtlc_PointScalarRules") return "Point lock opens only with the canonical scalar: not its negation, zero, the order, a non-canonical encoding or another length";
			if (base == "TestPtlc_PointLockEncoding") return "Point lock must be a compressed point on the curve";
			if (base == "TestPtlc_DestinationRules") return "A point lock must name a destination, a key lock may, and none may name an embedded contract, at create and in stored state";
			if (base == "TestPtlc_createRejectsInvalidBIP340Point") return "The node does not take a create with an invalid BIP340 lock";
			if (base == "TestPtlc_pointLock_create_rules") return "Point-lock create rules on the mock chain: destination required, encoding checked";
			if (base == "TestPtlc_pointLock_unlock") return "Point lock pays its named destination for the right scalar, direct and proxied, and nobody else";
			if (base == "TestPtlc_pointLock_expiry_and_reclaim") return "Point lock refuses the scalar at expiry and returns to its creator";
			if (base == "TestPtlc_fixedDestination_keyLock") return "Key lock with a fixed destination refuses a valid signature for another address";
			if (base == "TestPtlc_pointLock_tweakedSwap") return "Swap on two point locks with a tweak, in the safe order";
			if (base == "TestPtlc_pointLock_swapOrder_wrongOrderLosesBob") return "The swap with the party that lacks the secret locking first and last: it loses";
			if (base == "TestPtlc_pointLock_swapOrder_lateRevealCostsTheRevealer") return "A scalar revealed after the other entry has expired costs the revealer both legs";
			if (base == "TestPtlc_keySwap_adaptor") return "Swap on two BIP340 key locks with adaptor pre-signatures, in all four key and nonce parity cases";
			if (base == "TestPtlc_keySwap_keysPayOnlyTheCounterparty") return "With fixed destinations neither lock key can pay its own owner, and neither entry is reclaimable early";
			if (base == "TestPtlc_keySwap_bobNeverLocks") return "Key swap abandoned after the first lock: a pre-signature opens nothing and the creator reclaims";
			if (base == "TestPtlc_keySwap_aliceNeverClaims") return "Key swap abandoned after both locks: each reclaims at its own expiry";
			if (base == "TestPtlc_keySwap_lateClaimCostsTheClaimer") return "A completed signature published after expiry gives the secret away";
			if (base == "TestPtlc_keySwap_openDestinationLosesBob") return "Key swap whose first entry names no destination: its maker takes it back";
			if (base == "TestPtlc_spork_gating") return "Requires PTLC spork activation";
			if (base == "TestPtlc_zero") return "Rejects zero-value creates";
			if (base == "TestPtlc_unlock") return "Covers ED25519 direct unlock validation and payout";
			if (base == "TestPtlc_proxy_unlock") return "Covers ED25519 proxy unlock validation and payout";
			if (base == "TestPtlc_wrongChainSignature") return "Rejects signatures from another chain id";
			if (base == "TestPtlc_wrongContractSignature") return "Rejects signatures from another embedded contract domain";
			if (base == "TestPtlc_reclaim") return "Covers expiration and locker reclaim";
			if (base == "TestPtlc_create_expiration_time") return "Rejects creates at current expiration time";
			if (base == "TestPtlc_unlock_expiration_time") return "Rejects unlock at expiration";
			if (base == "TestPtlc_reclaim_expiration_time") return "Allows reclaim at expiration";
			if (base == "TestPtlc_reclaim_access") return "Restricts reclaim to the time-locking address";
			if (base == "TestPtlc_nonexistent") return "Rejects unlock/reclaim for unknown PTLC ids";
			if (base == "TestPtlc_nonexistent_after_unlock") return "Rejects reuse after unlock";
			if (base == "TestPtlc_nonexistent_after_reclaim") return "Rejects reuse after reclaim";
			if (base == "TestPtlc_create_expired") return "Rejects already-expired create attempts";
			if (base == "TestPtlc_unlockBIP340") return "Covers BIP340 direct unlock validation and payout";
			if (base == "TestPtlc_proxyUnlockBIP340") return "Covers BIP340 proxy unlock validation and payout";
			if (base == "TestPtlc_proxyUnlockBIP340_lifecycle") return "Covers BIP340 expired, reclaimed, proxy-first, and direct-first lifecycle paths";
			if (base == "TestPtlc_unlockQsrAccounting") return "Verifies QSR unlock accounting and contract drain";
			if (base == "TestPtlc_ED25519UnlockReplayCompetition") return "Covers ED25519 proxy-first and direct-first replay competition";
			return "PTLC regression coverage";
		}
		BEGIN {
			print "| Result | Test | What It Covers | Duration |";
			print "|---|---|---|---|";
		}
		/^[[:space:]]*--- (PASS|FAIL|SKIP): (Test|Fuzz)Ptlc/ {
			result=$2;
			sub(":", "", result);
			test=$3;
			duration=$0;
			sub(/^.*\(/, "", duration);
			sub(/\).*$/, "", duration);
			print "| " result " | `" test "` | " description(test) " | " duration " |";
		}
	' "$UNIT_LOG"
	echo
	echo "## Live Fuzz Results"
	echo
	echo "| Result | Fuzz Target | Execs | Interesting Inputs | Duration | Raw Log |"
	echo "|---|---|---:|---:|---|---|"
	while IFS=$'\t' read -r target result log_file; do
		execs="$(awk '/^fuzz: elapsed:/ && /execs:/ { line=$0 } END { if (line == "") { print "n/a"; } else { sub(/^.*execs: /, "", line); sub(/ .*/, "", line); print line; } }' "$log_file")"
		interesting="$(awk '/^fuzz: elapsed:/ && /new interesting:/ { line=$0 } END { if (line == "") { print "n/a"; } else { sub(/^.*new interesting: /, "", line); sub(/ .*/, "", line); print line; } }' "$log_file")"
		duration="$(awk '/^ok[[:space:]]+github.com\/zenon-network\/go-zenon\/vm\/embedded\/implementation/ { d=$3 } END { if (d == "") print "n/a"; else print d }' "$log_file")"
		echo "| $result | \`$target\` | $execs | $interesting | $duration | \`$log_file\` |"
	done < "$STATUS_FILE"
	echo
	echo "## Package Results"
	echo
	awk '/^(ok|FAIL)[[:space:]]+github.com\/zenon-network\/go-zenon\/vm\/embedded\/(definition|implementation|tests)/ { print "- `" $0 "`" }' "$UNIT_LOG"
	while IFS=$'\t' read -r _target _result log_file; do
		awk '/^(ok|FAIL)[[:space:]]+github.com\/zenon-network\/go-zenon\/vm\/embedded\/implementation/ { print "- `" $0 "`" }' "$log_file"
	done < "$STATUS_FILE"
} > "$SUMMARY_FILE"

echo
echo "Summary written to $SUMMARY_FILE"
echo "Unit/adversarial log written to $UNIT_LOG"
echo "Fuzz logs written to $RUN_DIR/fuzz-*.log"

if [ "$SUITE_STATUS" = "PASS" ]; then
	exit 0
fi
exit 1
