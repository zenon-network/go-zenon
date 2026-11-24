#!/usr/bin/env python3
"""Merge coverage files written by the driver or the smoke test into coverage.tsv.

    E2E_COVERAGE="$PWD/run.tsv" go test -tags e2e ./test/e2e/ ...
    test/inventory/record.py run.tsv [more.tsv ...]

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

if len(sys.argv) < 2:
    sys.exit("usage: record.py <coverage-file> [...]")
new, problems = [], []
for src in sys.argv[1:]:
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

added = 0
for key, token in new:
    if token not in rows[key]:
        rows[key].append(token)
        added += 1


def sort_key(token):
    cls, _, case = token.partition(":")
    return (int(cls[1:]), case)


fd, tmp = tempfile.mkstemp(dir=DIR)
with os.fdopen(fd, "w") as f:
    for key in order:
        f.write("%s\t%s\t%s\n" % (key[0], key[1], " ".join(sorted(rows[key], key=sort_key))))
os.replace(tmp, COV)
print("%d tokens added to %s from %d file(s)" % (added, os.path.relpath(COV), len(sys.argv) - 1))
