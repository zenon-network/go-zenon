#!/bin/sh
# Run the whole baseline for the commit that is checked out, unattended, and
# leave everything it produced in one new directory.
#
#   test/baseline.sh <new-output-directory>
#   QUALIFY=1 test/baseline.sh <dir>     # also race, shuffle, libznn and fuzzing
#
# This is procedure P12 of docs/TESTING.md. It takes about three hours, most of
# it waiting for momentums.
#
# The exit code is the verdict: 0 only if every required stage ran and gave the
# result it has to give. A stage that fails does not stop the independent
# stages after it, but stages that depend on it are recorded as not run, which
# is a failure. status.tsv has one line per stage: verdict, name, detail.
#
# Two stages have a result other than "exit 0" as their required result:
#   vet         go vet must print exactly the lines in test/known-vet.txt
#   joiner-105  the stock late joiner must show the signature of issue #105
# The smoke stage runs with --strict-cadence: after start-up no slot may be empty.
# Each driver stage must pass exactly the cases listed in test/e2e/cases/<stage>.txt,
# and the stages recorded must be exactly the required ones.
# When either stops being true the stage fails, and the expectation is to be
# changed here on purpose, not by accident.
#
# Needs Go with cgo, docker with compose, python3. Leaves no devnet running,
# unless the evidence of one could not be collected, or the run was interrupted:
# that one is kept, no further devnet stage is run, and the last lines say so.
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
[ $# -eq 1 ] || { echo "usage: test/baseline.sh <new-output-directory>" >&2; exit 2; }
mkdir -p "$1" || exit 2
S="$(cd "$1" && pwd)" || exit 2
[ -z "$(ls -A "$S")" ] || { echo "baseline: $S is not empty; every attempt gets its own directory" >&2; exit 2; }
cd "$ROOT" || exit 2

# A known environment: nothing inherited may change what is built, started or run.
unset FAST IMAGE GOIMG DEVNET_IMAGE DEVNET_GENESIS GOFLAGS GOEXPERIMENT GOARCH GOOS COMPOSE_FILE COMPOSE_PROJECT_NAME COMPOSE_PROFILES E2E_RPC E2E_CHAIN_ID E2E_MIN_HEIGHT E2E_DESTRUCTIVE E2E_COVERAGE E2E_NODES E2E_GENESIS
export E2E_NODES="pillar,pillar2,pillar3,rpc,observer"   # the driver checks all five through docker exec
export GOWORK=off
# Settings saved with `go env -w` (a GOFLAGS of -run=^$, say) would apply even
# with GOFLAGS unset; GOENV=off ignores that file.
export GOENV=off
# Every list this script compares (known-fuzz.txt, known-vet.txt, the case
# lists) is stored in C order; a locale's collation would order them otherwise.
export LC_ALL=C
FUZZTIME="${FUZZTIME:-30s}"

FAILED=0
KEPT=0
: > "$S/status.tsv"
# stage-times.tsv: when each stage ended, so that a clock movement can be put to a stage.
ok()   { printf '%s\t%s\n' "$(date +%s)" "$1" >> "$S/stage-times.tsv"; printf 'ok\t%s\t%s\n' "$1" "$2" | tee -a "$S/status.tsv"; }
bad()  { printf '%s\t%s\n' "$(date +%s)" "$1" >> "$S/stage-times.tsv"; printf 'FAIL\t%s\t%s\n' "$1" "$2" | tee -a "$S/status.tsv"; FAILED=$((FAILED + 1)); }
stage() { # <name> <exit-code> <detail>: required result is exit 0
  if [ "$2" -eq 0 ]; then ok "$1" "$3"; else bad "$1" "exit $2; $3"; fi
}
notrun() { for n in "$@"; do bad "$n" "not run: a stage it depends on failed"; done; }
# cleanup: remove the devnet; its exit status says whether that worked.
cleanup() { docker compose --profile sync down -v > "$S/cleanup.out" 2>&1; }
# A devnet that did not come up is removed, but not before what its containers
# printed is kept: that output is the only account of why. If it cannot be kept,
# or the removal fails, the devnet is left as it is, like a chain whose evidence
# down.sh could not collect, and no further devnet stage runs: the next up.sh
# would start by removing it. Either way the variant's down stage says which:
# upfailed <variant> <down-stage>.
upfailed() {
  if ! { docker compose --profile sync ps -a && docker compose --profile sync logs --no-color; } > "$S/up-failed-$1.logs" 2>&1; then
    bad "$2" "not run: the devnet did not start and its container output could not be collected (see up-failed-$1.logs); its containers and volumes were retained for inspection, possibly stopped, and no further devnet stage runs"
    IMG=1; IMGF=1; KEPT=1
  elif ! cleanup; then
    bad "$2" "not run: the devnet did not start; its container output is in up-failed-$1.logs, but removing it failed (see cleanup.out); its containers and volumes were retained as they are, possibly stopped, and no further devnet stage runs"
    IMG=1; IMGF=1; KEPT=1
  else
    bad "$2" "not run: the devnet did not start; its container output is in up-failed-$1.logs, and it was removed"
  fi
}
stopclock() { docker rm -f znnd-baseline-clock >/dev/null 2>&1; }
# An interrupt removes no chain: what is up may be the only evidence of why.
trap 'stopclock; echo "baseline: interrupted; a devnet that was up is left up. Inspect it, then remove it with: docker compose --profile sync down -v" >&2; exit 130' INT TERM

# The tree must be what git says it is: a local edit would enter the binary unseen.
if [ -n "$(git status --porcelain)" ] && [ "${ALLOW_DIRTY:-0}" != 1 ]; then
  git status --porcelain > "$S/dirty.txt"
  echo "baseline: the working tree has changes (see $S/dirty.txt); commit them, or set ALLOW_DIRTY=1 and they are saved with the run" >&2
  exit 2
fi
# Go builds every .go file under the module, also one git ignores (a scratch file
# under build/, say): it would enter the build or break it, unseen by git.
stray="$(git ls-files -o -i --exclude-standard -- '*.go')"
if [ -n "$stray" ]; then echo "baseline: Go files that git ignores are inside the module and would be built; move them out:" >&2; echo "$stray" >&2; exit 2; fi
git status --porcelain > "$S/dirty.txt"; git diff HEAD > "$S/dirty.diff"
{
  echo "harness-commit  $(git rev-parse HEAD)"
  echo "describe        $(git describe --tags --always)"
  echo "node-source     $(git log -1 --format=%H -- . ':!test' ':!docs')   (last commit touching anything outside test/ and docs/)"
  echo "dirty-files     $(wc -l < "$S/dirty.txt" | tr -d ' ')"
  echo "started         $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "host            $(uname -srm)"
  go version; go env GOOS GOARCH CGO_ENABLED CC | tr '\n' ' '; echo
  echo "docker          $(docker version --format '{{.Server.Version}} {{.Server.Os}}/{{.Server.Arch}}' 2>&1)"
  echo "compose         $(docker compose version --short 2>&1)"
  python3 --version
  echo "host-ports      http ${DEVNET_HTTP_PORT:-35997} ws ${DEVNET_WS_PORT:-35998} pillar-http ${DEVNET_PILLAR_HTTP_PORT:-35991} sync-http ${DEVNET_SYNC_HTTP_PORT:-35999} sync-ws ${DEVNET_SYNC_WS_PORT:-36001}"
  # The clock of the machine the nodes run on. A pillar drops its slot when its
  # wall clock is even slightly behind the timer that woke it, which happens
  # while the clock is being corrected, for example after the host was
  # suspended (docs/TESTING.md, P12). Kept as evidence for a failed cadence check.
  echo "node-clock      $(docker run --rm --entrypoint sh go-zenon-devnet:latest -c 'date -u +%Y-%m-%dT%H:%M:%SZ' 2>&1) (in a container), host $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  if command -v colima >/dev/null 2>&1; then colima ssh -- timedatectl timesync-status 2>&1 | grep -iE 'offset|frequency|jitter' | tr -s ' ' | tr '\n' ';'; echo; fi
} > "$S/environment.txt" 2>&1
go env > "$S/go-env.txt" 2>&1

# ---- P1: build, static checks, unit tests ---------------------------------
go build ./... > "$S/build.out" 2>&1; stage build $? "go build ./..."
go test ./... -count=1 > "$S/unit.out" 2>&1
stage unit $? "$(grep -c '^ok' "$S/unit.out") packages ok, $(grep -cE '^(FAIL|--- FAIL)' "$S/unit.out") failures"
go vet ./... 2>&1 | grep -v '^#' | sed -E 's/:[0-9]+:[0-9]+:/:/' | sort > "$S/vet.txt"
if diff test/known-vet.txt "$S/vet.txt" > "$S/vet.diff"; then ok vet "exactly the $(wc -l < test/known-vet.txt | tr -d ' ') known lines"
else bad vet "differs from test/known-vet.txt, see vet.diff"; fi
go run ./cmd/devnet-keygen --verify-genesis docker/devnet/genesis.json > "$S/genesis.out" 2>&1; stage verify-genesis $? "stock genesis"
( python3 test/devnet/genesis.py && for v in all-off mainnet-like all-on staged; do
    go run ./cmd/devnet-keygen --verify-genesis "test/devnet/genesis/$v.json" || exit 1
  done ) > "$S/genesis-variants.out" 2>&1
stage verify-variants $? "all-off mainnet-like all-on staged"
# What compose will start for each variant, rendered with the two variables
# up.sh sets for it. A variant that does not render is a failure here, not later.
rc=0; : > "$S/compose-config.out"
for v in stock all-on mainnet-like all-off fast; do
  case "$v" in stock) g="$ROOT/docker/devnet/genesis.json" ;; *) g="$ROOT/test/devnet/genesis/$v.json" ;; esac
  case "$v" in fast) i=go-zenon-devnet-fast:latest ;; *) i=go-zenon-devnet:latest ;; esac
  if DEVNET_GENESIS="$g" DEVNET_IMAGE="$i" docker compose -f docker-compose.yml -f test/devnet/compose.variant.yml --profile sync config > "$S/compose-config-$v.txt" 2>> "$S/compose-config.out" \
     && grep -q "$g" "$S/compose-config-$v.txt" && grep -q "image: $i" "$S/compose-config-$v.txt"; then echo "rendered $v" >> "$S/compose-config.out"
  else rc=1; echo "NOT rendered: $v" >> "$S/compose-config.out"; fi
done
stage compose-config $rc "$(grep -c '^rendered' "$S/compose-config.out") of 5 variants rendered with their genesis and image"

if [ "${QUALIFY:-0}" = 1 ]; then
  go test -race ./... -count=1 > "$S/race.out" 2>&1; stage race $? "$(grep -c 'DATA RACE' "$S/race.out") races reported"
  go test ./... -count=1 -shuffle=on > "$S/shuffle.out" 2>&1; stage shuffle $? "seeds: $(grep -ho 'shuffle [0-9]*' "$S/shuffle.out" | sort -u | tr '\n' ' ')"
  { go build -tags libznn ./... && go test -tags libznn ./cmd/libznn/... -count=1; } > "$S/libznn.out" 2>&1; stage libznn $? "build and test with the libznn tag"
  # The targets are the ones test/known-fuzz.txt names, by package and name, no
  # fewer and no more. They are taken from what the test binaries compile
  # (go test -list), not from the source text: a target behind a build tag is
  # not a target of this run. The listing is read as JSON events, so each name
  # comes with the package Go attributes it to, whatever else a test binary
  # prints; every listed name that starts with Fuzz is taken as Go printed it.
  # Each target must then show that it executed inputs.
  rc=0; : > "$S/fuzz.out"
  if ! go test -json -list '^Fuzz' ./... > "$S/fuzz-list.out" 2>&1; then bad fuzz "go test -list failed, see fuzz-list.out"
  elif ! python3 - "$(go list -m)" "$S/fuzz-list.out" > "$S/fuzz-targets.unsorted" <<'PY'
import json, sys
module, path = sys.argv[1], sys.argv[2]
# Go lists only names it compiled, so every listed name that starts with Fuzz
# is taken, without checking it as an identifier here: a check by Python's
# Unicode tables, which lag Go's, would drop a target silently. A stray line
# that is not a target fails the comparison with the manifest instead.
for line in open(path, encoding="utf-8"):
    line = line.strip()
    if not line.startswith("{"):
        continue
    e = json.loads(line)
    if e.get("Action") != "output" or not e.get("Package"):
        continue
    name = e.get("Output", "").strip()
    if name.startswith("Fuzz") and not any(c.isspace() for c in name):
        pkg = e["Package"]
        if pkg == module:
            pkg = "."
        elif pkg.startswith(module + "/"):
            pkg = "./" + pkg[len(module) + 1:]
        print("%s\t%s" % (pkg, name))
PY
  then bad fuzz "the fuzz listing could not be read as JSON events, see fuzz-list.out"
  else
    sort "$S/fuzz-targets.unsorted" > "$S/fuzz-targets.txt"; rm -f "$S/fuzz-targets.unsorted"
    if ! diff test/known-fuzz.txt "$S/fuzz-targets.txt" > "$S/fuzz-targets.diff"; then bad fuzz "the compiled fuzz targets are not those of test/known-fuzz.txt, see fuzz-targets.diff"
    else
      n=0; total=$(wc -l < test/known-fuzz.txt | tr -d ' ')
      while IFS="$(printf '\t')" read -r pkg t; do
        go test "$pkg" -run '^$' -fuzz "^$t\$" -fuzztime "$FUZZTIME" > "$S/fuzz-one.out" 2>&1; frc=$?
        cat "$S/fuzz-one.out" >> "$S/fuzz.out"
        # Proof that it ran: the fuzzing engine's own progress line with a positive count.
        execs=$(sed -nE 's/^fuzz: elapsed: .*execs: ([0-9]+) .*/\1/p' "$S/fuzz-one.out" | tail -1)
        if [ $frc -eq 0 ] && [ "${execs:-0}" -gt 0 ]; then n=$((n + 1)); echo "PASSED $pkg $t execs=$execs" >> "$S/fuzz.out"
        else rc=1; echo "FAILED $pkg $t exit=$frc execs=${execs:-none}" >> "$S/fuzz.out"; fi
      done < test/known-fuzz.txt
      rm -f "$S/fuzz-one.out"
      [ "$n" -eq "$total" ] || rc=1
      stage fuzz $rc "$n of $total targets passed and executed inputs, $FUZZTIME each"
    fi
  fi
fi

# ---- P2, P11: the two images ---------------------------------------------
test/devnet/build-image.sh > "$S/image.out" 2>&1; IMG=$?; stage image $IMG "$(grep -E '^Version' "$S/image.out")"
FAST=1 test/devnet/build-image.sh > "$S/image-fast.out" 2>&1; IMGF=$?; stage image-fast $IMGF "$(grep -E '^Version' "$S/image-fast.out")"

# ---- the clock of the machine the nodes run on ------------------------------
# A step of the wall clock makes pillars drop their slots (P4), so a run on such
# a machine says little about the node. With Colima this happens after the host
# was suspended; restart it. Two measurements, both in a container, since that
# is the clock the nodes read:
#   clock      before any devnet stage, the best of up to three 13 s measurements; a failure runs no devnet stage
#   clock-run  one reading every 20 s for as long as the devnet stages last, kept
#              in clock-samples.tsv and judged at the end
# busybox has no sub-second wall clock, so a reading waits for the wall clock to
# tick over to a new second and reads the uptime at that instant, in the same
# shell. With a steady clock, wall seconds minus uptime stays constant; the
# reading is that difference in milliseconds, to about 10 ms. A late read of
# the uptime makes one reading low and the next one normal again; a step moves
# every later reading. clock-run therefore judges the running median of five.
CLOCKIMG=""; [ $IMG -eq 0 ] && CLOCKIMG=go-zenon-devnet:latest; [ -z "$CLOCKIMG" ] && [ $IMGF -eq 0 ] && CLOCKIMG=go-zenon-devnet-fast:latest
TICK='tick() { s=$(date +%s); while [ "$(date +%s)" = "$s" ]; do :; done; read u _ < /proc/uptime; echo "$((s + 1)) $(( (s + 1) * 1000 - ${u%.*}${u#*.} * 10 ))"; }'
CLOCK=0
if [ -z "$CLOCKIMG" ]; then notrun clock; CLOCK=1
else
  drift="$(docker run --rm --entrypoint sh "$CLOCKIMG" -c "$TICK"'
    best=""; for i in 1 2 3; do set -- $(tick); a=$2; sleep 12; set -- $(tick); d=$(( $2 - a )); [ ${d#-} -le 30 ] && { best=$d; break; }; best=$d; done; echo $best' 2>&1)"
  case "$drift" in
    ''|*[!0-9-]*) bad clock "could not measure: $drift"; CLOCK=1 ;;
    *) if [ "${drift#-}" -le 30 ]; then ok clock "wall clock moved $drift ms against uptime in 13 s (best of up to three tries)"
       else bad clock "wall clock moved $drift ms against uptime in 13 s, three times over: it is being stepped; restart the docker VM and run again"; CLOCK=1; fi ;;
  esac
fi
if [ $CLOCK -ne 0 ]; then IMG=1; IMGF=1   # nothing on the devnet is run on such a clock
else
  stopclock; CLOCKSTART=$(date +%s)
  docker run -d --name znnd-baseline-clock --entrypoint sh "$CLOCKIMG" -c "$TICK"'
    while :; do tick; sleep 19; done' >/dev/null 2>&1 || CLOCKSTART=0   # clock-run then fails: nothing was read
fi

# The groups a variant must skip, and nothing else may be skipped.
FASTG="TestFastAccelerator TestFastBridgeAdmin TestFastPillar TestFastPlasma TestFastSentinel TestFastStake"
NORMG="TestAccelerator TestHtlc TestLedger TestPillar TestPlasma TestPtlc TestSentinel TestSpork TestSporkLadder TestStake TestToken"
skips() {
  case "$1" in
    stock|mainnet-like) echo "TestHaltOnUnknownSpork TestPtlc $FASTG" ;;
    all-on)  echo "TestHaltOnUnknownSpork $FASTG" ;;
    all-off) echo "TestAccelerator TestHaltOnUnknownSpork TestHtlc TestPtlc $FASTG" ;;
    fast)    echo "TestHaltOnUnknownSpork $NORMG" ;;
    destructive) ;;   # run with -run, so no other group is even started
  esac | tr ' ' '\n' | grep . | sort
}
driver() { # <stage-name> <variant-for-skips> [go test arguments]: the cases against the devnet that is up
  n="$1"; v="$2"; shift 2
  case "$v" in stock|destructive) g="$ROOT/docker/devnet/genesis.json" ;; *) g="$ROOT/test/devnet/genesis/$v.json" ;; esac
  E2E_GENESIS="$g" E2E_COVERAGE="$S/cov-$n.tsv" go test -tags e2e ./test/e2e/ -count=1 -v -timeout 60m "$@" > "$S/e2e-$n.out" 2>&1; rc=$?
  p=$(grep -cE '^[[:space:]]+--- PASS' "$S/e2e-$n.out"); f=$(grep -cE '^[[:space:]]*--- FAIL' "$S/e2e-$n.out")
  grep -E '^--- SKIP' "$S/e2e-$n.out" | sed -E 's/^--- SKIP: ([A-Za-z0-9_]+).*/\1/' | sort > "$S/skips-$n.txt"
  s=$(grep -cE '^[[:space:]]+--- SKIP' "$S/e2e-$n.out")
  if [ $rc -ne 0 ] || [ "$f" -ne 0 ]; then bad "driver-$n" "exit $rc, $p passed, $f failed"
  elif [ "$p" -eq 0 ]; then bad "driver-$n" "no case ran"
  # Named first: a case that skipped itself is otherwise only a missing name in the diff below.
  elif [ "$s" -ne 0 ]; then bad "driver-$n" "$p passed, but $s cases skipped themselves: $(grep -E '^[[:space:]]+--- SKIP' "$S/e2e-$n.out" | tr -s ' ' | tr '\n' ';')"
  elif ! grep -E '^[[:space:]]+--- PASS: ' "$S/e2e-$n.out" | sed -E 's/^[[:space:]]+--- PASS: ([^ ]+) .*/\1/' | sort | diff "test/e2e/cases/$n.txt" - > "$S/cases-$n.diff"; then
    bad "driver-$n" "$p passed, but they are not exactly the cases of test/e2e/cases/$n.txt, see cases-$n.diff"
  elif ! skips "$v" | diff - "$S/skips-$n.txt" > "$S/skips-$n.diff"; then bad "driver-$n" "$p passed, but the skipped groups are not the expected ones, see skips-$n.diff"
  else ok "driver-$n" "$p cases passed, none failed, none skipped"; fi
}
up() { # <variant>: 0 when the devnet is up and proven
  test/devnet/up.sh "$1" "$S/manifest-$1${2:-}.txt" > "$S/up-$1${2:-}.out" 2>&1; rc=$?
  stage "up-$1${2:-}" $rc "$(tail -1 "$S/up-$1${2:-}.out")"; return $rc
}
down() { # <name>
  test/devnet/down.sh "$S/logs-$1" > "$S/down-$1.out" 2>&1; rc=$?
  stage "down-$1" $rc "$(grep 'error and critical' "$S/down-$1.out")"
  # Without the evidence the chain is not removed, and nothing else may remove
  # it either: no further devnet stage is run.
  [ $rc -eq 0 ] || { IMG=1; IMGF=1; KEPT=1; }
}

# ---- P3 to P5, P9 on the stock genesis -----------------------------------
if [ $IMG -ne 0 ]; then notrun up-stock smoke joiner-105 joiner-libp2p driver-stock down-stock
elif ! up stock; then notrun smoke joiner-105 joiner-libp2p driver-stock; upfailed stock down-stock
else
  python3 test/devnet/smoke.py --height 45 --strict-cadence --out "$S/smoke.json" --cov "$S/cov-smoke.tsv" > "$S/smoke.out" 2>&1; stage smoke $? "$(tail -1 "$S/smoke.out"); $(grep -o '[0-9]* empty slots' "$S/smoke.out")"
  # Late joiner: started after the libp2p switch (height 20), observed for 90 s.
  if DEVNET_GENESIS="$ROOT/docker/devnet/genesis.json" DEVNET_IMAGE=go-zenon-devnet:latest \
     docker compose -f docker-compose.yml -f test/devnet/compose.variant.yml --profile sync up -d --no-build --no-deps syncnode > "$S/joiner-up.out" 2>&1; then
    # An unpinned joiner fails its stage: down.sh will keep the chain.
    pinned=0
    test/devnet/pin-start.sh syncnode >> "$S/joiner-up.out" 2>&1 || { pinned=1; echo "baseline: the joiner's start could not be pinned; down.sh will keep the chain" >> "$S/joiner-up.out"; }
    sleep 90
    python3 test/devnet/smoke.py --height 45 --expect-105 --joiner-only --out "$S/smoke-joiner-105.json" > "$S/smoke-joiner-105.out" 2>&1; rc=$?
    why="$(grep 'late-joiner-issue-105' "$S/smoke-joiner-105.out" | cut -c1-160)"
    if [ $pinned -ne 0 ]; then rc=1; why="the joiner's first start could not be pinned (joiner-up.out); $why"; fi
    stage joiner-105 $rc "$why"
    # The same node with the transport forced: it must then sync within two minutes.
    docker exec znnd-devnet-syncnode sh -c 'grep -q "\"P2PBackend\"" /root/.znn/config.json && exit 3; sed -i "s/\"Net\": {/\"Net\": {\n        \"P2PBackend\": \"libp2p\",/" /root/.znn/config.json && grep -q "\"P2PBackend\": \"libp2p\"" /root/.znn/config.json' \
      && docker restart znnd-devnet-syncnode > /dev/null 2>&1; rc=$?
    if [ $rc -ne 0 ]; then bad joiner-libp2p "could not set P2PBackend in the joiner's config (exit $rc)"
    else
      rc=1; i=0; while [ $i -lt 12 ] && [ $rc -ne 0 ]; do sleep 10; i=$((i + 1))
        python3 test/devnet/smoke.py --height 45 --sync --joiner-only --out "$S/smoke-joiner-libp2p.json" > "$S/smoke-joiner-libp2p.out" 2>&1; rc=$?; done
      stage joiner-libp2p $rc "after $((i * 10)) s: $(tail -1 "$S/smoke-joiner-libp2p.out")"
    fi
  else bad joiner-105 "the late joiner did not start"; bad joiner-libp2p "not run"; fi
  driver stock stock
  down stock
fi

# ---- P9 destructive, on its own stock chain ------------------------------
if [ $IMG -ne 0 ]; then notrun up-stock-destructive driver-destructive down-destructive
elif ! up stock -destructive; then notrun driver-destructive; upfailed stock-destructive down-destructive
else export E2E_DESTRUCTIVE=1; driver destructive destructive -run TestHaltOnUnknownSpork; unset E2E_DESTRUCTIVE; down destructive; fi

# ---- P10: the other genesis variants, each on an empty chain --------------
for v in all-on mainnet-like all-off; do
  if [ $IMG -ne 0 ]; then notrun "up-$v" "driver-$v" "down-$v"
  elif ! up "$v"; then notrun "driver-$v"; upfailed "$v" "down-$v"
  else driver "$v" "$v"; down "$v"; fi
done

# ---- P11: the test build with fast constants -----------------------------
if [ $IMGF -ne 0 ]; then notrun up-fast driver-fast down-fast
elif ! up fast; then notrun driver-fast; upfailed fast down-fast
else
  mkdir -p build && test/devnet/fast/overlay.sh "$ROOT" > build/fast-overlay.json
  driver fast fast -overlay build/fast-overlay.json
  down fast
fi

# ---- the clock during the devnet stages ------------------------------------
# clock-run fails when the clock moved, and also when it was not watched: the
# sampler must still be running, its readings must start with the devnet stages
# and reach to now, and no two may be more than 45 s apart. A single reading
# more than 30 ms off its neighbours is an excursion and fails the stage too.
if [ $CLOCK -eq 0 ]; then
  running="$(docker inspect -f '{{.State.Running}}' znnd-baseline-clock 2>/dev/null)"
  docker logs znnd-baseline-clock > "$S/clock-samples.tsv" 2>/dev/null; CLOCKEND=$(date +%s); stopclock
  python3 - "$S/clock-samples.tsv" "$S/stage-times.tsv" "${CLOCKSTART:-0}" "$CLOCKEND" "${running:-false}" > "$S/clock-run.out" 2>&1 <<'PY'
import statistics, sys
rows = [l.split() for l in open(sys.argv[1]) if l.strip()]
began, ended, running = int(sys.argv[3]), int(sys.argv[4]), sys.argv[5] == "true"
ok = [r for r in rows if len(r) == 2 and r[0].isdigit() and r[1].lstrip("-").isdigit()]
if len(ok) != len(rows) or len(ok) < 5:
    print("%d usable readings of %d; sampler running at collection: %s" % (len(ok), len(rows), running)); sys.exit(1)
t, v = [int(r[0]) for r in ok], [int(r[1]) for r in ok]
# Was the whole run watched? Times are the container's and the host's wall
# clocks, which differ by well under a second.
unwatched = []
if not running:
    unwatched.append("the sampler was not running when it was collected")
if began == 0 or t[0] - began > 30:
    unwatched.append("the first reading is %d s after the sampler was started" % (t[0] - began))
if ended - t[-1] > 45:
    unwatched.append("the last reading is %d s old" % (ended - t[-1]))
gaps = [t[i + 1] - t[i] for i in range(len(t) - 1) if t[i + 1] - t[i] > 45 or t[i + 1] <= t[i]]
if gaps:
    unwatched.append("%d times two readings are over 45 s apart or out of order, at most %d s" % (len(gaps), max(gaps)))
med = [statistics.median(v[i:i + 5]) for i in range(len(v) - 4)]
spread, raw = max(med) - min(med), max(v) - min(v)
# Excursions: single readings more than 30 ms off the median of the five
# readings around them. The running median alone hides one or two of them.
def around(i):
    # Always five readings: at either end of the trace the window is the
    # first or last five, so a move in the last two readings is measured
    # against the level before it, not against itself.
    lo = min(max(0, i - 2), len(v) - 5)
    return statistics.median(v[lo:lo + 5])
off = [i for i in range(len(v)) if abs(v[i] - around(i)) > 30]
# Which stages the clock moved in: per stage, the spread of the running median
# over the readings taken while it ran (and the one before it started).
stages = [(int(l.split()[0]), l.split()[1]) for l in open(sys.argv[2]) if len(l.split()) == 2]
def stage_at(when):
    for end, name in stages:
        if when <= end:
            return name
    return "after the last recorded stage"
moved, start = [], 0
for end, name in stages:
    inside = [med[i] for i in range(len(med)) if start - 25 <= t[i + 2] <= end]
    if len(inside) > 1 and max(inside) - min(inside) > 30:
        moved.append("%s %d ms" % (name, max(inside) - min(inside)))
        print("moved while %s ran: %d ms" % (name, max(inside) - min(inside)))
    start = end
if spread > 30 and not moved:
    # No single stage holds the movement: name the stages it spans.
    # From the last reading at one extreme to the first at the other after it.
    hi = [i for i in range(len(med)) if med[i] == max(med)]
    lo = [i for i in range(len(med)) if med[i] == min(med)]
    first, then = (hi, lo) if hi[0] < lo[0] else (lo, hi)
    a, b = t[max(i for i in first if i < then[0]) + 2], t[then[0] + 2]
    names, prev = [], 0
    for end, name in stages:
        if end >= a and prev <= b:
            names.append(name)
        prev = end
    names = names or [stage_at(a)]
    moved.append("across " + (names[0] if len(names) == 1 else "%s to %s" % (names[0], names[-1])))
    print("moved across stages: %s" % ", ".join(names))
for i in off:
    print("excursion: the reading at %d is %d ms off its neighbours, during %s" % (t[i], v[i] - around(i), stage_at(t[i])))
for u in unwatched:
    print("not watched: %s" % u)
print("%d readings; running median of five moved %d ms, single readings %d ms; %d excursions over 30 ms%s%s" % (
    len(v), spread, raw, len(off), "; during: " + ", ".join(moved) if moved else "",
    "; NOT WATCHED THROUGHOUT: " + "; ".join(unwatched) if unwatched else ""))
sys.exit(0 if spread <= 30 and not off and not unwatched else 1)
PY
  stage clock-run $? "$(tail -1 "$S/clock-run.out")"
else notrun clock-run; fi

# ---- every required stage must be there, once -------------------------------
{
  echo build unit vet verify-genesis verify-variants compose-config
  [ "${QUALIFY:-0}" = 1 ] && echo race shuffle libznn fuzz
  echo image image-fast clock up-stock smoke joiner-105 joiner-libp2p driver-stock down-stock
  echo up-stock-destructive driver-destructive down-destructive
  for v in all-on mainnet-like all-off fast; do echo "up-$v driver-$v down-$v"; done
  echo clock-run
} | tr ' ' '\n' | grep . | sort > "$S/stages-expected.txt"
cut -f2 "$S/status.tsv" | sort > "$S/stages-recorded.txt"
if ! diff "$S/stages-expected.txt" "$S/stages-recorded.txt" > "$S/stages.diff"; then bad stages "the recorded stages are not exactly the required ones, each once; see stages.diff"; fi
[ $KEPT -eq 0 ] || echo "A devnet's containers and volumes were retained, some possibly stopped, because its evidence or its start-up output could not be collected, or removing it failed; inspect each container's state (docker compose --profile sync ps -a), then remove them: docker compose --profile sync down -v"

echo "finished        $(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$S/environment.txt"
echo
if [ $FAILED -eq 0 ]; then echo "BASELINE PASSED: $(wc -l < "$S/status.tsv" | tr -d ' ') stages, all with the required result. Evidence in $S"
else echo "BASELINE FAILED: $FAILED of $(wc -l < "$S/status.tsv" | tr -d ' ') stages. See $S/status.tsv:"; grep '^FAIL' "$S/status.tsv"; fi
[ $FAILED -eq 0 ]
