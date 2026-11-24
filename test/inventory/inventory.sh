#!/bin/sh
# Print every testable item of go-zenon, one per line, as "kind<TAB>item".
# The list is derived from the source, so anything added to the code shows up here
# and, until a test case is recorded for it in coverage.tsv, in check.sh's gap report.
set -eu
cd "$(dirname "$0")/../.."
src() { find "$@" -name '*.go' ! -name '*_test.go' | sort; }

# Embedded contract methods: ABI function entries, by definition file. common.go
# holds schemas shared by several contracts, not methods of a contract; each
# contract that uses one lists it in its own ABI, so common.go is left out.
for f in $(src vm/embedded/definition); do
  [ "$(basename "$f")" = common.go ] && continue
  grep -o '{"type":"function","name":"[A-Za-z0-9]*"' "$f" 2>/dev/null |
    sed -E "s|.*\"name\":\"([A-Za-z0-9]*)\"|contract-method\t$(basename "$f" .go).\1|" || true
done

# RPC methods: exported methods on the *Api types. The server registers every
# exported method with a suitable signature, String included, and serves
# rpc.modules itself.
for f in $(src rpc/api); do
  sed -nE 's/^func \([a-z]+ \*?([A-Za-z]+(Api|API))\) ([A-Z][A-Za-z0-9]*)\(.*/rpc-method\t\1.\3/p' "$f"
done
grep -qE '^func \([a-z]+ \*RPCService\) Modules\(' rpc/server/server.go && printf 'rpc-method\tRPCService.Modules\n'

# Subscription topics.
sed -nE 's/^func \([a-z]+ \*?Api\) (To[A-Z][A-Za-z0-9]*|[A-Z][A-Za-z0-9]*)\(ctx context\.Context.*/subscription\t\1/p' rpc/api/subscribe/api.go

# Sporks.
sed -nE 's/^[[:space:]]*([A-Za-z0-9]+Spork)[[:space:]]*=.*/spork\t\1/p' common/types/spork.go

# Verifier rules: the per-rule methods of the account-block and momentum
# verifiers, qualified by receiver type (two types in one file share method names).
for f in verifier/account_block.go verifier/momentum.go; do
  sed -nE 's/^func \([a-z]+ \*?([A-Za-z]+)\) ([a-z][A-Za-z0-9]*)\(.*/verifier-rule\t\1.\2/p' "$f"
done

# Wire protocol messages and protocol error codes.
sed -nE '/^[[:space:]]+Err/!s/^[[:space:]]+([A-Za-z]+Msg)([[:space:]]*=.*)?$/protocol-msg\t\1/p' protocol/protocol.go
sed -nE 's/^[[:space:]]+(Err[A-Za-z]+)([[:space:]]*=[[:space:]]*iota)?$/protocol-error\t\1/p' protocol/protocol.go

# Declared error values, by package: each is a refusal path that needs a case
# reaching it. Any exported value built by errors.New, fmt.Errorf or
# errors.Errorf counts, whatever its name (RevokeNotDue, ReclaimNotDue) and
# whether it sits in a var block or on a line of its own.
for f in $(src .); do
  case "$f" in ./build/*|./test/*) continue ;; esac
  pkg="$(dirname "$f" | sed 's#^\./##')"
  sed -nE \
    -e "s#^(var)?[[:space:]]*(Err[A-Za-z0-9]+)[[:space:]]*=[[:space:]]*(constants\.|common\.|New).*#error	$pkg.\2#p" \
    -e "s#^(var)?[[:space:]]*([A-Z][A-Za-z0-9]+)[[:space:]]*=[[:space:]]*(errors\.New|fmt\.Errorf|errors\.Errorf)\(.*#error	$pkg.\2#p" "$f"
done

# Command-line flags, and the fields of the two config structures. node/config.go
# is what an operator writes in config.json; p2p/config.go is the structure the
# node builds for the transport. They share field names, so each is qualified.
sed -nE 's/^[[:space:]]+Name:[[:space:]]+"([a-z0-9.-]+)".*/cli-flag\t\1/p' app/flags.go
for f in node/config.go p2p/config.go; do
  sed -nE "s/^[[:space:]]+([A-Z][A-Za-z0-9]+)[[:space:]]+[][*A-Za-z0-9.]+([[:space:]]+\`.*)?([[:space:]]*\/\/.*)?$/config-field\t$(dirname "$f").\1/p" "$f"
done
