#!/bin/sh
# Pin a devnet node's identity and its first start, taken while it runs:
#
#   test/devnet/pin-start.sh <node>        (pillar, pillar2, pillar3, rpc, observer, syncnode)
#
# Writes build/devnet-pins/znnd-devnet-<node>.pin (DEVNET_PINS overrides the
# directory) with the container's ID and the first "current time is ..." line
# znnd printed to the container's output (app/cli.go). up.sh pins the five
# core nodes; whoever starts the late joiner pins it right after starting it.
# down.sh requires every node it collects to carry a pin with the same
# container ID and the same first start before it removes the chain, so a log
# and an output that both lack the first session cannot pass as complete.
#
# The pin certifies the first session only if the container has not
# started twice by then: docker must report no restarts and a start within
# 60 s of the container's creation, read after the output's first start was
# found. A container that restarted before it was pinned is refused, since
# its first session can no longer be told from a later one.
#
# A pin is written once per container: an existing pin for another container
# ID is refused (remove the old chain with down.sh, or up.sh clears the pins),
# and an existing pin for the same container must agree with what it records.
set -eu
cd "$(dirname "$0")/../.."
n="${1:?usage: test/devnet/pin-start.sh <node>}"
c="znnd-devnet-$n"
PINS="${DEVNET_PINS:-build/devnet-pins}"
mkdir -p "$PINS"
id=$(docker inspect -f '{{.Id}}' "$c") || { echo "pin-start.sh: cannot inspect $c" >&2; exit 1; }
[ -n "$id" ] || { echo "pin-start.sh: $c has no container ID" >&2; exit 1; }
deadline=$(( $(date +%s) + ${PIN_WAIT:-120} ))
while :; do
  out=$(docker logs "$c" 2>&1) || { echo "pin-start.sh: cannot read the output of $c" >&2; exit 1; }
  first=$(printf '%s\n' "$out" | sed -n 's/^current time is \([0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]\)[[:space:]]*$/\1/p' | head -1)
  [ -n "$first" ] && break
  [ "$(date +%s)" -lt "$deadline" ] || { echo "pin-start.sh: $c printed no start of znnd (current time is ...) within ${PIN_WAIT:-120} s" >&2; exit 1; }
  sleep 1
done
pin="$PINS/$c.pin"
if [ -f "$pin" ]; then
  old=$(sed -n 's/^id=//p' "$pin")
  oldstart=$(sed -n 's/^start=//p' "$pin")
  [ "$old" = "$id" ] || { echo "pin-start.sh: $pin is for container $old, not $id; remove the old chain first (down.sh, or up.sh)" >&2; exit 1; }
  [ "$oldstart" = "$first" ] || { echo "pin-start.sh: $pin records the first start $oldstart, the output now shows $first" >&2; exit 1; }
  echo "pin-start.sh: $c already pinned: $first"
  exit 0
fi
life=$(docker inspect -f '{{.Created}} {{.State.StartedAt}} {{.RestartCount}}' "$c") || { echo "pin-start.sh: cannot inspect the lifecycle of $c" >&2; exit 1; }
python3 - $life <<'LIFE' || { echo "pin-start.sh: $c is not on its first session ($life); its first start cannot be pinned" >&2; exit 1; }
import datetime, re, sys
created, started, restarts = sys.argv[1], sys.argv[2], sys.argv[3]
def at(s):
    m = re.match(r"(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(\.\d+)?Z$", s)
    if not m:
        sys.exit("cannot read the time %s" % s)
    return datetime.datetime.strptime(m.group(1), "%Y-%m-%dT%H:%M:%S")
gap = (at(started) - at(created)).total_seconds()
if restarts != "0" or gap < 0 or gap > 60:
    sys.exit("restarts %s, started %d s after it was created" % (restarts, gap))
LIFE
printf 'id=%s\nstart=%s\n' "$id" "$first" > "$pin.tmp" && mv "$pin.tmp" "$pin" || { echo "pin-start.sh: cannot write $pin" >&2; exit 1; }
echo "pin-start.sh: pinned $c: first start $first"
