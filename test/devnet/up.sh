#!/bin/sh
# Start the five devnet nodes on a genesis variant, from an image built by
# build-image.sh, on an empty chain, and prove what is running before anything
# is tested against it.
#
#   test/devnet/up.sh [stock|all-off|mainnet-like|all-on|staged|fast] [manifest-file]
#   test/devnet/down.sh [log-directory]
#
# 'fast' runs the test build (FAST=1 test/devnet/build-image.sh) on a genesis with
# every spork and a genesis time of now.
#
# It fails, and leaves nothing half-started behind as "up", when: the variant is
# unknown; the old chain cannot be removed; a node does not answer RPC in time; a
# container does not run the expected image, binary or genesis; or the chain is
# not new. The genesis momentum of every node is compared with the one computed
# from the genesis file by test/devnet/genesishash. The manifest records image
# id, binary and genesis digests per node.
set -eu
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
VARIANT="${1:-stock}"
MANIFEST="${2:-}"
NODES="pillar pillar2 pillar3 rpc observer"
cd "$ROOT"
sha() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1; }
die() { echo "up.sh: $*" >&2; exit 1; }

# 1. Validate everything before touching the running chain.
case "$VARIANT" in stock|all-off|mainnet-like|all-on|staged|fast) ;; *) die "unknown variant '$VARIANT'; see test/devnet/genesis.py --list" ;; esac
if [ "$VARIANT" = fast ]; then IMAGE="${DEVNET_IMAGE:-go-zenon-devnet-fast:latest}"; BIN="$ROOT/build/linux-fast/znnd"
else IMAGE="${DEVNET_IMAGE:-go-zenon-devnet:latest}"; BIN="$ROOT/build/linux/znnd"; fi
IMAGE_ID="$(docker image inspect -f '{{.Id}}' "$IMAGE" 2>/dev/null)" || die "image $IMAGE does not exist; run test/devnet/build-image.sh"
if [ "$VARIANT" = stock ]; then GENESIS="$ROOT/docker/devnet/genesis.json"
else python3 test/devnet/genesis.py "$VARIANT" >/dev/null || die "cannot generate the $VARIANT genesis"; GENESIS="$ROOT/test/devnet/genesis/$VARIANT.json"; fi
[ -s "$GENESIS" ] || die "genesis file $GENESIS is missing"
GENESIS_SHA="$(sha "$GENESIS")"
[ -f "$BIN" ] || die "$BIN is missing: the binary in the containers could not be compared with the one that was built; run test/devnet/build-image.sh"
BIN_SHA="$(sha "$BIN")"
# The genesis momentum this file must produce, computed here and not asked of a node.
OVERLAY=""
if [ "$VARIANT" = fast ]; then mkdir -p build && test/devnet/fast/overlay.sh "$ROOT" > build/fast-overlay.json || die "cannot write the fast overlay"; OVERLAY="-overlay build/fast-overlay.json"; fi
# shellcheck disable=SC2086
GENESIS_MOMENTUM="$(go run $OVERLAY ./test/devnet/genesishash "$GENESIS" 2>/dev/null | tail -1)"
case "$GENESIS_MOMENTUM" in *[!0-9a-f]*|'') die "cannot compute the genesis momentum of $GENESIS (go run ./test/devnet/genesishash)" ;; esac
[ ${#GENESIS_MOMENTUM} -eq 64 ] || die "cannot compute the genesis momentum of $GENESIS"

# 2. Remove the old chain, and check that it is gone. A leftover oldnode
# from the S-4 suite (test/e2e/sporkhalt_test.go) is not in this compose
# project, so `down` does not remove it: remove it, its sync-past helper
# and its volume explicitly, or the container check below fails and the
# next oldnode run would boot on a stale database.
docker compose --profile sync down -v >/dev/null 2>&1 || die "cannot remove the previous devnet (docker compose down failed)"
docker rm -f znnd-devnet-oldnode oldnode-sync-past >/dev/null 2>&1 || true
docker volume rm znnd-oldnode-data >/dev/null 2>&1 || true
left="$(docker ps -aq --filter name=znnd-devnet-)" || die "cannot list containers"
left="$(printf '%s' "$left" | grep -c . || true)"
[ "$left" = 0 ] || die "$left devnet containers survived the teardown"
PROJECT="$(docker compose config --format json | python3 -c 'import json,sys; print(json.load(sys.stdin)["name"])')" || die "cannot read the compose project name"
vols="$(docker volume ls -q)" || die "cannot list volumes"
left="$(printf '%s\n' "$vols" | grep -cE "^${PROJECT}_(pillar|pillar2|pillar3|rpc|observer)-data\$" || true)"
[ "$left" = 0 ] || die "$left devnet volumes survived the teardown; a node would keep its old genesis"
# The pins of the old chain go with it (test/devnet/pin-start.sh).
PINS="${DEVNET_PINS:-build/devnet-pins}"
# Only the pin files of this devnet's containers are removed, never the
# directory or anything else in it: DEVNET_PINS may point anywhere.
mkdir -p "$PINS" || die "cannot create the start pin directory $PINS"
for n in pillar pillar2 pillar3 rpc observer syncnode; do
  rm -f "$PINS/znnd-devnet-$n.pin" || die "cannot clear the start pin $PINS/znnd-devnet-$n.pin"
done

# 3. Start. The overlay pins the image and the genesis for every variant.
export DEVNET_GENESIS="$GENESIS" DEVNET_IMAGE="$IMAGE"
# shellcheck disable=SC2086
docker compose -f docker-compose.yml -f test/devnet/compose.variant.yml up -d --no-build $NODES >/dev/null 2>&1 || die "docker compose up failed"
# Pin each node's container ID and first start right away, at launch, while
# the node is on its first session (test/devnet/pin-start.sh refuses one that
# has restarted): down.sh checks the collected evidence against these before
# it removes the chain.
for n in $NODES; do
  test/devnet/pin-start.sh "$n" >/dev/null || die "cannot pin the first start of znnd-devnet-$n (test/devnet/pin-start.sh)"
done

# 4. Wait, with a limit, until every node answers RPC.
rpc() { docker exec "znnd-devnet-$1" wget -qO- -T 5 --post-data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$2\",\"params\":$3}" --header 'Content-Type: application/json' http://localhost:35997 2>/dev/null; }
# field <path...>: one value out of a JSON-RPC answer on stdin, by its path under
# "result"; fails on an error answer, a missing key or anything that is not JSON.
# Answers are parsed, never matched by pattern: a momentum contains other hashes
# and heights than its own.
field() { python3 -c '
import json, sys
r = json.load(sys.stdin)
if not isinstance(r, dict) or r.get("error") is not None or "result" not in r: sys.exit(1)
r = r["result"]
for k in sys.argv[1:]: r = r[int(k)] if isinstance(r, list) else r[k]
if isinstance(r, (dict, list)) or r is None or isinstance(r, bool): sys.exit(1)
print(r)' "$@" 2>/dev/null; }
# ishash <value>: 64 lowercase hex digits, not all zero.
ishash() { case "$1" in *[!0-9a-f]*) return 1 ;; esac; [ ${#1} -eq 64 ] && [ "$1" != "$ZERO" ]; }
# The genesis momentum has no parent: its previousHash is the zero hash.
ZERO=0000000000000000000000000000000000000000000000000000000000000000
deadline=$(( $(date +%s) + 180 ))
# Readiness only: each node answers RPC. Nothing here is a proof; the
# proof is section 5, which reads everything again.
for n in $NODES; do
  until rpc "$n" ledger.getFrontierMomentum '[]' | field height >/dev/null; do
    [ "$(date +%s)" -lt "$deadline" ] || die "node $n did not answer RPC within 180 s; see: docker logs znnd-devnet-$n"
    sleep 2
  done
done

# 5. Prove what is running: image, binary, genesis, and that the chain is new.
out=""
first_version=""   # its own name: an exported VERSION must not be taken as the first node's
for n in $NODES; do
  c="znnd-devnet-$n"
  img="$(docker inspect -f '{{.Image}}' "$c")"
  [ "$img" = "$IMAGE_ID" ] || die "$c runs image $img, want $IMAGE ($IMAGE_ID)"
  bsha="$(docker exec "$c" sha256sum /usr/local/bin/znnd | cut -d' ' -f1)"
  [ "$bsha" = "$BIN_SHA" ] || die "$c runs a znnd binary ($bsha) that is not $BIN ($BIN_SHA)"
  gsha="$(docker exec "$c" sha256sum /root/.znn/genesis.json | cut -d' ' -f1)"
  [ "$gsha" = "$GENESIS_SHA" ] || die "$c was seeded with genesis $gsha, want $GENESIS_SHA ($GENESIS)"
  # One frontier reply is checked and recorded: its height must be that of a new
  # chain, its hash a momentum hash, the node must serve that hash at that
  # height with the same parent, the parent must not be the momentum itself and
  # must be what the node serves one height below. Every answer about height 1,
  # the frontier's or the one below it, must be the genesis momentum computed
  # from the file. The frontier may move between the reads, so a mismatch is
  # read again.
  for try in 1 2 3; do
    fp=""; bx=""; fr="$(rpc "$n" ledger.getFrontierMomentum '[]')"
    fh="$(printf '%s' "$fr" | field height)" && fx="$(printf '%s' "$fr" | field hash)" || die "$c gave no usable frontier"
    case "$fh" in ''|*[!0-9]*) die "$c reports frontier height '$fh'" ;; esac
    [ "$fh" -ge 1 ] && [ "$fh" -le 30 ] || die "$c is at height $fh right after start: this is not a new chain"
    ishash "$fx" || die "$c reports frontier hash '$fx' at height $fh"
    own="$(rpc "$n" ledger.getMomentumsByHeight "[$fh,1]")"
    oh="$(printf '%s' "$own" | field list 0 height)" && ox="$(printf '%s' "$own" | field list 0 hash)" || die "$c gave no momentum at its frontier height $fh"
    ok=1
    { [ "$oh" = "$fh" ] && [ "$ox" = "$fx" ]; } || ok=0
    if [ "$ok" = 1 ] && [ "$fh" = 1 ]; then
      [ "$fx" = "$GENESIS_MOMENTUM" ] || die "$c reports frontier $fx at height 1; the genesis file gives $GENESIS_MOMENTUM"
      fp="$(printf '%s' "$fr" | field previousHash)" && [ "$fp" = "$ZERO" ] || die "$c reports genesis frontier $fx with parent '${fp:-}', not the zero hash"
      op="$(printf '%s' "$own" | field list 0 previousHash)" && [ "$op" = "$ZERO" ] || die "$c serves genesis momentum $fx by height with parent '${op:-}', not the zero hash"
    fi
    if [ "$ok" = 1 ] && [ "$fh" -gt 1 ]; then
      fp="$(printf '%s' "$fr" | field previousHash)" && ishash "$fp" || die "$c reports frontier $fx at height $fh with parent '${fp:-}'"
      [ "$fp" != "$fx" ] || die "$c reports frontier $fx at height $fh as its own parent"
      op="$(printf '%s' "$own" | field list 0 previousHash)" || die "$c gave no parent for its momentum at height $fh"
      [ "$op" = "$fp" ] || die "$c reports frontier $fx at height $fh with parent $fp, and the same momentum by height with parent $op"
      below="$(rpc "$n" ledger.getMomentumsByHeight "[$((fh - 1)),1]")"
      bh="$(printf '%s' "$below" | field list 0 height)" && bx="$(printf '%s' "$below" | field list 0 hash)" || die "$c gave no momentum at height $((fh - 1))"
      { [ "$bh" = "$((fh - 1))" ] && [ "$bx" = "$fp" ]; } || ok=0
      if [ "$ok" = 1 ] && [ "$bh" = 1 ]; then
        [ "$bx" = "$GENESIS_MOMENTUM" ] || die "$c serves $bx at height 1; the genesis file gives $GENESIS_MOMENTUM"
        bp="$(printf '%s' "$below" | field list 0 previousHash)" && [ "$bp" = "$ZERO" ] || die "$c serves genesis momentum $bx with parent '${bp:-}', not the zero hash"
      fi
    fi
    [ "$ok" = 1 ] && break
    [ "$try" -lt 3 ] || die "$c reports frontier $fx at height $fh (parent ${fp:-none}) and serves $ox (height $oh) there and ${bx:-nothing} below it"
    sleep 2
  done
  h="$fh"
  first="$(rpc "$n" ledger.getMomentumsByHeight '[1,1]')"
  g1h="$(printf '%s' "$first" | field list 0 height)" && g1="$(printf '%s' "$first" | field list 0 hash)" || die "$c gave no momentum at height 1"
  [ "$g1h" = 1 ] && ishash "$g1" || die "$c answered the request for height 1 with height $g1h"
  [ "$g1" = "$GENESIS_MOMENTUM" ] || die "$c has genesis momentum $g1; the genesis file gives $GENESIS_MOMENTUM"
  g1p="$(printf '%s' "$first" | field list 0 previousHash)" && [ "$g1p" = "$ZERO" ] || die "$c serves genesis momentum $g1 with parent '${g1p:-}', not the zero hash"
  ver="$(rpc "$n" stats.processInfo '[]' | field version)" || die "$c gave no version"
  [ "$ver" = "${first_version:=$ver}" ] || die "$c reports version $ver, the nodes before it $first_version"
  out="$out$n image=$img znnd=$bsha genesis-file=$gsha genesis-momentum=$g1 version=$ver height-at-start=$h frontier-hash=$fx frontier-parent=$fp genesis-parent=$g1p
"
done
report="variant=$VARIANT image=$IMAGE genesis=$GENESIS expected-genesis-momentum=$GENESIS_MOMENTUM
$out"
[ -z "$MANIFEST" ] || printf '%s' "$report" > "$MANIFEST"
printf '%s' "$report"
echo "devnet up on genesis variant '$VARIANT'"
