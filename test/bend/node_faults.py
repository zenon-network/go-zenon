#!/usr/bin/env python3
"""Deliberate faults for the node, used by run.sh (step 5).

mutants.py breaks the model, to show that the laws notice. This breaks the
node, to show that the comparison notices: each fault removes one check
from a copy of a Go source file of znnd, and the comparison test named with
it must then fail.

No file of the node is edited. The broken copy is written under
test/bend/.scratch and given to the Go tool with `go test -overlay`, which
compiles it in place of the original for that one test run.

    node_faults.py --list                 name<TAB>file<TAB>description
    node_faults.py --run <out dir> [name...]
                                          run all, or the named ones; writes
                                          node-faults.log and node-faults-detail.log

A fault replaces one exact piece of text. If the node's code changes and
the text is no longer there exactly once, the fault is reported as STALE
and the run fails, so a fault cannot stop working without anyone noticing.
"""
import json
import os
import re
import shutil
import signal
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))

# How a fault must be caught. Each fault names one written scenario of its
# test and the two lines that scenario must end in with the fault in the
# node: what the node did (a mark per step, A accepted or R refused, then
# the balances and for the contracts the number of entries) and what the
# model says. The test must fail and print exactly that difference. A test
# that fails in any other way, on another scenario, with other marks or
# balances, or on a check of its own, has not shown this fault noticed.
# The lines are fixed because the mock chain is: the same accounts,
# balances and clock every time.

# name, what the broken node does, file, text, replacement, test that must
# fail, and (scenario, the node's line, the model's line, and a line of
# the test's direct check on the node where there is one for the fault:
# the entry still held, the stake not emptied, the refund as it came back).
# That line begins with the same scenario and the number of the step in it,
# so that a check printed in another scenario does not count.
FAULTS = [
    # The balance check alone is not what stops an overspend: with it gone,
    # SubBalance panics on the negative balance and the block is still
    # refused. So this fault makes the insufficient branch of applyTransfer
    # succeed, which lets the send through without the debit.
    ("overspend-accepted", "accepts a send above the balance", "vm/vm.go",
     "\tif !enoughFunds(vm.context, block) {\n\t\treturn constants.ErrInsufficientBalance\n\t}\n\n\tvm.context.SubBalance(",
     "\tif !enoughFunds(vm.context, block) {\n\t\treturn nil\n\t}\n\n\tvm.context.SubBalance(",
     "TestLedgerModelMatchesNode",
     # every send is accepted, also the ones above the balance, and account 0 is left with nothing
     ("overspend",
      "AAAAA 0 800000000000 100000000000 50000000000",
      "RRRAR 1200000000000 800000000000 100000000000 50000000000",
      None)),
    ("no-received-check", "lets an account receive a send it has received", "verifier/account_block.go",
     "\tif status {\n\t\treturn ErrABFromBlockAlreadyReceived",
     "\tif false && status {\n\t\treturn ErrABFromBlockAlreadyReceived",
     "TestLedgerModelMatchesNode",
     # every repeat of a receive is accepted, and account 0 ends with more than there was
     ("double-receive",
      "AAAAAAAAAA 2000000001400 700 100000000000 50000000000",
      "AAARARRARR 1199999999300 800000000700 100000000000 50000000000",
      None)),
    ("no-recipient-rule", "lets any account receive a send", "verifier/account_block.go",
     "if !receiverMismatchTolerated(",
     "if false && !receiverMismatchTolerated(",
     "TestLedgerModelMatchesNode",
     # accounts 2 and 3 receive a send addressed to account 1
     ("two-receivers",
      "AAAAARAAR 1200000000000 800000000500 100000000500 50000000500",
      "AARRARARR 1199999999500 800000000500 100000000000 50000000000",
      None)),
    ("htlc-unlock-keeps-entry", "pays an HTLC unlock and keeps the entry", "vm/embedded/implementation/htlc.go",
     "\tcommon.DealWithErr(htlcInfo.Delete(context.Storage()))\n\thtlcLog.Debug(\"unlocked\"",
     "\thtlcLog.Debug(\"unlocked\"",
     "TestEscrowModelMatchesHtlc",
     # the unlock pays and the entry is still there at the end
     ("unlock-twice",
      "AARRR 1199999999500 800000000500 100000000000 50000000000 entries 1",
      "AARRR 1199999999500 800000000500 100000000000 50000000000 entries 0",
      r"unlock-twice step 1: unlock entry 0 by 1, right preimage or signature: true: the contract's status says accepted=true, but the entry was held before: true, after: true")),
    ("htlc-expired-unlock-succeeds", "reports an expired HTLC unlock as accepted, without paying", "vm/embedded/implementation/htlc.go",
     "\t\treturn nil, constants.ErrExpired\n",
     "\t\treturn nil, nil\n",
     "TestEscrowModelMatchesHtlc",
     # the unlock at the expiry is reported accepted
     ("expiry-edge-reclaim",
      "AAAARA 1200000000000 800000000000 100000000000 50000000000 entries 0",
      "ARAARA 1200000000000 800000000000 100000000000 50000000000 entries 0",
      r"expiry-edge-reclaim step 1: unlock entry 0 by 1, right preimage or signature: true: the contract's status says accepted=true, but the entry was held before: true, after: true")),
    ("htlc-reclaim-before-expiry", "lets the maker reclaim an HTLC before it expires", "vm/embedded/implementation/htlc.go",
     "if momentum.Timestamp.Unix() < htlcInfo.ExpirationTime {",
     "if false && momentum.Timestamp.Unix() < htlcInfo.ExpirationTime {",
     "TestEscrowModelMatchesHtlc",
     # the second entry is reclaimed before its expiry
     ("expiry-edge-reclaim",
      "ARAAAR 1200000000000 800000000000 100000000000 50000000000 entries 0",
      "ARAARA 1200000000000 800000000000 100000000000 50000000000 entries 0",
      None)),
    ("htlc-reclaim-by-anyone", "lets any account reclaim an expired HTLC", "vm/embedded/implementation/htlc.go",
     "if htlcInfo.TimeLocked != sendBlock.Address {",
     "if false && htlcInfo.TimeLocked != sendBlock.Address {",
     "TestEscrowModelMatchesHtlc",
     # the reclaim by account 2, which did not make the entry, is accepted
     ("reclaim-twice",
      "AARRRR 1200000000000 800000000000 100000000000 50000000000 entries 0",
      "ARRARR 1200000000000 800000000000 100000000000 50000000000 entries 0",
      None)),
    ("ptlc-unlock-keeps-entry", "pays a PTLC unlock and keeps the entry", "vm/embedded/implementation/ptlc.go",
     "\tcommon.DealWithErr(ptlcInfo.Delete(context.Storage()))\n\tdeletePtlcReclaimLock(context, id)",
     "\tdeletePtlcReclaimLock(context, id)",
     "TestEscrowModelMatchesPtlc",
     # the unlock pays and the entry is still there at the end
     ("unlock-twice",
      "AARRR 1199999999500 800000000500 100000000000 50000000000 entries 1",
      "AARRR 1199999999500 800000000500 100000000000 50000000000 entries 0",
      r"unlock-twice step 1: unlock entry 0 by 1, right preimage or signature: true: the contract's status says accepted=true, but the entry was held before: true, after: true")),
    ("stake-cancel-keeps-amount", "pays a stake cancel and leaves the amount in the entry", "vm/embedded/implementation/stake.go",
     "\tstakeInfo.Amount = big.NewInt(0)\n",
     "",
     "TestEscrowModelMatchesStake",
     # the cancels after the first, which a sound node accepts as paying zero, are refused
     ("cancel-twice",
      "AARR 1200000000000 800000000000 100000000000 50000000000 entries 1",
      "AAAA 1200000000000 800000000000 100000000000 50000000000 entries 1",
      r"cancel-twice step 4: reclaim or cancel entry 0 by 0: accepted, and the stake is there: true, holding 5000000000, want it there and empty")),
    ("fusion-cancel-keeps-entry", "pays a fusion cancel and keeps the entry", "vm/embedded/implementation/plasma.go",
     "\tcommon.DealWithErr(fusionInfo.Delete(context.Storage()))\n",
     "",
     "TestEscrowModelMatchesFusion",
     # the entry is still there, and a second cancel pays again
     ("cancel-twice",
      "AAAA 12010000000000 8000000000000 1000000000000 50000000000 entries 1",
      "AARR 12000000000000 8000000000000 1000000000000 50000000000 entries 0",
      r"cancel-twice step 4: reclaim or cancel entry 0 by 0: accepted, and the fusion is still there, holding 5000000000")),
    ("refund-missing", "keeps the amount of a call the contract refuses", "vm/vm.go",
     "\tif sendBlock.Amount.Sign() > 0 {",
     "\tif false && sendBlock.Amount.Sign() > 0 {",
     "TestEscrowModelMatchesHtlc",
     # account 0 ends 200 short: the refused create of 200 did not come back
     ("refund-from-funds",
      "AR 1199999999300 800000000000 100000000000 50000000000 entries 1",
      "AR 1199999999500 800000000000 100000000000 50000000000 entries 1",
      r"refund-from-funds step 1: create by 0 for 1 amount 200 expiry \d+: refused, and the amount was not given back: the contract held 500 before and 700 after, and no block came back")),
    ("refund-doubled", "gives back twice the amount of a call the contract refuses", "vm/vm.go",
     "Amount:        new(big.Int).Set(sendBlock.Amount),",
     "Amount:        new(big.Int).Lsh(sendBlock.Amount, 1),",
     "TestEscrowModelMatchesHtlc",
     # account 0 ends 200 over: the refused create of 200 came back twice
     ("refund-from-funds",
      "AR 1199999999700 800000000000 100000000000 50000000000 entries 1",
      "AR 1199999999500 800000000000 100000000000 50000000000 entries 1",
      r"refund-from-funds step 1: create by 0 for 1 amount 200 expiry \d+: refused, and 1 block\(s\) came back carrying 400, want one carrying 200; the contract held 500 before and 300 after")),
]


# A line the test itself printed: go test puts the file and line before it.
# The node's own log lines (they begin "t=") and anything else on the
# output are not the test speaking, whatever words they hold.
# Exactly four spaces before the file: that is how go test writes the first
# line of a message of a top-level test. The later lines of a message that
# runs over several are indented further, and so is everything a subtest
# says; text quoted inside a message is therefore never taken for a line of
# the test. (The tool is run with -fullpath=false, so the file has no path.)
OWN = re.compile(r"^    [a-z_]+_test\.go:\d+: (.*)$")

# A call the test could not read back. The test stops on it; whatever else
# it printed before, the run says nothing about the fault.
UNREAD = r"unreadable: .*: the call could not be read back: "


# Output is cut into lines at the line feed and nowhere else. (Python's
# splitlines also cuts at form feeds and other separators, which a log line
# of the node may carry inside it: what follows one would then stand at
# the start of a line and read as the test's own.)
def events(raw):
    """What go test -json wrote: one event a line, each saying which test
    it belongs to. Returns the whole output as text, the output of each
    test by its exact name, and the names of the tests that failed. A line
    that is not an event (the tool's own complaints) is kept in the text."""
    text, by_test, failed, runs = [], {}, set(), {}
    for line in raw.split("\n"):
        try:
            ev = json.loads(line)
            if not isinstance(ev, dict) or "Action" not in ev:
                raise ValueError
        except ValueError:
            text.append(line + "\n")
            continue
        if ev["Action"] in ("output", "build-output"):
            text.append(ev.get("Output", ""))
            by_test.setdefault(ev.get("Test", ""), []).append(ev.get("Output", ""))
        elif ev["Action"] == "fail" and ev.get("Test"):
            failed.add(ev["Test"])
        elif ev["Action"] == "run" and ev.get("Test"):
            runs[ev["Test"]] = runs.get(ev["Test"], 0) + 1
    return "".join(text), {k: "".join(v) for k, v in by_test.items()}, failed, runs


def of_test(out, test):
    """The lines of the named test itself. out is that test's own output,
    as go test -json attributes it: not another test's, and not a
    subtest's, whose findings are not the named test's."""
    return out.split("\n")


def own_lines(out, test):
    return [m.group(1).rstrip() for m in map(OWN.match, of_test(out, test)) if m]


def differences(out, test):
    """Every difference the test printed, as (scenario, node line, model
    line). The message is three lines: the test's own line naming the
    scenario, then the node's line and the model's."""
    lines = of_test(out, test)
    found = []
    for i, line in enumerate(lines):
        m = OWN.match(line)
        m = m and re.fullmatch(r"(\S+): the node and the model disagree", m.group(1).rstrip())
        if not m or i + 2 >= len(lines):
            continue
        node = re.fullmatch(r"\s+node:  (.*)", lines[i + 1])
        model = re.fullmatch(r"\s+model: (.*)", lines[i + 2])
        if node and model:
            found.append((m.group(1), node.group(1).rstrip(), model.group(1).rstrip()))
    return found


def first_evidence(out, expect, test):
    """The difference the fault must be caught by, if the test printed it."""
    scenario, node, model, check = expect
    if (scenario, node, model) not in differences(out, test):
        return []
    found = ["    %s: the node and the model disagree" % scenario, "    node:  " + node, "    model: " + model]
    if check is not None:
        lines = [l for l in own_lines(out, test) if re.fullmatch(check, l)]
        if not lines:
            return []
        found.append("    " + lines[0][:200])
    return found


STOP = {"launching": False, "pending": None, "stopping": False}


def stopped(code):
    """The handler of a signal that tells this script to stop."""
    def handler(*_):
        # Once the script is on its way out, a further signal does nothing:
        # it must not cut short the stopping of the go tool. That is said
        # here, in the handler, so that there is no moment between the
        # first signal and the cleanup at which a second one could.
        if STOP["stopping"]:
            return
        if STOP["launching"]:
            STOP["pending"] = code
        else:
            STOP["stopping"] = True
            sys.exit(code)
    return handler


def run_test(cmd, cwd, env):
    """Run the go tool in a process group of its own, and take the whole
    group down if this script is stopped while it runs: the test binary and
    the model's container client are its children, not ours."""
    # A signal that comes while the tool is being started is kept until its
    # process is known here, and then acted on inside the try below.
    STOP["launching"] = True
    proc = subprocess.Popen(cmd, cwd=cwd, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, start_new_session=True)
    try:
        STOP["launching"] = False
        if STOP["pending"] is not None:
            STOP["stopping"] = True
            sys.exit(STOP["pending"])
        proc.stdout_text, proc.stderr_text = proc.communicate()
    except BaseException:
        # stopping now: a second signal must not cut the stopping short
        for s in (signal.SIGTERM, signal.SIGHUP, signal.SIGINT):
            signal.signal(s, signal.SIG_IGN)
        try:
            os.killpg(proc.pid, signal.SIGTERM)
        except OSError:
            pass
        proc.wait()
        raise
    return proc


def run(out_dir, names):
    known = [f[0] for f in FAULTS]
    unknown = [n for n in names if n not in known]
    if len(set(names)) != len(names):
        sys.exit("node_faults.py: a fault is named twice")
    if unknown or len(set(known)) != len(known):
        sys.exit("node_faults.py: no fault named %s" % ", ".join(unknown) if unknown else "node_faults.py: a fault name is used twice")
    os.makedirs(out_dir, exist_ok=True)
    # The copies and overlays of this run, in a directory no other run uses.
    # (The go tool is given the overlay by path, so it must stay until the
    # test has run; run.sh removes its whole scratch directory at the end.)
    # (made absolute: it is handed on to the test, which runs elsewhere)
    base = os.path.abspath(os.environ.get("BEND_SCRATCH") or os.path.join(HERE, ".scratch"))
    os.makedirs(base, exist_ok=True)
    scratch = tempfile.mkdtemp(prefix="node-faults-", dir=base)
    try:
        return run_in(out_dir, names, scratch)
    finally:
        shutil.rmtree(scratch, ignore_errors=True)


def run_in(out_dir, names, scratch):
    # the whole output of each fault's test, for reading afterwards
    full = os.path.join(out_dir, "node-faults-output")
    os.makedirs(full, exist_ok=True)
    log = open(os.path.join(out_dir, "node-faults.log"), "w")
    detail = open(os.path.join(out_dir, "node-faults-detail.log"), "w")
    bad = 0
    ran = 0
    for name, what, rel, old, new, test, expect in FAULTS:
        if names and name not in names:
            continue
        ran += 1
        src = os.path.join(ROOT, rel)
        text = open(src).read()
        if text.count(old) != 1:
            line = "STALE    %s: its text occurs %d times in %s, want 1" % (name, text.count(old), rel)
            bad += 1
            print(line, flush=True)
            log.write(line + "\n")
            continue
        copy = os.path.join(scratch, name + "-" + os.path.basename(rel))
        open(copy, "w").write(text.replace(old, new))
        overlay = os.path.join(scratch, name + ".json")
        json.dump({"Replace": {src: copy}}, open(overlay, "w"))
        env = dict(os.environ)
        env.pop("BEND_DEPTH", None)
        # what the test writes for the model goes into this run's directory
        # too, so that it is removed with it if the run is stopped
        env["BEND_SCRATCH"] = scratch
        # one execution of the test: a -cpu list in GOFLAGS would run it
        # once for each entry
        env["GOFLAGS"] = " ".join(f for f in env.get("GOFLAGS", "").split() if not f.startswith(("-cpu", "--cpu")))
        proc = run_test(
            ["go", "test", "-overlay", overlay, "-tags", "bendrules", "./test/bend/", "-run", "^%s$" % test, "-count=1", "-json", "-fullpath=false", "-timeout", "20m"],
            cwd=ROOT, env=env)
        # out is everything, for the checks on the run as a whole; mine is
        # what the named test itself printed, and all that counts as evidence
        out, by_test, failures, runs = events(proc.stdout_text)
        # The named test must have run exactly once: its evidence is then
        # of one execution, and not put together from two.
        once = runs.get(test, 0) == 1 and not [n for n in runs if n != test and n.split("/")[0].startswith(test + "-")]
        out += proc.stderr_text
        mine = by_test.get(test, "")
        # The test can also fail because the model could not be run at all
        # (docker missing, the image gone). That is a failure of the tools,
        # not the fault being noticed.
        tooling = "[build failed]" in out or "[setup failed]" in out or "running the model:" in out or "the model printed" in out
        # A test binary that panicked or ran out of time did not finish its
        # comparison, whatever it had printed before: also not a catch.
        crashed = re.search(r"^(panic: |fatal error: |panic: test timed out)|\*\*\* Test killed", out, re.M)
        evidence = first_evidence(mine, expect, test)
        failed = proc.returncode != 0 and test in failures and once
        # (anywhere in the output: no test's unread call is evidence)
        unread = [m.group(1) for m in map(OWN.match, out.split("\n")) if m and re.match(UNREAD, m.group(1))]
        with open(os.path.join(full, name + ".log"), "w") as f:
            f.write(out)
        if unread:
            verdict = "ERROR   "
            bad += 1
            evidence = ["    the test could not read a call back, so it shows nothing about the fault:"] + ["    " + l[:200] for l in unread[:3]]
        elif crashed and not tooling:
            verdict = "ERROR   "
            bad += 1
            evidence = ["    the test did not finish: " + crashed.group(0).strip()]
        elif tooling:
            verdict = "ERROR   "
            bad += 1
            evidence = ["    " + l.strip()[:200] for l in out.split("\n") if "running the model:" in l or "the model printed" in l or "failed]" in l][:6]
        elif failed and evidence:
            verdict = "caught  "
        elif proc.returncode != 0:
            verdict = "ERROR   "
            bad += 1
            evidence = ["    the test failed, but did not print the difference this fault must be caught by:",
                        "    %s: node %s, model %s" % expect[:3]] + (["    and the line: " + expect[3]] if expect[3] else []) + ["    what it printed first:"] + ["    " + l[:200] for l in own_lines(mine, test)[:4]] + [
                "    " + re.sub(r"^\s*[a-z_]+_test\.go:\d+: ", "", l).strip()[:200]
                for l in out.split("\n") if "outside the rule" in l or "panic" in l and not l.startswith("t=")][:4]
        else:
            verdict = "MISSED  "
            bad += 1
            evidence = []
        differ = len(differences(mine, test))
        line = "%s %s: the node %s (%s); %s fails" % (verdict, name, what, rel, test)
        if verdict == "caught  " and differ:
            line += ", %d scenarios differ" % differ
        print(line, flush=True)
        log.write(line + "\n")
        detail.write("=== %s: the node %s\n  file   : %s\n" % (name, what, rel))
        for label, piece in (("node", old), ("fault", new)):
            for l in (piece.strip("\n").split("\n") or ["(line removed)"]):
                detail.write("  %-7s: %s\n" % (label, l.strip()))
        detail.write("  test   : %s -> %s\n" % (test, verdict.strip()))
        detail.write("\n".join(evidence) + "\n\n")
    log.close()
    detail.close()
    if ran == 0:
        sys.exit("node_faults.py: no fault was run")
    return 1 if bad else 0


def main():
    # told to stop: leave through the finally that removes this run's files
    signal.signal(signal.SIGTERM, stopped(143))
    signal.signal(signal.SIGHUP, stopped(129))
    signal.signal(signal.SIGINT, stopped(130))
    if len(sys.argv) == 2 and sys.argv[1] == "--list":
        for name, what, rel, *_ in FAULTS:
            print("%s\t%s\t%s" % (name, rel, what))
        return 0
    if len(sys.argv) >= 3 and sys.argv[1] == "--run":
        return run(sys.argv[2], sys.argv[3:])
    sys.exit(__doc__)


if __name__ == "__main__":
    sys.exit(main())
