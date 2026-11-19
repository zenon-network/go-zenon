#!/usr/bin/env python3
"""Deliberate faults for the Bend models, used by run.sh.

A law that is proven about a model says something only if a wrong model
would fail it. Each mutant below puts one double-spend fault into a copy of
the model; run.sh then requires `bend PROOF.bend` to print SOME PROOFS FAIL.

    mutants.py --list <rule>                name<TAB>description, one per line
    mutants.py --apply <name> <copy dir>    edit the copy of test/bend in place

A mutant is chosen so that a law is false with it, not merely so that the
proof text no longer fits. A mutant replaces one exact piece of text. If the
model changes and the text is no longer there, --apply stops with an error,
so a mutant cannot go stale without anyone noticing.
"""
import os
import sys

MUTANTS = {
    "ledger": [
        ("no-balance-check", "a send above the balance is accepted", "ledger.bend",
         "    case False{}:\n      None{}\n    case True{}:\n      Some{Nat.sub(h, amt)}",
         "    case False{}:\n      Some{0n}\n    case True{}:\n      Some{Nat.sub(h, amt)}"),
        ("no-debit", "a send leaves the sender's balance as it was", "ledger.bend",
         "    case True{}:\n      Some{Nat.sub(h, amt)}",
         "    case True{}:\n      Some{h}"),
        ("no-received-check", "a send can be received again by the same account", "ledger.bend",
         "  match seen:\n    case True{}:\n      Miss{}",
         "  match seen:\n    case True{}:\n      Hit{amount, Con{Send{id, to, amount, True{}, Con{acc, got}}, t}}"),
        ("no-received-mark", "a receive leaves no mark", "ledger.bend",
         "      Hit{amount, Con{Send{id, to, amount, True{}, Con{acc, got}}, t}}",
         "      Hit{amount, Con{Send{id, to, amount, True{}, got}, t}}"),
        ("no-recipient-rule", "any account may receive a send", "ledger.bend",
         "  match mine:\n    case False{}:\n      Miss{}",
         "  match mine:\n    case False{}:\n      Sends.recv.c3(Sends.mem(acc, got), acc, id, to, amount, got, t)"),
        ("double-credit", "a receive credits twice the amount", "ledger.bend",
         "Accepted{State{Bal.credit(acc, bals, amount),",
         "Accepted{State{Bal.credit(acc, bals, Nat.add(amount, amount)),"),
        ("send-not-recorded", "a send records twice its amount in transit", "ledger.bend",
         "Con{Send{next, to, amount, False{}, Nil{}}, sends}",
         "Con{Send{next, to, Nat.add(amount, amount), False{}, Nil{}}, sends}"),
    ],
    "escrow": [
        ("unlock-keeps-entry", "an unlock pays and leaves the entry in place", "escrow.bend",
         "Entries.here(Entries.may_unlock(key, now, expiry), payee, amount, t)",
         "Entries.here(Entries.may_unlock(key, now, expiry), payee, amount, Con{Entry{id, maker, payee, amount, expiry}, t})"),
        ("reclaim-keeps-entry", "a reclaim pays and leaves the entry in place", "escrow.bend",
         "Entries.here(Entries.may_reclaim(from, maker, now, expiry), maker, amount, t)",
         "Entries.here(Entries.may_reclaim(from, maker, now, expiry), maker, amount, Con{Entry{id, maker, payee, amount, expiry}, t})"),
        ("claim-keeps-entry", "a PTLC unlock pays and leaves the entry in place", "escrow.bend",
         "Entries.here(Entries.may_unlock(key, now, expiry), to, amount, t)",
         "Entries.here(Entries.may_unlock(key, now, expiry), to, amount, Con{Entry{id, maker, payee, amount, expiry}, t})"),
        ("cancel-keeps-amount", "a stake cancel pays and leaves the amount in the entry", "escrow.bend",
         "maker, amount, Con{Entry{id, maker, payee, 0n, expiry}, t})",
         "maker, amount, Con{Entry{id, maker, payee, amount, expiry}, t})"),
        ("refund-doubled", "a failed call gives back twice the amount", "escrow.bend",
         "Accepted{Vault{L.Bal.credit(from, bals, amount), entries, now, next}}",
         "Accepted{Vault{L.Bal.credit(from, bals, Nat.add(amount, amount)), entries, now, next}}"),
        ("refund-missing", "a failed call keeps the amount", "escrow.bend",
         "Accepted{Vault{L.Bal.credit(from, bals, amount), entries, now, next}}",
         "Accepted{Vault{bals, entries, now, next}}"),
        ("payout-from-another", "an unlock pays the amount of the entry and of the next one", "escrow.bend",
         "Entries.here(Entries.may_unlock(key, now, expiry), payee, amount, t)",
         "Entries.here(Entries.may_unlock(key, now, expiry), payee, Nat.add(amount, Entries.held(t)), Nil{})"),
        ("payout-doubled", "a payout credits twice the entry's amount", "escrow.bend",
         "Accepted{Vault{L.Bal.credit(to, bals, amount), entries, now, next}}",
         "Accepted{Vault{L.Bal.credit(to, bals, Nat.add(amount, amount)), entries, now, next}}"),
        ("entry-doubled", "a create records twice the amount it took", "escrow.bend",
         "Con{Entry{next, from, payee, amount, expiry}, entries}",
         "Con{Entry{next, from, payee, Nat.add(amount, amount), expiry}, entries}"),
        ("payout-drops-another", "paying one entry removes another one as well", "escrow.bend",
         "      Paid{to, amount, Con{e, entries}}",
         "      Paid{to, amount, entries}"),
        ("reclaim-before-expiry", "the maker can reclaim before the expiry", "escrow.bend",
         "  Bool.and(Nat.is_eq(from, maker), Nat.is_ge(now, expiry))",
         "  Nat.is_eq(from, maker)"),
        ("reclaim-by-anyone", "any account can reclaim after the expiry", "escrow.bend",
         "  Bool.and(Nat.is_eq(from, maker), Nat.is_ge(now, expiry))",
         "  Nat.is_ge(now, expiry)"),
        ("unlock-without-preimage", "an unlock with the wrong preimage pays", "escrow.bend",
         "  Bool.and(key, Nat.is_lt(now, expiry))",
         "  Nat.is_lt(now, expiry)"),
        ("unlock-after-expiry", "an unlock after the expiry pays", "escrow.bend",
         "  Bool.and(key, Nat.is_lt(now, expiry))",
         "  key"),
    ],
}


def main():
    if len(sys.argv) == 3 and sys.argv[1] == "--list":
        rule = os.path.basename(os.path.normpath(sys.argv[2]))
        for name, what, *_ in MUTANTS.get(rule, []):
            print("%s\t%s" % (name, what))
        return 0
    if len(sys.argv) == 4 and sys.argv[1] == "--apply":
        for rule, mutants in MUTANTS.items():
            for name, _, fname, old, new in mutants:
                if name != sys.argv[2]:
                    continue
                path = os.path.join(sys.argv[3], rule, fname)
                with open(path) as f:
                    text = f.read()
                if text.count(old) != 1:
                    sys.exit("mutant %s: its text occurs %d times in %s, want 1" % (name, text.count(old), fname))
                with open(path, "w") as f:
                    f.write(text.replace(old, new))
                return 0
        sys.exit("no mutant named %s" % sys.argv[2])
    sys.exit(__doc__)


if __name__ == "__main__":
    sys.exit(main())
