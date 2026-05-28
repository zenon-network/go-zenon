#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RESULTS_DIR="${TESTNET_RESULTS_DIR:-$ROOT_DIR/test-results/ptlc}"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
RUN_DIR="$RESULTS_DIR/$TIMESTAMP"
LOG_FILE="$RUN_DIR/go-test.log"
SUMMARY_FILE="$RUN_DIR/summary.md"

RPC_URL="${PTLC_TESTNET_RPC:-http://localhost:35997}"
GO_TEST_FLAGS="${TESTNET_GO_TEST_FLAGS:--timeout 25m}"
GOCACHE_DIR="${GOCACHE:-$ROOT_DIR/.gocache}"

mkdir -p "$RUN_DIR" "$GOCACHE_DIR"

echo "PTLC testnet suite"
echo "  rpc:     $RPC_URL"
echo "  results: $RUN_DIR"
echo

set +e
(
	cd "$ROOT_DIR"
	PTLC_TESTNET_RPC="$RPC_URL" GOCACHE="$GOCACHE_DIR" \
		go test -tags testnet ./testnet/ptlc -count=1 $GO_TEST_FLAGS -v
) 2>&1 | tee "$LOG_FILE"
TEST_STATUS="${PIPESTATUS[0]}"
set -e

if [ "$TEST_STATUS" -eq 0 ]; then
	SUITE_STATUS="PASS"
else
	SUITE_STATUS="FAIL"
fi

{
	echo "# PTLC Testnet Suite"
	echo
	echo "| Field | Value |"
	echo "|---|---|"
	echo "| Status | $SUITE_STATUS |"
	echo "| Date UTC | $TIMESTAMP |"
	echo "| RPC endpoint | \`$RPC_URL\` |"
	echo "| Package | \`./testnet/ptlc\` |"
	echo "| Flags | \`$GO_TEST_FLAGS\` |"
	echo "| Raw log | \`$LOG_FILE\` |"
	echo
	echo "## Test Results"
	echo
	awk '
		function description(test) {
			if (test == "TestPtlcCreateValidationViaRPC") {
				return "Create validation rejects zero amount, bad point type, bad point lock, and expired create";
			}
			if (test == "TestPtlcED25519DomainSeparatedUnlockViaRPC") {
				return "ED25519 unlock requires the domain-separated message and pays the recipient";
			}
			if (test == "TestPtlcBIP340ProxyDestinationBindingViaRPC") {
				return "BIP340 proxy unlock rejects the wrong destination and accepts the bound destination";
			}
			if (test == "TestPtlcExpirationAndReclaimViaRPC") {
				return "Expiration rejects early reclaim and expired unlock, then allows locker reclaim";
			}
			if (test == "TestRpcRelayDiagnosticViaRPC") {
				return "Opt-in diagnostic comparing dedicated RPC and pillar visibility";
			}
			return "";
		}
		BEGIN {
			print "| Result | Test | What It Covers | Duration |";
			print "|---|---|---|---|";
		}
		/^--- (PASS|FAIL|SKIP): / {
			result=$2;
			sub(":", "", result);
			test=$3;
			duration=$0;
			sub(/^.*\(/, "", duration);
			sub(/\).*$/, "", duration);
			print "| " result " | `" test "` | " description(test) " | " duration " |";
		}
	' "$LOG_FILE"
	echo
	echo "## Package Result"
	echo
	awk '/^(ok|FAIL)[[:space:]]+github.com\/zenon-network\/go-zenon\/testnet\/ptlc/ { print "- `" $0 "`" }' "$LOG_FILE"
} > "$SUMMARY_FILE"

echo
echo "Summary written to $SUMMARY_FILE"
echo "Raw log written to $LOG_FILE"

exit "$TEST_STATUS"
