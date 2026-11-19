#!/bin/sh
# Check the Bend rules of test/bend and compare them with the node.
# How to read the result: docs/BEND_LAWS.md.
#
#   test/bend/run.sh                  # all six steps, for every rule
#   RULES=escrow test/bend/run.sh     # steps 1 to 3 for one rule (step 4 always runs all)
#   OUT=dir test/bend/run.sh          # keep the logs in dir
#   NODE=0 test/bend/run.sh           # skip step 4
#   VERDICT=0 test/bend/run.sh        # skip the second check (no Lean image needed)
#   FAULTS=0 test/bend/run.sh         # skip step 5
#   DEPTH=4 SHARDS=6 test/bend/run.sh # step 6 to length 4, in six processes (default DEPTH=2, SHARDS=1)
#
# Steps:
#   1. proofs    bend PROOF.bend must print ALL PROOFS CHECK
#   2. verdict   the same proofs, rechecked by Bend's kernel that is proven in Lean
#   3. mutants   each deliberate fault put into a copy of the model must make
#                the proofs fail; this shows the laws are not empty.
#                mutants-detail.log shows where each one breaks
#   4. node      go test -tags bendrules: the model and the real chain,
#                verifier and VM must agree on the same blocks
#   5. faults    each deliberate fault put into a copy of the node's Go code
#                (node_faults.py, through go test -overlay) must make the
#                comparison of step 4 fail; this shows step 4 would notice
#   6. exhaust   every sequence of up to DEPTH steps from a short list, on
#                node and model: within that bound, no sampling
#
# Bend runs in a container without a network and with the rule directory
# mounted read-only. Nothing is installed on the host.
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
BEND_VERSION="${BEND_VERSION:-2.0.40}"
IMAGE="${BEND_IMAGE:-go-zenon-bend:$BEND_VERSION}"
VIMAGE="${BEND_VERDICT_IMAGE:-$IMAGE-verdict}"
VERDICT="${VERDICT:-1}"
RULES="${RULES:-ledger escrow}"
SCRATCH="$HERE/.scratch"
OUT="${OUT:-$SCRATCH/out}"
mkdir -p "$OUT" "$SCRATCH"
OUT="$(cd "$OUT" && pwd)"
fail=0

bend() { # bend <image> <dir mounted at /work> <args...>
  img="$1"; dir="$2"; shift 2
  docker run --rm --network none -v "$dir":/work:ro "$img" "$@" 2>&1
}

docker image inspect "$IMAGE" >/dev/null 2>&1 || docker build -t "$IMAGE" --build-arg BEND_VERSION="$BEND_VERSION" "$HERE"
echo "commit  $(git -C "$ROOT" rev-parse HEAD)"
echo "bend    $(bend "$IMAGE" "$HERE" version)  image $(docker image inspect -f '{{.Id}}' "$IMAGE")"
for rule in $RULES; do echo "laws    $(grep -c '^law ' "$HERE/$rule/LAWS.bend") in $rule/LAWS.bend"; done

echo
echo "== 1. proofs"
: > "$OUT/proofs.log"
for rule in $RULES; do
  res="$(bend "$IMAGE" "$HERE" "$rule/PROOF.bend" --check-only)"
  echo "$rule: $res" | tee -a "$OUT/proofs.log"
  echo "$res" | grep -q '^ALL PROOFS CHECK' || { echo "FAIL: proofs of $rule"; fail=1; }
done

echo
echo "== 2. verdict"
: > "$OUT/verdict.log"
if [ "$VERDICT" = 0 ]; then
  echo "skipped (VERDICT=0)" | tee "$OUT/verdict.log"
else
  docker image inspect "$VIMAGE" >/dev/null 2>&1 || docker build -t "$VIMAGE" --build-arg BASE="$IMAGE" -f "$HERE/Dockerfile.verdict" "$HERE"
  for rule in $RULES; do
    res="$(bend "$VIMAGE" "$HERE" "$rule/PROOF.bend" --verdict)"
    echo "$rule: $res" | tee -a "$OUT/verdict.log"
    echo "$res" | grep -q '^ALL PROOFS CHECK' || { echo "FAIL: verdict of $rule"; fail=1; }
  done
fi

echo
echo "== 3. mutants"
# mutants.log has one line per mutant; mutants-detail.log has, for each, the
# checker's report of the first proof that broke: where, what the proof
# needed (expected) and what the faulty model gives (observed).
: > "$OUT/mutants.log"; : > "$OUT/mutants-detail.log"
for rule in $RULES; do
  python3 "$HERE/mutants.py" --list "$rule" | while IFS="$(printf '\t')" read -r name what; do
    dir="$SCRATCH/mutant-$rule-$name"
    rm -rf "$dir"; mkdir -p "$dir"
    # every rule directory goes into the copy, since a rule may import another
    for src in "$HERE"/*/LAWS.bend; do r="$(basename "$(dirname "$src")")"; mkdir -p "$dir/$r"; cp "$HERE/$r"/*.bend "$dir/$r/"; done
    python3 "$HERE/mutants.py" --apply "$rule" "$name" "$dir"
    res="$(bend "$IMAGE" "$dir" "$rule/PROOF.bend" --check-only || true)"
    where="$(echo "$res" | sed -n 's/^Location: //p' | head -1)"
    if echo "$res" | grep -q '^SOME PROOFS FAIL'; then
      echo "caught   $rule/$name: $what; first proof to break: $where" | tee -a "$OUT/mutants.log"
    else
      echo "MISSED   $rule/$name: $what" | tee -a "$OUT/mutants.log"
    fi
    { echo "=== $rule/$name: $what"; (cd "$dir" && diff "$HERE/$rule" "$rule" | grep '^[<>]' | sed 's/^</  model :/; s/^>/  mutant:/'); echo "$res"; echo; } >> "$OUT/mutants-detail.log"
    rm -rf "$dir"
  done
done
if grep -q '^MISSED' "$OUT/mutants.log" || ! grep -q '^caught' "$OUT/mutants.log"; then echo "FAIL: mutants"; fail=1; fi

echo
echo "== 4. node"
# The mock chain logs every block; node.log keeps the lines of the test.
if [ "${NODE:-1}" = 0 ]; then
  echo "skipped (NODE=0)" | tee "$OUT/node.log"
elif (cd "$ROOT" && BEND_IMAGE="$IMAGE" go test -tags bendrules ./test/bend/ -skip '^TestExhaustive' -count=1 -v -timeout 30m) > "$SCRATCH/node-full.log" 2>&1; then
  grep -E '^(=== |--- |ok|PASS|FAIL)|^ +[a-z_]+_test\.go:[0-9]+: ' "$SCRATCH/node-full.log" | sed 's/^ *[a-z_]*_test\.go:[0-9]*: //' > "$OUT/node.log"
  grep -E '^(--- |ok|FAIL)|scenarios|refused +[0-9]+ times|accepted +[0-9]+|refunded +[0-9]+|paid zero|rounds:' "$OUT/node.log"
else
  cp "$SCRATCH/node-full.log" "$OUT/node.log"; cat "$OUT/node.log"; echo "FAIL: node"; fail=1
fi

echo
echo "== 5. node faults"
if [ "${FAULTS:-1}" = 0 ]; then
  echo "skipped (FAULTS=0)" | tee "$OUT/node-faults.log"
else
  python3 "$HERE/node_faults.py" --run "$OUT" || { echo "FAIL: node faults"; fail=1; }
fi

echo
echo "== 6. exhaustive"
DEPTH="${DEPTH:-2}"; SHARDS="${SHARDS:-1}"
if [ "$DEPTH" = 0 ]; then
  echo "skipped (DEPTH=0)" | tee "$OUT/exhaustive.log"
else
  (cd "$ROOT" && go test -tags bendrules -c -o "$SCRATCH/bend.test" ./test/bend/)
  # One process per shard. Each writes "exit N" as its last line whatever
  # happens to the test binary, and its own exit status is collected below:
  # a shard that is killed part way must not leave a log that reads as a pass.
  i=0; pids=""
  while [ "$i" -lt "$SHARDS" ]; do
    (
      cd "$HERE"
      rc=0
      BEND_IMAGE="$IMAGE" BEND_DEPTH="$DEPTH" BEND_SHARD="$i/$SHARDS" "$SCRATCH/bend.test" -test.run '^TestExhaustive' -test.v -test.timeout 12h > "$SCRATCH/exhaustive-$i.log" 2>&1 || rc=$?
      echo "exit $rc" >> "$SCRATCH/exhaustive-$i.log"
      exit "$rc"
    ) &
    pids="$pids $!"
    i=$((i + 1))
  done
  shards_ok=1
  for pid in $pids; do
    wait "$pid" || shards_ok=0
  done
  : > "$OUT/exhaustive.log"
  i=0
  while [ "$i" -lt "$SHARDS" ]; do
    grep -E '^(--- |exit )|every sequence|disagree|node:  |model: |outside the rule|running the model' "$SCRATCH/exhaustive-$i.log" | sed "s/^ *[a-z_]*_test\\.go:[0-9]*: //; s/^/shard $i: /" >> "$OUT/exhaustive.log"
    # a shard counts only when it ended with exit 0 and every exhaustive test in it passed
    tail -1 "$SCRATCH/exhaustive-$i.log" | grep -q '^exit 0$' || shards_ok=0
    want="$(grep -c '^func TestExhaustive' "$HERE/exhaustive_test.go")"
    have="$(grep -c -- '^--- PASS: TestExhaustive' "$SCRATCH/exhaustive-$i.log" || true)"
    [ "$have" = "$want" ] || { echo "shard $i: $have of $want exhaustive tests passed" | tee -a "$OUT/exhaustive.log"; shards_ok=0; }
    i=$((i + 1))
  done
  python3 - "$OUT/exhaustive.log" "$DEPTH" <<'PY'
import re, sys
total, differ, tests = {}, {}, {}
for line in open(sys.argv[1]):
    m = re.search(r"every sequence of up to (\d+) of (\d+) steps: (\d+) sequences, (\d+) different", line)
    if m:
        last = (m.group(1), m.group(2), int(m.group(3)), int(m.group(4)))
    m = re.search(r"--- (PASS|FAIL): (TestExhaustive\w+)", line)
    if m:
        name = m.group(2)
        d, n, seqs, diff = last
        t = tests.setdefault(name, [d, n, 0, 0, True])
        t[2] += seqs; t[3] += diff; t[4] = t[4] and m.group(1) == "PASS"
for name, (d, n, seqs, diff, ok) in tests.items():
    print("%-22s every sequence of up to %s of %s steps: %6d sequences, %d different  %s" % (name, d, n, seqs, diff, "PASS" if ok else "FAIL"))
PY
  if [ "$shards_ok" = 0 ] || grep -q -E '^shard [0-9]+: (--- FAIL|exit [1-9])' "$OUT/exhaustive.log"; then echo "FAIL: exhaustive"; fail=1; fi
fi

echo
if [ "$fail" = 0 ]; then echo "PASS  logs in $OUT"; else echo "FAIL  logs in $OUT"; fi
exit "$fail"
