#!/usr/bin/env python3
"""Compare the source-derived inventory with coverage.tsv and report gaps.

    test/inventory/check.sh            # counts per kind; exit 1 while anything is open
    test/inventory/check.sh --list     # also name every item without a case
    test/inventory/check.sh --sync     # after a code change: add new items, drop removed ones

coverage.tsv: kind, item, cases. A case token is "<class>:<case id>", for
example "E4:PL-3" (classes E1 to E12, docs/TESTING.md section 1).
dispositions.tsv: kind, item, reason, for an item that can never have a passing
case, such as an ABI entry with no implementation. Such an item must have no
case tokens and is counted on its own.

The check fails on: an item with no case and no disposition; an item of the
source missing from the map, or one in the map that the source no longer has; a
duplicate or malformed row; a token that is not of the form above; a disposed
item that carries tokens; an inventory that came out empty for any kind.
--sync prints what it adds and drops before it rewrites the map, and never
touches the tokens of an item that stays. It writes nothing when the map or the
dispositions have a problem: a duplicate or malformed row, a disposition for an
item the source does not have, or one for an item that has cases.
"""
import os, re, subprocess, sys, tempfile

DIR = os.path.dirname(os.path.abspath(__file__))
COV, DISP = os.path.join(DIR, "coverage.tsv"), os.path.join(DIR, "dispositions.tsv")
KINDS = ["contract-method", "rpc-method", "subscription", "spork", "verifier-rule", "protocol-msg",
         "protocol-error", "error", "cli-flag", "config-field"]
TOKEN = re.compile(r"^E([1-9]|1[0-2]):[A-Za-z0-9][A-Za-z0-9.-]*$")


def fail(msg):
    print("check: " + msg, file=sys.stderr)
    sys.exit(2)


def inventory():
    run = subprocess.run([os.path.join(DIR, "inventory.sh")], capture_output=True, text=True)
    if run.returncode != 0 or run.stderr.strip():
        fail("inventory.sh failed (exit %d): %s" % (run.returncode, run.stderr.strip()[:300]))
    items, seen = [], set()
    for line in run.stdout.splitlines():
        parts = line.split("\t")
        if len(parts) != 2 or parts[0] not in KINDS or not parts[1]:
            fail("inventory.sh printed a malformed line: %r" % line)
        if tuple(parts) in seen:
            fail("inventory.sh printed %s twice; two things in the source share that name" % "\t".join(parts))
        seen.add(tuple(parts))
        items.append(tuple(parts))
    for k in KINDS:
        if not any(i[0] == k for i in items):
            fail("the inventory has no %s at all; the extraction for it is broken" % k)
    return items


def read_map():
    problems, rows = [], {}
    if not os.path.exists(COV):
        fail("%s does not exist; create it with --sync" % COV)
    for n, line in enumerate(open(COV), 1):
        parts = line.rstrip("\n").split("\t")
        if len(parts) != 3:
            problems.append("line %d: %d columns, want 3" % (n, len(parts)))
            continue
        key, tokens = (parts[0], parts[1]), parts[2].split()
        if key in rows:
            problems.append("line %d: %s listed twice" % (n, "\t".join(key)))
        bad = [t for t in tokens if not TOKEN.match(t)]
        if bad or len(set(tokens)) != len(tokens):
            problems.append("line %d: %s has malformed or repeated tokens: %s" % (n, "\t".join(key), " ".join(bad or tokens)))
        rows[key] = tokens
    return rows, problems


def read_dispositions():
    out = {}
    if os.path.exists(DISP):
        for n, line in enumerate(open(DISP), 1):
            if line.startswith("#") or not line.strip():
                continue
            parts = line.rstrip("\n").split("\t")
            if len(parts) != 3 or not parts[2].strip():
                fail("dispositions.tsv line %d: want kind, item, reason" % n)
            if (parts[0], parts[1]) in out:
                fail("dispositions.tsv line %d: %s\t%s listed twice" % (n, parts[0], parts[1]))
            out[(parts[0], parts[1])] = parts[2]
    return out


def main():
    items = inventory()
    if "--sync" in sys.argv:
        # The map is validated before it is rewritten: a duplicate or malformed
        # row would otherwise lose its marks silently in the rewrite.
        rows, problems = read_map() if os.path.exists(COV) else ({}, [])
        if problems:
            print("\n".join(problems))
            fail("--sync: coverage.tsv has the problems above; nothing was written")
        # So are the dispositions: each must name an item of the source, once,
        # and an item that has cases cannot also be disposed of.
        disposed = read_dispositions()
        problems = ["disposition for %s\t%s, which the source does not have" % k for k in sorted(set(disposed) - set(items))]
        problems += ["%s\t%s has a disposition and case tokens; it cannot have both" % k for k in sorted(disposed) if rows.get(k)]
        if problems:
            print("\n".join(problems))
            fail("--sync: dispositions.tsv has the problems above; nothing was written")
        for key in sorted(set(items) - set(rows)):
            print("ADDED   %s\t%s" % key)
        for key in sorted(set(rows) - set(items)):
            print("DROPPED %s\t%s\t%s" % (key[0], key[1], " ".join(rows[key]) or "(no cases)"))
        fd, tmp = tempfile.mkstemp(dir=DIR)
        with os.fdopen(fd, "w") as f:
            for key in items:
                f.write("%s\t%s\t%s\n" % (key[0], key[1], " ".join(rows.get(key, []))))
        os.replace(tmp, COV)
    rows, problems = read_map()
    disposed = read_dispositions()
    missing = sorted(set(items) - set(rows))
    stale = sorted(set(rows) - set(items))
    problems += ["MISSING %s\t%s (in the source, not in the map; run --sync)" % k for k in missing]
    problems += ["STALE   %s\t%s (in the map, not in the source; run --sync)" % k for k in stale]
    problems += ["disposition for %s\t%s, which the source does not have" % k for k in sorted(set(disposed) - set(items))]
    problems += ["%s\t%s has a disposition and case tokens; it cannot have both" % k for k in sorted(disposed) if rows.get(k)]
    for p in problems:
        print(p)
    open_items = [k for k in items if k in rows and not rows[k] and k not in disposed]
    if "--list" in sys.argv:
        for k in open_items:
            print("OPEN    %s\t%s" % k)
        for k in sorted(disposed):
            print("N/A     %s\t%s\t%s" % (k[0], k[1], disposed[k]))
    print("\n%-16s %7s %8s %6s %6s" % ("kind", "items", "has-case", "n/a", "open"))
    tot = [0, 0, 0, 0]
    for kind in KINDS:
        mine = [k for k in items if k[0] == kind]
        row = [len(mine), sum(1 for k in mine if rows.get(k)), sum(1 for k in mine if k in disposed), sum(1 for k in open_items if k[0] == kind)]
        tot = [a + b for a, b in zip(tot, row)]
        print("%-16s %7d %8d %6d %6d" % tuple([kind] + row))
    print("%-16s %7d %8d %6d %6d" % tuple(["total"] + tot))
    print("\nhas-case means at least one case names the item; it does not mean every edge class is covered.")
    sys.exit(1 if problems or open_items else 0)


if __name__ == "__main__":
    main()
