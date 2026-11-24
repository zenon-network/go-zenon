#!/bin/sh
# Print the Go build overlay that adds the fast test constants to a source
# tree. Usage: overlay.sh <source-root> [root-as-the-build-sees-it]
set -eu
ROOT="$1"; SEEN="${2:-$1}"
cat <<JSON
{"Replace": {
  "$SEEN/vm/constants/zz_fast_test_constants.go": "$SEEN/test/devnet/fast/constants.go.in",
  "$SEEN/consensus/zz_fast_test_constants.go": "$SEEN/test/devnet/fast/consensus.go.in"
}}
JSON
