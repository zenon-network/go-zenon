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
WITNESSED="ledger escrow"
# The tests of step 4 and of step 6, by name. The source must have exactly
# these, and each must be seen to pass: a test that is renamed, removed or
# added without a word here fails the step. For step 6 each has the number
# of steps in its list and how far past DEPTH it goes.
NODE_TESTS="TestLedgerModelMatchesNode TestReplacementSpendsOnce TestRollbackKeepsTotal TestEscrowModelMatchesHtlc TestEscrowModelMatchesPtlc TestEscrowModelMatchesStake TestEscrowModelMatchesFusion"
# Also the letter its scenarios' names begin with.
EXHAUSTIVE_TESTS="TestExhaustiveLedger:11:0:x TestExhaustiveLedgerAfterSend:11:0:y TestExhaustiveHtlc:9:0:x TestExhaustivePtlc:9:0:x TestExhaustiveStake:6:1:x TestExhaustiveFusion:6:1:x"
# The tests the go tool finds in the package, one name a line: its own
# list, not a reading of the source, which could miss a declaration the
# tool accepts.
# What the tool would run as a test is listed (tests, fuzz targets,
# examples; a benchmark is run by no step here), once, when step 4 or 6
# first asks. Only what the tool prints as its list is read, not what it
# says beside it on the error stream, and the listing must itself end
# well: one that fails gives a line that equals no expected name.
tests_in_package() {
  if [ ! -f "$RUN/list.status" ]; then
    if await go test -C "$ROOT" -tags bendrules -json=false -list '.*' ./test/bend/ > "$RUN/list.txt" 2> "$RUN/list.err"; then echo 0 > "$RUN/list.status"; else echo 1 > "$RUN/list.status"; cat "$RUN/list.err" >&2; fi
  fi
  if [ "$(cat "$RUN/list.status")" = 0 ]; then grep -E '^(Test|Fuzz|Example)' "$RUN/list.txt" | sort; else echo "(the listing failed)"; fi
}
# Whatever a log shows as passed at the top level, one name a line.
# (Each once: with -cpu in GOFLAGS the tool runs a test several times.)
passed_in() { sed -n 's/^--- PASS: \([^ ]*\) .*/\1/p' "$1" | sort -u; }
RULES="${RULES:-ledger escrow}"
# each rule once, in the order given
RULES="$(printf '%s\n' $RULES | awk '!seen[$0]++' | tr '\n' ' ')"
SCRATCH="$HERE/.scratch"
OUT="${OUT:-$SCRATCH/out}"
mkdir -p "$OUT" "$SCRATCH"
# Everything this run writes while it works goes into a directory of its
# own, so that two runs at once do not read or remove each other's files.
RUN="$(mktemp -d "$SCRATCH/run.XXXXXX")"
# Removed when the script ends, also when it is interrupted: the shards of
# step 6 are stopped first, so that none writes into a directory that is gone.
pids=""
trap 'rm -rf "$RUN"' EXIT
# Told to stop: everything this script started is stopped first, so that
# nothing goes on writing into a directory that is gone. Every long step is
# started in a process group of its own (await, below), and this script
# keeps the list of them: stopping a group stops the go tool, its test
# binary and the client of a model's container together, without having to
# look the processes up.
active=""; launching=0; pending=0
stop_all() {
  # between starting a command and noting it, only remember the signal
  if [ "$launching" = 1 ]; then pending=1; return 0; fi
  trap "" INT TERM HUP
  # Asked again until none is left: a command that was only just started
  # may not have made its group yet, and is then reached as a process
  # first and as a group the next time round. (The two forms are the ways
  # shells take a process group as the target.)
  tries=0
  while [ "$tries" -lt 50 ]; do
    left=0
    for p in $active; do
      kill -TERM "-$p" 2>/dev/null || kill -TERM -- "-$p" 2>/dev/null || true
      kill -TERM "$p" 2>/dev/null && left=1
    done
    [ "$left" = 1 ] || break
    tries=$((tries + 1)); python3 -c 'import time; time.sleep(0.1)'
  done
  wait 2>/dev/null || true
  exit 130
}
trap stop_all INT TERM HUP
# ingroup starts a command as the leader of a new process group, in the
# background, and notes it. await also waits for it: the shell acts on a
# signal while it waits, and not while a foreground command runs. A command
# started this way reads nothing: what it needs is given to it in files.
GROUP='import os, sys; os.setsid(); os.execvp(sys.argv[1], sys.argv[1:])'
ingroup() {
  launching=1
  python3 -c "$GROUP" "$@" < /dev/null & last=$!
  active="$active $last"
  launching=0
  [ "$pending" = 0 ] || stop_all
}
# done takes a command that has been waited for off the list, so that its
# number, which the system may give to another process, is not signalled.
done_with() { rest=""; for p in $active; do [ "$p" = "$1" ] || rest="$rest $p"; done; active="$rest"; }
await() { ingroup "$@"; mine=$last; rc=0; wait "$mine" || rc=$?; done_with "$mine"; return "$rc"; }
export BEND_SCRATCH="$RUN"
export BEND_IMAGE="$IMAGE"
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
S3="$RUN/step3"; mkdir "$S3"
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
    # the container reads the copy as another user, whatever the umask here
    chmod -R a+rX "$d/work" || copied=0
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
node_tests() { await go test -C "$ROOT" -tags bendrules ./test/bend/ -skip '^TestExhaustive' -count=1 -v -json=false -fullpath=false -timeout 30m; }
# The mock chain logs every block; node.log keeps the lines of the test.
if [ "${NODE:-1}" = 0 ]; then
  echo "skipped (NODE=0)" | tee "$OUT/node.log"
elif node_tests > "$RUN/node-full.log" 2>&1; then
  grep -E '^(=== |--- |ok|PASS|FAIL)|^ +[a-z_]+_test\.go:[0-9]+: ' "$RUN/node-full.log" | sed 's/^ *[a-z_]*_test\.go:[0-9]*: //' > "$OUT/node.log"
  grep -E '^(--- |ok|FAIL)|scenarios|refused +[0-9]+ times|accepted +[0-9]+|refunded +[0-9]+|paid zero|rounds:' "$OUT/node.log" || true
  # "go test" also ends well when it ran nothing (a -run in GOFLAGS, a
  # build tag that hides the files, tests renamed away). The comparisons
  # in the source must be the ones named above, and each must have passed.
  tests_in_package > /dev/null   # made here, where a signal can stop it; read below
  have="$(tests_in_package | grep -v '^TestExhaustive' | tr '\n' ' ')"
  want="$(printf '%s\n' $NODE_TESTS | sort | tr '\n' ' ')"
  [ -n "$have" ] && [ "$have" = "$want" ] || { echo "FAIL: node (the tests in the package are: $have; run.sh expects: $want)"; fail=1; }
  # every name that passed at the top level, whatever kind of test it is
  ran="$(passed_in "$RUN/node-full.log" | tr '\n' ' ')"
  [ "$ran" = "$want" ] || { echo "FAIL: node (passed: $ran; run.sh expects: $want)"; fail=1; }
else
  cp "$RUN/node-full.log" "$OUT/node.log"; cat "$OUT/node.log"; echo "FAIL: node"; fail=1
fi

echo
echo "== 5. node faults"
if [ "${FAULTS:-1}" = 0 ]; then
  echo "skipped (FAULTS=0)" | tee "$OUT/node-faults.log"
else
  # The log is made anew, so that it is this run's, and every fault the
  # script lists must be in it as caught, each once and nothing else: a run
  # that ended well having tried fewer is not a pass.
  rm -f "$OUT/node-faults.log"
  await python3 "$HERE/node_faults.py" --run "$OUT" || { echo "FAIL: node faults"; fail=1; }
  # The names, one a line: the ones the script lists (the listing must end
  # well), and the ones on the log's lines. Every line of the log must be
  # the catch of one fault, and the two lists must be the same.
  if python3 "$HERE/node_faults.py" --list > "$RUN/faults.tsv" && [ -s "$RUN/faults.tsv" ]; then
    cut -f1 "$RUN/faults.tsv" | sort > "$RUN/faults.want"
    touch "$OUT/node-faults.log"
    sed -n 's/^caught   \([a-z0-9-]*\): the node .*/\1/p' "$OUT/node-faults.log" | sort > "$RUN/faults.have"
    lines="$(grep -c '' "$OUT/node-faults.log" || true)"
    if ! cmp -s "$RUN/faults.have" "$RUN/faults.want" || [ "$lines" != "$(grep -c '' "$RUN/faults.want")" ]; then
      echo "FAIL: node faults (caught: $(tr '\n' ' ' < "$RUN/faults.have"); the faults are: $(tr '\n' ' ' < "$RUN/faults.want"); lines in the log: $lines)"; fail=1
    fi
  else
    echo "FAIL: node faults (the faults could not be listed)"; fail=1
  fi
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
  await go test -C "$ROOT" -tags bendrules -c -o "$RUN/bend.test" ./test/bend/
  # One process per shard. Each writes "exit N" as its last line whatever
  # happens to the test binary, and its own exit status is collected below:
  # a shard that is killed part way must not leave a log that reads as a pass.
  cd "$HERE"   # the tests look for the models beside them
  i=0; pids=""
  while [ "$i" -lt "$SHARDS" ]; do
    ingroup env BEND_DEPTH="$DEPTH" BEND_SHARD="$i/$SHARDS" "$RUN/bend.test" -test.run '^TestExhaustive' -test.v -test.fullpath=false -test.timeout 12h > "$RUN/exhaustive-$i.log" 2>&1
    pids="$pids $last"
    i=$((i + 1))
  done
  shards_ok=1; started=0
  for pid in $pids; do
    rc=0
    wait "$pid" || rc=$?
    done_with "$pid"
    echo "exit $rc" >> "$RUN/exhaustive-$started.log"
    [ "$rc" = 0 ] || shards_ok=0
    started=$((started + 1))
  done
  [ "$started" = "$SHARDS" ] || { echo "started $started of $SHARDS shards"; shards_ok=0; }
  : > "$OUT/exhaustive.log"
  i=0
  while [ "$i" -lt "$SHARDS" ]; do
    grep -E '^(--- |exit )|every sequence|disagree|node:  |model: |outside the rule|running the model' "$RUN/exhaustive-$i.log" | sed "s/^ *[a-z_]*_test\\.go:[0-9]*: //; s/^/shard $i: /" >> "$OUT/exhaustive.log"
    # a shard counts only when it ended with exit 0 and every exhaustive test in it passed
    tail -1 "$RUN/exhaustive-$i.log" | grep -q '^exit 0$' || shards_ok=0
    i=$((i + 1))
  done
  # The summary, and a check of it. Each test reports for itself, in one
  # line of its own, which shard it ran and how many sequences it compared
  # with the model. Required here: the exhaustive tests in the source are
  # the ones named at the top; each ran once in every shard, in the shard
  # it was started as, to its length and with its number of steps; and each
  # shard compared exactly its share of the sequences there are. A run that
  # passed while comparing fewer is not a pass.
  tests_in_package > /dev/null
  have="$(tests_in_package | grep '^TestExhaustive' | tr '\n' ' ')"
  # (The check is a program in a file: a command in the background is given
  # no standard input by some shells, and then would check nothing.)
  cat > "$RUN/summary.py" <<'PY'
import re, sys
run, shards, depth, in_source = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), sys.argv[4].split()
want = {}
for spec in sys.argv[5:]:
    name, steps, extra, tag = spec.split(":")
    want[name] = (int(steps), depth + int(extra), tag)

def digest(steps, d, shard, of, tag):
    """The SHA-256 of the names of the sequences shard is to compare, as
    sequences() in exhaustive_test.go lists them: lengths 1 to d, each
    counted up with the last step fastest, every of-th one starting with
    the shard-th. Fed to the hash one name at a time."""
    import hashlib, itertools
    h, count, first = hashlib.sha256(), 0, True
    for length in range(1, d + 1):
        for seq in itertools.product(range(steps), repeat=length):
            if count % of == shard:
                h.update((("" if first else "\n") + tag + "-" + ".".join(map(str, seq))).encode())
                first = False
            count += 1
    return h.hexdigest()
bad = []
if sorted(in_source) != sorted(want):
    bad.append("the exhaustive tests in the source are %s; run.sh expects %s" % (" ".join(sorted(in_source)), " ".join(sorted(want))))
# a line the test printed itself, and in it the report
REPORT = re.compile(r"^\s+[a-z_]+_test\.go:\d+: exhaustive: test=(\w+) shard=(\d+)/(\d+) depth=(\d+) steps=(\d+) compared=(\d+) names=([0-9a-f]{64}) different=(\d+)$")
seen = {}  # (test, shard) -> (depth, steps, compared, different)
passed = set()
for i in range(shards):
    line = ""
    # newline="": the lines as they are written, so that "exit 0" with
    # anything else after it is not taken for the shard's last line
    for line in open("%s/exhaustive-%d.log" % (run, i), errors="replace", newline=""):
        m = REPORT.match(line.rstrip("\n"))
        if m:
            name, shard, of = m.group(1), int(m.group(2)), int(m.group(3))
            if shard != i or of != shards:
                bad.append("%s reports shard %d/%d in the log of shard %d of %d" % (name, shard, of, i, shards))
            elif name not in want:
                bad.append("%s reports in shard %d but is not expected" % (name, i))
            elif (name, i) in seen:
                bad.append("%s reports twice in shard %d" % (name, i))
            else:
                seen[(name, i)] = (int(m.group(4)), int(m.group(5)), int(m.group(6)), m.group(7), int(m.group(8)))
        if re.match(r"\s*--- FAIL", line):
            bad.append("a test failed in shard %d: %s" % (i, line.strip()[:120]))
        m = re.match(r"--- PASS: (\S+) ", line)
        if m:
            passed.add((m.group(1), i))
            if m.group(1) not in want:
                bad.append("%s passed in shard %d but is not expected" % (m.group(1), i))
    # the last line of the log, as it is written, is the shard's exit status
    if line != "exit 0\n":
        bad.append("shard %d did not end with exit 0" % i)
# First everything that costs nothing, for every test and every shard: the
# shard ended well, the report is there, the test passed, the length and
# the steps are right, and the count is the shard's share. The digests,
# which mean listing every sequence again, are only worked out when all of
# that holds.
rows, cheap = [], True
for name in want:
    steps, d, tag = want[name]
    total = sum(steps ** k for k in range(1, d + 1))
    compared = different = 0
    problem = ""
    for i in range(shards):
        count = total // shards + (1 if i < total % shards else 0)
        r = seen.get((name, i))
        if r is None or (name, i) not in passed:
            problem = "did not run and pass in shard %d" % i
        elif r[0] != d or r[1] != steps:
            problem = "shard %d ran to length %d from %d steps, want %d from %d" % (i, r[0], r[1], d, steps)
        elif r[2] != count:
            problem = "shard %d compared %d sequences, and its share of %d is %d" % (i, r[2], total, count)
        if problem:
            break
        compared += r[2]; different += r[4]
    if not problem and different:
        problem = "FAIL"
    cheap = cheap and not problem
    rows.append([name, steps, d, tag, total, compared, different, problem])
cheap = cheap and not bad
for name, steps, d, tag, total, compared, different, problem in rows:
    if cheap:
        for i in range(shards):
            if seen[(name, i)][3] != digest(steps, d, i, shards, tag):
                problem = "shard %d compared %d sequences, but not the ones of its share" % (i, seen[(name, i)][2])
                break
    if problem:
        bad.append("%s: %s" % (name, problem))
    print("%-30s every sequence of up to %d of %d steps: %6d of %d compared, %d different  %s" % (name, d, steps, compared, total, different, problem or ("PASS" if cheap else "not checked further")))
for b in bad:
    print("FAIL: exhaustive (%s)" % b)
if bad or not want:
    sys.exit(1)
# said last, and looked for by run.sh: without it nothing was checked
print("exhaustive: checked %d tests in %d shards" % (len(want), shards))
PY
  if await python3 "$RUN/summary.py" "$RUN" "$SHARDS" "$DEPTH" "$have" $EXHAUSTIVE_TESTS > "$RUN/summary.txt" 2> "$RUN/summary.err"; then summary_ok=1; else summary_ok=0; fi
  cat "$RUN/summary.txt"; cat "$RUN/summary.err" >&2
  [ "$summary_ok" = 1 ] && [ "$(tail -1 "$RUN/summary.txt")" = "exhaustive: checked $(printf '%s\n' $EXHAUSTIVE_TESTS | grep -c '') tests in $SHARDS shards" ] || shards_ok=0
  if [ "$shards_ok" = 0 ] || grep -q -E '^shard [0-9]+: (--- FAIL|exit [1-9])' "$OUT/exhaustive.log"; then echo "FAIL: exhaustive"; fail=1; fi
fi

echo
if [ "$fail" = 0 ]; then echo "PASS  logs in $OUT"; else echo "FAIL  logs in $OUT"; fi
exit "$fail"
