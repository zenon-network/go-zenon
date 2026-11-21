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
# The rules whose mutants must each turn a witness False (step 3).
WITNESSED="ledger"
RULES="${RULES:-ledger escrow}"
# each rule once, in the order given
RULES="$(printf '%s\n' $RULES | awk '!seen[$0]++' | tr '\n' ' ')"
SCRATCH="$HERE/.scratch"
OUT="${OUT:-$SCRATCH/out}"
mkdir -p "$OUT" "$SCRATCH"
OUT="$(cd "$OUT" && pwd)"
fail=0

bend() { # bend <image> <dir mounted at /work> <args...>
  img="$1"; dir="$2"; shift 2
  docker run --rm --network none -v "$dir":/work:ro "$img" "$@" 2>&1
}

# Print one line as it is. (echo would rewrite backslash sequences, and
# what is printed here may come from Bend's output.)
say() { printf '%s\n' "$1"; }

docker image inspect "$IMAGE" >/dev/null 2>&1 || docker build -t "$IMAGE" --build-arg BEND_VERSION="$BEND_VERSION" "$HERE"
echo "commit  $(git -C "$ROOT" rev-parse HEAD)"
echo "bend    $(bend "$IMAGE" "$HERE" version)  image $(docker image inspect -f '{{.Id}}' "$IMAGE")"
for rule in $RULES; do echo "laws    $(grep -c '^law ' "$HERE/$rule/LAWS.bend") in $rule/LAWS.bend"; done

echo
echo "== 1. proofs"
: > "$OUT/proofs.log"
for rule in $RULES; do
  res="$(bend "$IMAGE" "$HERE" "$rule/PROOF.bend" --check-only)"
  say "$rule: $res" | tee -a "$OUT/proofs.log"
  say "$res" | grep -q '^ALL PROOFS CHECK' || { echo "FAIL: proofs of $rule"; fail=1; }
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
    say "$rule: $res" | tee -a "$OUT/verdict.log"
    say "$res" | grep -q '^ALL PROOFS CHECK' || { echo "FAIL: verdict of $rule"; fail=1; }
  done
fi

echo
echo "== 3. mutants"
# mutants.log has one line per mutant; mutants-detail.log has, for each, the
# checker's report of the first proof that broke: where, what the proof
# needed (expected) and what the faulty model gives (observed).
#
# A proof that no longer checks does not show that a law is false: it may
# only no longer fit the changed text. So a rule with a witness.bend is held
# to more: each mutant has a witness there, one computed case of a named
# law, which must be True on the model and False with the mutant in it.
# This step keeps its files in a directory of its own, so that two runs do
# not take each other's, with short names whatever the mutants are called.
S3="$(mktemp -d "$SCRATCH/step3.XXXXXX")"
# The two logs are written there as well and put into $OUT when the step is
# done, so that the count below is of this run's lines only.
: > "$S3/mutants.log"; : > "$S3/mutants-detail.log"
listed=0
for rule in $RULES; do
  # The list goes to a file first: in a pipeline the status of the listing
  # would be lost, and a rule whose mutants could not be listed would pass.
  # mutants.py refuses a list with a name used twice or a description that
  # is not one plain line.
  list="$S3/list.tsv"
  if ! python3 "$HERE/mutants.py" --list "$rule" > "$list" || [ ! -s "$list" ]; then
    say "MISSED   $rule: its mutants could not be listed, or it has none" | tee -a "$S3/mutants.log"
    fail=1
    continue
  fi
  # Whether a rule must have witnesses is said here, by name, and not read
  # off the presence of the file: a witness.bend left out of a change would
  # otherwise put its rule back on the weaker check without a word. A rule
  # that has the file and is not named is a mistake in this script, and
  # every one of its mutants is MISSED.
  mode=proofs-only
  case " $WITNESSED " in *" $rule "*) mode=witnessed;; esac
  if [ "$mode" = proofs-only ] && [ -f "$HERE/$rule/witness.bend" ]; then mode=unnamed; fi
  model="$S3/model.txt"; : > "$model"; rm -f "$S3/model.ok"
  if [ "$mode" = witnessed ]; then
    # On the model the witnesses must run to the end, and each mutant must
    # have exactly one answer there, which is True (judged per mutant).
    if [ ! -f "$HERE/$rule/witness.bend" ]; then
      say "$rule/witness.bend is missing" > "$model"
    elif bend "$IMAGE" "$HERE" "$rule/witness.bend" > "$model"; then
      : > "$S3/model.ok"
    fi
    [ -f "$S3/model.ok" ] || { say "=== $rule: witness.bend did not run on the model"; cat "$model"; say ""; } >> "$S3/mutants-detail.log"
  fi
  # (the second test takes a last row that does not end in a newline)
  while IFS="$(printf '\t')" read -r name what || [ -n "$name" ]; do
    listed=$((listed+1))
    # a fresh path for each mutant: the container's view of a path that is
    # removed and made again can lag behind
    d="$S3/m$listed"; mkdir -p "$d/work"
    say "$mode" > "$d/mode"; cp "$model" "$d/model.txt"; [ ! -f "$S3/model.ok" ] || : > "$d/model.ok"
    : > "$d/proofs.txt"; : > "$d/witness.txt"
    # every rule directory goes into the copy, since a rule may import another
    # A copy that fails is that mutant's failure, not the end of the list.
    copied=1
    for src in "$HERE"/*/LAWS.bend; do r="$(basename "$(dirname "$src")")"; mkdir -p "$d/work/$r" && cp "$HERE/$r"/*.bend "$d/work/$r/" 2>> "$d/proofs.txt" || copied=0; done
    if [ "$copied" = 1 ] && python3 "$HERE/mutants.py" --apply "$rule" "$name" "$d/work" > "$d/proofs.txt" 2>&1; then
      : > "$d/applied"
      bend "$IMAGE" "$d/work" "$rule/PROOF.bend" --check-only > "$d/proofs.txt" || true
      if [ "$mode" = witnessed ] && bend "$IMAGE" "$d/work" "$rule/witness.bend" > "$d/witness.txt"; then : > "$d/witness.ok"; fi
    fi
    # The verdict is made by mutants.py from the files as Bend wrote them,
    # byte for byte: one line, and a status that is 0 only for "caught".
    if line="$(python3 "$HERE/mutants.py" --judge "$rule" "$name" "$d")"; then :; else fail=1; fi
    case "$line" in "caught   $rule/$name: "*) ;; "MISSED   $rule/$name: "*) fail=1;; *) line="MISSED   $rule/$name: no verdict could be made"; fail=1;; esac
    say "$line"; say "$line" >> "$S3/mutants.log"
    { say "=== $rule/$name: $what"
      (cd "$d/work" && diff "$HERE/$rule" "$rule" | grep '^[<>]' | sed 's/^</  model :/; s/^>/  mutant:/') || true
      cat "$d/proofs.txt" || true
      if [ "$mode" = witnessed ]; then say "witness output on the model:"; cat "$d/model.txt" || true; say "witness output with the mutant:"; cat "$d/witness.txt" || true; fi
      say ""; } >> "$S3/mutants-detail.log"
    rm -rf "$d"
  done < "$list"
done
# Every listed mutant has one line, each line is a catch, and nothing above
# set the failure.
lines="$(grep -c '' "$S3/mutants.log" || true)"; caught="$(grep -c '^caught   ' "$S3/mutants.log" || true)"
# Each log goes into $OUT under a name of this run's own and is then renamed
# into place, so that a second run writing to the same $OUT at the same
# moment leaves whole files: the last one to finish.
for f in mutants.log mutants-detail.log; do
  if cp "$S3/$f" "$OUT/.$f.$$" && mv -f "$OUT/.$f.$$" "$OUT/$f"; then :; else echo "FAIL: mutants ($f could not be written to $OUT)"; fail=1; fi
done
rm -rf "$S3"
if [ "$listed" = 0 ] || [ "$lines" != "$listed" ] || [ "$caught" != "$listed" ]; then echo "FAIL: mutants ($caught caught of $listed listed, $lines lines)"; fail=1; fi

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
# Whole numbers only, and at least one shard: with none, no sequence would
# run and nothing would be there to fail.
# Two digits at most, so that the comparisons below are within what every
# shell can count: a number too long for the shell made them all false and
# the loops run no shard.
for v in "$DEPTH" "$SHARDS"; do
  # No leading zero either: "04" would count as four shards and then not
  # equal the 4 that were started.
  case "$v" in ""|*[!0-9]*|???*|0?*) echo "DEPTH and SHARDS must be whole numbers below 100, written without a leading zero"; exit 1;; esac
done
if [ "$DEPTH" != 0 ] && [ "$SHARDS" -lt 1 ]; then echo "SHARDS must be at least 1"; exit 1; fi
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
  shards_ok=1; started=0
  for pid in $pids; do
    started=$((started + 1))
    wait "$pid" || shards_ok=0
  done
  [ "$started" = "$SHARDS" ] || { echo "started $started of $SHARDS shards"; shards_ok=0; }
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
