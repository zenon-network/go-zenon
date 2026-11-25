#!/bin/sh
# Stop the devnet and remove its chain state.
#
#   test/devnet/down.sh [log-directory]
#
# With a directory, the evidence is collected first and the chain is removed
# only if that worked: each of the five nodes' zenon.log and its container
# output, the final height and hash of every node and one momentum all five
# share at the lowest of those heights, every error and critical line, and a
# count of them by message, and every rollback a node logged (rollbacks.txt).
# A node whose log cannot be copied, or which does not answer, or whose frontier
# contradicts its own history, or a chain the five do not share, is an error,
# and nothing is removed. Heights must be positive and hashes 64 hex digits.
# The late joiner is optional and is recorded as absent when it was not started;
# a node found at the start (the joiner included) must give its evidence at the
# end, so a container that disappears or stops answering docker in between is
# an error too.
set -eu
cd "$(dirname "$0")/../.."
NODES="pillar pillar2 pillar3 rpc observer"
if [ -n "${1:-}" ]; then
  D="$1"; mkdir -p "$D"
  [ -z "$(ls -A "$D")" ] || { echo "down.sh: $D is not empty; use a new directory for each run" >&2; exit 1; }
  : > "$D/final.txt"
  # The inventory: the nodes found now. Each of them must give its evidence below.
  FOUND=""
  # Absent means that docker answered and said there is no such container;
  # any other failure to ask (a timeout, a daemon error) says nothing, and
  # stops the teardown. Only the late joiner may be absent.
  for n in $NODES syncnode; do
    c="znnd-devnet-$n"
    if ! err=$(docker inspect "$c" 2>&1 >/dev/null); then
      case "$err" in
        *"No such object"*|*"No such container"*|*"no such object"*|*"no such container"*)
          # A joiner pinned on this chain was started (up.sh clears the
          # pins of the previous one): gone now, it is lost, not absent.
          if [ "$n" = syncnode ] && [ -f "${DEVNET_PINS:-build/devnet-pins}/$c.pin" ]; then
            echo "down.sh: $c was started and pinned on this chain but no longer exists; its evidence is lost, nothing was removed" >&2; exit 1
          fi
          [ "$n" = syncnode ] && { echo "$n absent (not started)" >> "$D/final.txt"; continue; }
          echo "down.sh: container $c does not exist; nothing was removed" >&2; exit 1 ;;
        *) echo "down.sh: cannot ask docker about $c ($err); nothing was removed" >&2; exit 1 ;;
      esac
    fi
    st=$(docker inspect -f '{{.State.Status}} restarts={{.RestartCount}}' "$c") || {
      echo "down.sh: cannot inspect $c; nothing was removed" >&2; exit 1; }
    echo "$n state=$st" >> "$D/final.txt"
    FOUND="$FOUND $n"
  done
  # Where the chain ended. Every core node's frontier; at every height that is
  # some node's frontier, one hash on all the nodes that have reached it; on
  # each node, from just below the lowest frontier up to its own, one linked
  # chain (unique hashes, each previousHash the hash it serves one height
  # below, its frontier reply included); and a node asked twice about one
  # height must answer the same. Parsed as JSON.
  if ! python3 - $NODES >> "$D/final.txt" <<'PY'
import json, os, re, subprocess, sys, time
def call(node, method, params):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
    p = subprocess.run(["docker", "exec", "znnd-devnet-" + node, "wget", "-qO-", "-T", "10", "--post-data", body,
                        "--header", "Content-Type: application/json", "http://localhost:35997"],
                       capture_output=True, text=True, timeout=30)
    out = p.stdout
    # A failed docker exec is a failed read, whatever it printed.
    if p.returncode != 0:
        raise ValueError("%s %s: docker exec exit %d: %s" % (node, method, p.returncode, (p.stderr or out)[:200]))
    r = json.loads(out)
    if not isinstance(r, dict) or r.get("error") is not None or not isinstance(r.get("result"), dict):
        raise ValueError("%s %s: %s" % (node, method, out[:200]))
    return r["result"]
HASH = re.compile(r"[0-9a-f]{64}")
ZERO = "0" * 64
def ident(m):
    h, x = (m.get("height"), m.get("hash")) if isinstance(m, dict) else (None, None)
    if type(h) is not int or h < 1 or not isinstance(x, str) or not HASH.fullmatch(x) or x == "0" * 64:
        raise ValueError("not a momentum: %s" % json.dumps(m)[:200])
    return h, x
def parent(m):
    p = m.get("previousHash")
    if not isinstance(p, str) or not HASH.fullmatch(p):
        raise ValueError("no previous hash: %s" % json.dumps(m)[:200])
    return p
class Contradiction(Exception):
    pass
def checkpoint(nodes):
    lines, said, parents = [], {}, {}
    def at(node, height):
        page = call(node, "ledger.getMomentumsByHeight", [height, 1]).get("list")
        if not isinstance(page, list) or len(page) != 1 or ident(page[0])[0] != height:
            raise ValueError("%s has no momentum at height %d" % (node, height))
        x = page[0]["hash"]
        if said.setdefault((node, height), x) != x:
            raise Contradiction("CONTRADICTION: %s answered %s and then %s for height %d" % (node, said[(node, height)], x, height))
        p = parent(page[0])
        # The genesis momentum has no parent: its previousHash is the zero hash.
        if height == 1 and p != ZERO:
            raise Contradiction("CONTRADICTION: %s serves genesis momentum %s with parent %s, not the zero hash" % (node, x, p))
        if parents.setdefault((node, height), p) != p:
            raise Contradiction("CONTRADICTION: %s gave momentum %s at height %d the parent %s and then %s" % (node, x, height, parents[(node, height)], p))
        return x
    replies = {n: call(n, "ledger.getFrontierMomentum", []) for n in nodes}
    front = {n: ident(replies[n]) for n in nodes}
    for n in nodes:
        lines.append("%s frontier height=%d hash=%s" % ((n,) + front[n]))
    # A node must say the same about its own frontier when asked by height,
    # and the frontier reply's parent must be what it serves one height below.
    for n in nodes:
        own = at(n, front[n][0])
        if own != front[n][1]:
            raise Contradiction("CONTRADICTION: %s reports frontier %s at height %d and momentum %s at that height" % (n, front[n][1], front[n][0], own))
        if front[n][0] > 1 and parent(replies[n]) != at(n, front[n][0] - 1):
            raise Contradiction("CONTRADICTION: %s reports frontier %s with parent %s, but serves %s at height %d" % (
                n, front[n][1], parent(replies[n]), said[(n, front[n][0] - 1)], front[n][0] - 1))
        if front[n][0] == 1 and parent(replies[n]) != ZERO:
            raise Contradiction("CONTRADICTION: %s reports genesis frontier %s with parent %s, not the zero hash" % (n, front[n][1], parent(replies[n])))
    # On each node, from one below the lowest frontier up to its own: unique
    # hashes, and each momentum's parent is what the node serves one below.
    # Across nodes (below): at every one of those heights, the nodes that have
    # it agree on its hash and its parent.
    low = min(h for h, _ in front.values())
    for n in nodes:
        start = max(1, low - 1)
        seen = {}
        for h in range(start, front[n][0] + 1):
            x = at(n, h)
            if x in seen:
                raise Contradiction("CONTRADICTION: %s serves momentum %s at heights %d and %d" % (n, x, seen[x], h))
            seen[x] = h
            if h > start and parents[(n, h)] != said[(n, h - 1)]:
                raise Contradiction("CONTRADICTION: %s serves %s at height %d with parent %s, but %s at height %d" % (
                    n, x, h, parents[(n, h)], said[(n, h - 1)], h - 1))
    # At every height from one below the lowest frontier to the highest, all
    # nodes that have reached it must have one momentum with one parent: two
    # tips above a common height are a fork, and one hash with two parents, or
    # two blocks below one shared hash, is a contradiction between nodes.
    for h in range(max(1, low - 1), max(fh for fh, _ in front.values()) + 1):
        there = {n: at(n, h) for n in nodes if front[n][0] >= h}
        if len(set(there.values())) != 1:
            raise Contradiction("DISAGREEMENT at height %d: %s" % (h, json.dumps(there)))
        up = {n: parents[(n, h)] for n in there}
        if len(set(up.values())) != 1:
            raise Contradiction("DISAGREEMENT on the parent of %s at height %d: %s" % (there[nodes[0]] if nodes[0] in there else list(there.values())[0], h, json.dumps(up)))
    # And the whole reading once more: nothing a node said may have changed.
    for (n, h) in list(said):
        at(n, h)
    lines.append("agreement: all %d nodes have momentum %s at height %d, the lowest frontier, and no two nodes differ in hash or parent at any height from %d to %d, and each node's chain is linked from height %d to its frontier" % (
        len(nodes), said[(nodes[0], low)], low, max(1, low - 1), max(h for h, _ in front.values()), max(1, low - 1)))
    return lines
# The chain is alive while it is read, so a reorganisation between two reads
# can look like a contradiction once. Three tries, 12 s apart; the last one counts.
for attempt in (1, 2, 3):
    try:
        print("\n".join(checkpoint(sys.argv[1:])))
        sys.exit(0)
    except Contradiction as e:
        print("try %d: %s" % (attempt, e))
    except Exception as e:
        print("try %d: no final checkpoint: %s: %s" % (attempt, type(e).__name__, e))
    if attempt < 3:
        time.sleep(float(os.environ.get("DOWN_RETRY_SLEEP", "12")))
sys.exit(1)
PY
  then echo "down.sh: the nodes gave no common final momentum (see $D/final.txt); nothing was removed" >&2; exit 1; fi
  # A common cutoff through shutdown: once the checkpoint has passed, every
  # node of the inventory is stopped, and only then are its output and logs
  # copied, so everything a node logged, up to and including its shutdown, is
  # in them. A node that cannot be stopped stops the teardown; nodes stopped
  # before it stay stopped (docker compose --profile sync start brings them
  # back), and the chain is kept.
  for n in $FOUND; do
    c="znnd-devnet-$n"
    docker stop "$c" >/dev/null || {
      echo "down.sh: cannot stop $c; nothing was removed, and nodes stopped before it stay stopped" >&2; exit 1; }
    echo "$n stopped at $(date -u +%Y-%m-%dT%H:%M:%SZ) (the cutoff of its collected output and logs)" >> "$D/final.txt" ||
      { echo "down.sh: cannot record the stop of $c; nothing was removed" >&2; exit 1; }
  done
  # Every node of the inventory must give its output and log; one that can no
  # longer be inspected is an error, not an absence.
  for n in $FOUND; do
    c="znnd-devnet-$n"
    docker inspect "$c" >/dev/null 2>&1 || { echo "down.sh: $c was found at the start but cannot be inspected now; nothing was removed" >&2; exit 1; }
    docker logs "$c" > "$D/$n.stdout" 2>&1 || { echo "down.sh: cannot read the output of $c; nothing was removed" >&2; exit 1; }
    # The node rotates zenon.log at 100 MB and keeps older segments beside it
    # (zenon-<time>.log, common/logs.go). All of them are copied and joined,
    # oldest first, and the joined log must begin where this container's
    # znnd first began. The container's output is never rotated, and znnd
    # prints "current time is <time>" there at each start (app/cli.go); the
    # starts are reconciled in order: the joined log must begin with the
    # node's start line ("using znnd config"), hold as many start lines as
    # the output has starts, and each must be within 5 s of its counterpart.
    # The first start must also be the one pinned while the node ran
    # (test/devnet/pin-start.sh, by up.sh for the core nodes and by whoever
    # started the joiner), for this very container ID, so an output and a log
    # that both lack the first session fail too. A missing or foreign pin, a
    # missing session, a segment that is gone, or a listing or join that
    # fails, means the evidence is incomplete, and nothing is removed.
    pin="${DEVNET_PINS:-build/devnet-pins}/$c.pin"
    [ -f "$pin" ] || { echo "down.sh: $c has no start pin ($pin; test/devnet/pin-start.sh $n while it runs); nothing was removed" >&2; exit 1; }
    id=$(docker inspect -f '{{.Id}}' "$c") || { echo "down.sh: cannot read the container ID of $c; nothing was removed" >&2; exit 1; }
    pinid=$(sed -n 's/^id=//p' "$pin") && pinstart=$(sed -n 's/^start=//p' "$pin") || { echo "down.sh: cannot read $pin; nothing was removed" >&2; exit 1; }
    [ -n "$pinid" ] && [ "$pinid" = "$id" ] || { echo "down.sh: $pin is for container '$pinid', $c is $id; nothing was removed" >&2; exit 1; }
    [ -n "$pinstart" ] || { echo "down.sh: $pin records no first start; nothing was removed" >&2; exit 1; }
    restarts=$(docker inspect -f '{{.RestartCount}}' "$c") || { echo "down.sh: cannot read the restart count of $c; nothing was removed" >&2; exit 1; }
    rm -rf "${D:?}/${n:?}-logdir"
    if ! docker cp "$c:/root/.znn/log" "$D/$n-logdir" 2>/dev/null || [ ! -s "$D/$n-logdir/zenon.log" ]; then
      echo "down.sh: cannot copy the logs of $c; nothing was removed" >&2; exit 1
    fi
    if ! segs=$(python3 - "$D/$n-logdir" "$D/$n.log" "$D/$n.stdout" "$pinstart" "$restarts" <<'JOIN'
import os, re, sys, datetime
logdir, out, stdout, pinned, restarts = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5]
names = os.listdir(logdir)
segments = sorted(f for f in names if re.fullmatch(r"zenon-.+\.log", f))
if "zenon.log" not in names:
    sys.exit("no zenon.log among %s" % names)
with open(out, "wb") as dst:
    for f in segments + ["zenon.log"]:
        with open(os.path.join(logdir, f), "rb") as src:
            dst.write(src.read())
with open(out, "rb") as f:
    first = f.readline().decode("utf-8", "replace")
if 'msg="using znnd config"' not in first:
    sys.exit("the log does not begin with the node's start line: %r" % first[:120])
m = re.match(r"t=(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)([+-]\d{4}) ", first)
if not m:
    sys.exit("cannot read the time of the log's first line: %r" % first[:60])
start = datetime.datetime.strptime(m.group(1) + m.group(2), "%Y-%m-%dT%H:%M:%S%z")
starts, raw = [], []
with open(stdout, "rb") as f:
    for line in f:
        s = re.match(r"current time is (\d{4}-\d\d-\d\d \d\d:\d\d:\d\d)\s*$", line.decode("utf-8", "replace"))
        if s:
            raw.append(s.group(1))
            # Printed in the container's local time, the same zone as the
            # log's t= (its offset is taken from the log line).
            starts.append(datetime.datetime.strptime(s.group(1) + m.group(2), "%Y-%m-%d %H:%M:%S%z"))
if not starts:
    sys.exit("the container's output has no start of znnd (current time is ...)")
# Docker's own record: every restart it made is a session the output and
# the log must hold (a manual restart is not counted by docker, so this is a
# floor).
if not restarts.isdigit() or len(starts) < int(restarts) + 1:
    sys.exit("docker restarted the container %s time(s), and its output has %d start(s) of znnd: a session is missing" % (restarts, len(starts)))
# The output's first start must be the one pinned while the node ran.
if raw[0] != pinned:
    sys.exit("the container's output begins with the start %s, the pin taken while it ran says %s: the first session is not in the output" % (raw[0], pinned))
# Every start line of the joined log, in order.
logstarts = []
with open(out, "rb") as f:
    for line in f:
        line = line.decode("utf-8", "replace")
        if 'msg="using znnd config"' not in line:
            continue
        t = re.match(r"t=(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)([+-]\d{4}) ", line)
        if not t:
            sys.exit("cannot read the time of a start line of the log: %r" % line[:60])
        logstarts.append(datetime.datetime.strptime(t.group(1) + t.group(2), "%Y-%m-%dT%H:%M:%S%z"))
if len(logstarts) != len(starts):
    sys.exit("the container's output has %d start(s) of znnd and the log %d: a session's log is missing" % (len(starts), len(logstarts)))
for k, (o, l) in enumerate(zip(starts, logstarts)):
    if abs((o - l).total_seconds()) > 5:
        sys.exit("start %d is at %s in the container's output and %s in the log: they do not pair up" % (k + 1, o.isoformat(), l.isoformat()))
print(len(segments))
JOIN
    ); then
      echo "down.sh: the logs of $c could not be listed, joined or shown to be complete (see above); nothing was removed" >&2; exit 1
    fi
    echo "$n log segments: $segs rotated + zenon.log; first start $pinstart as pinned for container $id" >> "$D/final.txt"
    rm -rf "${D:?}/${n:?}-logdir"
  done
  # What is read from the copied logs must be read in full: grep's exit 1
  # (no line matched) is a result, anything else is a failure to read, and
  # the chain is then kept, as when a log could not be copied.
  readfail() { echo "down.sh: $1; nothing was removed" >&2; exit 1; }
  rc=0; grep -hE 'lvl=(eror|crit)' "$D"/*.log > "$D/error-lines.txt" || rc=$?
  [ $rc -le 1 ] || readfail "reading the error lines of the logs failed (grep exit $rc)"
  # The same line with hashes, addresses, heights, times and pointers removed, counted.
  sed -E 's/^t=[^ ]+ //; s/[0-9a-f]{64}/HASH/g; s/z1[a-z0-9]{38}/ADDR/g; s/[Hh]eight[:=][0-9]+/height=N/g; s/0x[0-9a-f]+/PTR/g; s/[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9:]+ \+0000 UTC/TIME/g' "$D/error-lines.txt" > "$D/error-lines.blank" ||
    readfail "blanking the error lines failed"
  # One command at a time, each checked: sh has no pipefail, and a failed
  # stage in a pipeline would otherwise leave an empty count that reads as none.
  sort "$D/error-lines.blank" > "$D/error-lines.sorted" || readfail "sorting the error lines failed"
  uniq -c "$D/error-lines.sorted" > "$D/error-lines.counted" || readfail "counting the error lines failed"
  sort -rn "$D/error-lines.counted" > "$D/error-histogram.txt" || readfail "ordering the error counts failed"
  # A count that does not add up to the lines it counted is a failed count.
  n1=$(wc -l < "$D/error-lines.txt") || readfail "counting the error lines failed"
  n2=$(awk '{ s += $1 } END { print s + 0 }' "$D/error-histogram.txt") || readfail "summing the error counts failed"
  [ "$n1" -eq "$n2" ] || readfail "the error histogram counts $n2 lines, the error lines are $n1"
  rm -f "$D/error-lines.blank" "$D/error-lines.sorted" "$D/error-lines.counted"
  # Cold start and reorganisations, as a record of their own: every rollback a
  # node logged, with the time and the height it went back to. None is normal;
  # some at start-up have been seen. It is evidence, not a verdict.
  : > "$D/rollbacks.txt"
  for n in $FOUND; do
    rc=0; grep 'msg="rollbacking momentums"' "$D/$n.log" > "$D/rollbacks.$n" || rc=$?
    [ $rc -le 1 ] || readfail "reading the rollbacks in the log of $n failed (grep exit $rc)"
    sed -E "s/^t=([^ ]+) .*Height:([0-9]+)\\}.*/$n\t\1\tto-height=\2/" "$D/rollbacks.$n" >> "$D/rollbacks.txt" || readfail "reading the rollbacks of $n failed"
    rm -f "$D/rollbacks.$n"
  done
  grep '^agreement' "$D/final.txt"
  echo "rollbacks logged: $(wc -l < "$D/rollbacks.txt" | tr -d ' ') ($(cut -f1 "$D/rollbacks.txt" | sort | uniq -c | awk '{printf "%s%s %s", s, $2, $1; s=", "}')); see $D/rollbacks.txt"
  echo "error and critical lines: $(wc -l < "$D/error-lines.txt" | tr -d ' ') in $(ls "$D"/*.log | wc -l | tr -d ' ') logs; by message in $D/error-histogram.txt"
fi
# The nodes were stopped above, after the checkpoint; compose removes them
# with their volumes.
docker compose --profile sync down -v
