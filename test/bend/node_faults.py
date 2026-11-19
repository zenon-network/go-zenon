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
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))

# name, what the broken node does, file, text, replacement, test that must fail
FAULTS = [
    # The balance check alone is not what stops an overspend: with it gone,
    # SubBalance panics on the negative balance and the block is still
    # refused. So this fault makes the insufficient branch of applyTransfer
    # succeed, which lets the send through without the debit.
    ("overspend-accepted", "accepts a send above the balance", "vm/vm.go",
     "\tif !enoughFunds(vm.context, block) {\n\t\treturn constants.ErrInsufficientBalance\n\t}\n\n\tvm.context.SubBalance(",
     "\tif !enoughFunds(vm.context, block) {\n\t\treturn nil\n\t}\n\n\tvm.context.SubBalance(",
     "TestLedgerModelMatchesNode"),
    ("no-received-check", "lets an account receive a send it has received", "verifier/account_block.go",
     "\tif status {\n\t\treturn ErrABFromBlockAlreadyReceived",
     "\tif false && status {\n\t\treturn ErrABFromBlockAlreadyReceived",
     "TestLedgerModelMatchesNode"),
    ("no-recipient-rule", "lets any account receive a send", "verifier/account_block.go",
     "if !receiverMismatchTolerated(",
     "if false && !receiverMismatchTolerated(",
     "TestLedgerModelMatchesNode"),
    ("htlc-unlock-keeps-entry", "pays an HTLC unlock and keeps the entry", "vm/embedded/implementation/htlc.go",
     "\tcommon.DealWithErr(htlcInfo.Delete(context.Storage()))\n\thtlcLog.Debug(\"unlocked\"",
     "\thtlcLog.Debug(\"unlocked\"",
     "TestEscrowModelMatchesHtlc"),
    ("htlc-expired-unlock-succeeds", "reports an expired HTLC unlock as accepted, without paying", "vm/embedded/implementation/htlc.go",
     "\t\treturn nil, constants.ErrExpired\n",
     "\t\treturn nil, nil\n",
     "TestEscrowModelMatchesHtlc"),
    ("htlc-reclaim-before-expiry", "lets the maker reclaim an HTLC before it expires", "vm/embedded/implementation/htlc.go",
     "if momentum.Timestamp.Unix() < htlcInfo.ExpirationTime {",
     "if false && momentum.Timestamp.Unix() < htlcInfo.ExpirationTime {",
     "TestEscrowModelMatchesHtlc"),
    ("htlc-reclaim-by-anyone", "lets any account reclaim an expired HTLC", "vm/embedded/implementation/htlc.go",
     "if htlcInfo.TimeLocked != sendBlock.Address {",
     "if false && htlcInfo.TimeLocked != sendBlock.Address {",
     "TestEscrowModelMatchesHtlc"),
    ("ptlc-unlock-keeps-entry", "pays a PTLC unlock and keeps the entry", "vm/embedded/implementation/ptlc.go",
     "\tcommon.DealWithErr(ptlcInfo.Delete(context.Storage()))\n\tdeletePtlcReclaimLock(context, id)",
     "\tdeletePtlcReclaimLock(context, id)",
     "TestEscrowModelMatchesPtlc"),
    ("stake-cancel-keeps-amount", "pays a stake cancel and leaves the amount in the entry", "vm/embedded/implementation/stake.go",
     "\tstakeInfo.Amount = big.NewInt(0)\n",
     "",
     "TestEscrowModelMatchesStake"),
    ("fusion-cancel-keeps-entry", "pays a fusion cancel and keeps the entry", "vm/embedded/implementation/plasma.go",
     "\tcommon.DealWithErr(fusionInfo.Delete(context.Storage()))\n",
     "",
     "TestEscrowModelMatchesFusion"),
    ("refund-missing", "keeps the amount of a call the contract refuses", "vm/vm.go",
     "\tif sendBlock.Amount.Sign() > 0 {",
     "\tif false && sendBlock.Amount.Sign() > 0 {",
     "TestEscrowModelMatchesHtlc"),
    ("refund-doubled", "gives back twice the amount of a call the contract refuses", "vm/vm.go",
     "Amount:        new(big.Int).Set(sendBlock.Amount),",
     "Amount:        new(big.Int).Lsh(sendBlock.Amount, 1),",
     "TestEscrowModelMatchesHtlc"),
]


# What the comparison prints when it notices something wrong with the node:
# a scenario on which node and model differ, or one of the direct checks on
# the node. A test that fails without any of these failed for another
# reason (a crash, an error the scenario did not expect), and that is not
# the fault being noticed.
NOTICED = r"the node and the model disagree|status says|came back|second time|the contract held|went to account|accepted a registration|has not answered"


def first_evidence(out):
    """The first lines of the test's own output that show the failure."""
    keep = []
    for line in out.splitlines():
        if line.startswith("t="):  # the node's own log
            continue
        text = re.sub(r"^\s*[a-z_]+_test\.go:\d+: ", "", line)
        if re.search(NOTICED + r"|node:  |model: ", line):
            keep.append("    " + text.strip()[:200])
        if len(keep) >= 4:
            break
    return keep


def run(out_dir, names):
    os.makedirs(out_dir, exist_ok=True)
    scratch = os.path.join(HERE, ".scratch", "node-faults")
    os.makedirs(scratch, exist_ok=True)
    log = open(os.path.join(out_dir, "node-faults.log"), "w")
    detail = open(os.path.join(out_dir, "node-faults-detail.log"), "w")
    bad = 0
    for name, what, rel, old, new, test in FAULTS:
        if names and name not in names:
            continue
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
        proc = subprocess.run(
            ["go", "test", "-overlay", overlay, "-tags", "bendrules", "./test/bend/", "-run", "^%s$" % test, "-count=1", "-v", "-timeout", "20m"],
            cwd=ROOT, env=env, capture_output=True, text=True)
        out = proc.stdout + proc.stderr
        # The test can also fail because the model could not be run at all
        # (docker missing, the image gone). That is a failure of the tools,
        # not the fault being noticed.
        tooling = "[build failed]" in out or "[setup failed]" in out or "running the model:" in out or "the model printed" in out
        evidence = first_evidence(out)
        if tooling:
            verdict = "ERROR   "
            bad += 1
            evidence = ["    " + l.strip()[:200] for l in out.splitlines() if "running the model:" in l or "the model printed" in l or "failed]" in l][:6]
        elif proc.returncode != 0 and "--- FAIL: " + test in out and re.search(NOTICED, out):
            verdict = "caught  "
        elif proc.returncode != 0:
            verdict = "ERROR   "
            bad += 1
            evidence = ["    the test failed, but not on a difference or a check of the node:"] + [
                "    " + re.sub(r"^\s*[a-z_]+_test\.go:\d+: ", "", l).strip()[:200]
                for l in out.splitlines() if "outside the rule" in l or "panic" in l and not l.startswith("t=")][:4]
        else:
            verdict = "MISSED  "
            bad += 1
            evidence = []
        differ = len(re.findall(r"the node and the model disagree", out))
        line = "%s %s: the node %s (%s); %s fails" % (verdict, name, what, rel, test)
        if verdict == "caught  " and differ:
            line += ", %d scenarios differ" % differ
        print(line, flush=True)
        log.write(line + "\n")
        detail.write("=== %s: the node %s\n  file   : %s\n" % (name, what, rel))
        for label, piece in (("node", old), ("fault", new)):
            for l in (piece.strip("\n").splitlines() or ["(line removed)"]):
                detail.write("  %-7s: %s\n" % (label, l.strip()))
        detail.write("  test   : %s -> %s\n" % (test, verdict.strip()))
        detail.write("\n".join(evidence) + "\n\n")
    log.close()
    detail.close()
    return 1 if bad else 0


def main():
    if len(sys.argv) == 2 and sys.argv[1] == "--list":
        for name, what, rel, *_ in FAULTS:
            print("%s\t%s\t%s" % (name, rel, what))
        return 0
    if len(sys.argv) >= 3 and sys.argv[1] == "--run":
        return run(sys.argv[2], sys.argv[3:])
    sys.exit(__doc__)


if __name__ == "__main__":
    sys.exit(main())
