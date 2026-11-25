#!/usr/bin/env python3
"""Merge coverage files written by the driver or the smoke test into coverage.tsv.

    E2E_COVERAGE="$PWD/run.tsv" go test -tags e2e ./test/e2e/ ...
    test/inventory/record.py run.tsv [more.tsv ...]
    test/inventory/record.py --replace smoke.tsv stock.tsv fast.tsv ...

By default the rows are added: no token is ever removed, so a case that stops
exercising an item, or is renamed, leaves its token behind. --replace instead
rebuilds every item's tokens from the inputs alone and prints each token it
drops. Give it the coverage files of one complete baseline run (the smoke test
and every driver stage); with fewer, the cases that did not run lose their
tokens.

Each input row is kind, item, "class:case", written for a case that passed.
Every input is read and checked first. If any row is malformed, names an item
the inventory does not have, or names one that has a disposition, nothing is
written and the exit code is 1: a typo in a case cannot invent coverage, and a
bad file cannot half-apply. The map is replaced in one step.
"""
import os, re, sys, tempfile

DIR = os.path.dirname(os.path.abspath(__file__))
COV, DISP = os.path.join(DIR, "coverage.tsv"), os.path.join(DIR, "dispositions.tsv")
TOKEN = re.compile(r"^E([1-9]|1[0-2]):[A-Za-z0-9][A-Za-z0-9.-]*$")

rows, order = {}, []
for n, line in enumerate(open(COV), 1):
    parts = line.rstrip("\n").split("\t")
    if len(parts) != 3 or (parts[0], parts[1]) in rows:
        sys.exit("record: coverage.tsv line %d is malformed or repeated; run test/inventory/check.sh" % n)
    rows[(parts[0], parts[1])] = parts[2].split()
    order.append((parts[0], parts[1]))
disposed = set()
if os.path.exists(DISP):
    for line in open(DISP):
        parts = line.rstrip("\n").split("\t")
        if not line.startswith("#") and len(parts) == 3:
            disposed.add((parts[0], parts[1]))

args = sys.argv[1:]
replace = "--replace" in args
args = [a for a in args if a != "--replace"]
if not args:
    sys.exit("usage: record.py [--replace] <coverage-file> [...]")
new, problems = [], []
for src in args:
    if not os.path.isfile(src):
        problems.append("%s: no such file (E2E_COVERAGE must be an absolute path: the driver runs in test/e2e)" % src)
        continue
    for n, line in enumerate(open(src), 1):
        if not line.strip():
            continue
        parts = line.rstrip("\n").split("\t")
        where = "%s line %d" % (src, n)
        if len(parts) != 3 or not TOKEN.match(parts[2]):
            problems.append("%s: malformed row %r" % (where, line.rstrip("\n")))
        elif (parts[0], parts[1]) not in rows:
            problems.append("%s: %s\t%s is not in the inventory" % (where, parts[0], parts[1]))
        elif (parts[0], parts[1]) in disposed:
            problems.append("%s: %s\t%s has a disposition; it cannot be covered" % (where, parts[0], parts[1]))
        else:
            new.append(((parts[0], parts[1]), parts[2]))
if problems:
    print("\n".join(problems))
    sys.exit("record: nothing was written")

old = {key: list(tokens) for key, tokens in rows.items()}
if replace:
    rows = {key: [] for key in rows}
added = 0
for key, token in new:
    if token not in rows[key]:
        rows[key].append(token)
        added += 1
dropped = [(key, t) for key in order for t in old[key] if t not in rows[key]]
for key, t in dropped:
    print("DROPPED %s\t%s\t%s" % (key[0], key[1], t))
if replace:
    added = sum(1 for key in order for t in rows[key] if t not in old[key])


def sort_key(token):
    cls, _, case = token.partition(":")
    return (int(cls[1:]), case)


fd, tmp = tempfile.mkstemp(dir=DIR)
with os.fdopen(fd, "w") as f:
    for key in order:
        f.write("%s\t%s\t%s\n" % (key[0], key[1], " ".join(sorted(rows[key], key=sort_key))))
os.replace(tmp, COV)
print("%d tokens added%s to %s from %d file(s)" % (added, ", %d dropped" % len(dropped) if replace else "", os.path.relpath(COV), len(args)))
