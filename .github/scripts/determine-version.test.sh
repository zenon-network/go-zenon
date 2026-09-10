#!/usr/bin/env bash
set -euo pipefail

# Regression tests for determine-version.sh's base-tag selection. Each case
# builds a throwaway git repo, tags it to reproduce a release history shape,
# and pins the resulting version string, base count, channel suffix and
# prerelease flag. No release is published and no binaries are built.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DETERMINE_VERSION="${SCRIPT_DIR}/determine-version.sh"

pass=0
fail=0

assert_eq() {
  local desc="$1" expected="$2" actual="$3"
  if [ "$expected" = "$actual" ]; then
    echo "ok - $desc"
    pass=$((pass + 1))
  else
    echo "FAIL - $desc: expected '$expected', got '$actual'"
    fail=$((fail + 1))
  fi
}

assert_matches() {
  local desc="$1" pattern="$2" actual="$3"
  if [[ "$actual" =~ $pattern ]]; then
    echo "ok - $desc ($actual)"
    pass=$((pass + 1))
  else
    echo "FAIL - $desc: '$actual' does not match /$pattern/"
    fail=$((fail + 1))
  fi
}

count_occurrences() {
  local haystack="$1" needle="$2"
  grep -o -- "$needle" <<<"$haystack" | wc -l | tr -d ' '
}

new_repo() {
  WORK="$(mktemp -d)"
  git -C "$WORK" init -q
  git -C "$WORK" config user.email test@example.com
  git -C "$WORK" config user.name test
}

commit() {
  git -C "$WORK" commit --allow-empty -q -m "$1"
}

tag() {
  git -C "$WORK" tag "$1"
}

count_since() {
  git -C "$WORK" rev-list "$1..HEAD" --count
}

# Runs determine-version.sh against $WORK and sets OUT_VERSION/OUT_PRERELEASE.
run_determine_version() {
  local ref_type="$1" ref_name="$2"
  local out
  out="$(cd "$WORK" && REF_TYPE="$ref_type" REF_NAME="$ref_name" "$DETERMINE_VERSION")"
  OUT_VERSION="$(sed -n 's/^version=//p' <<<"$out")"
  OUT_PRERELEASE="$(sed -n 's/^prerelease=//p' <<<"$out")"
}

echo "== Case 1: two consecutive dev releases after a stable/alphanet base tag =="
new_repo
commit "base"
tag "v0.0.8-alphanet"
commit "c1"
count1="$(count_since v0.0.8-alphanet)"
run_determine_version branch dev
assert_matches "first dev build version" "^v0\.0\.8-dev-${count1}-g[0-9a-f]+\$" "$OUT_VERSION"
assert_eq "first dev build prerelease" "true" "$OUT_PRERELEASE"
tag "$OUT_VERSION" # mimics svenstaro/upload-release-action creating the release tag
commit "c2"
count2="$(count_since v0.0.8-alphanet)"
run_determine_version branch dev
assert_matches "second dev build version" "^v0\.0\.8-dev-${count2}-g[0-9a-f]+\$" "$OUT_VERSION"
assert_eq "second dev build channel suffix count" "1" "$(count_occurrences "$OUT_VERSION" '\-dev\-')"
assert_eq "second dev build prerelease" "true" "$OUT_PRERELEASE"

echo "== Case 2: two consecutive testnet releases =="
new_repo
commit "base"
tag "v0.0.8-alphanet"
commit "c1"
count1="$(count_since v0.0.8-alphanet)"
run_determine_version branch testnet
assert_matches "first testnet build version" "^v0\.0\.8-testnet-${count1}-g[0-9a-f]+\$" "$OUT_VERSION"
assert_eq "first testnet build prerelease" "true" "$OUT_PRERELEASE"
tag "$OUT_VERSION"
commit "c2"
count2="$(count_since v0.0.8-alphanet)"
run_determine_version branch testnet
assert_matches "second testnet build version" "^v0\.0\.8-testnet-${count2}-g[0-9a-f]+\$" "$OUT_VERSION"
assert_eq "second testnet build channel suffix count" "1" "$(count_occurrences "$OUT_VERSION" '\-testnet\-')"
assert_eq "second testnet build prerelease" "true" "$OUT_PRERELEASE"

echo "== Case 3: both prerelease tag families reachable in history =="
new_repo
commit "base"
tag "v0.0.8-alphanet"
commit "dev1"
run_determine_version branch dev
dev_tag="$OUT_VERSION"
tag "$dev_tag"
commit "testnet1"
count_testnet="$(count_since v0.0.8-alphanet)"
run_determine_version branch testnet
assert_matches "testnet build bases off alphanet, not the dev tag" "^v0\.0\.8-testnet-${count_testnet}-g[0-9a-f]+\$" "$OUT_VERSION"
tag "$OUT_VERSION"
commit "dev2"
count_dev="$(count_since v0.0.8-alphanet)"
run_determine_version branch dev
assert_matches "dev build bases off alphanet, not the testnet tag" "^v0\.0\.8-dev-${count_dev}-g[0-9a-f]+\$" "$OUT_VERSION"
assert_eq "dev build channel suffix count" "1" "$(count_occurrences "$OUT_VERSION" '\-dev\-')"
assert_eq "dev build has no testnet suffix" "0" "$(count_occurrences "$OUT_VERSION" '\-testnet\-')"

echo "== Case 4: explicit stable/alphanet tag build uses its tag verbatim =="
new_repo
commit "base"
tag "v1.2.3-alphanet"
run_determine_version tag "v1.2.3-alphanet"
assert_eq "tag build version" "v1.2.3-alphanet" "$OUT_VERSION"
assert_eq "tag build prerelease" "false" "$OUT_PRERELEASE"

echo
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
