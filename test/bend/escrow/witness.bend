import Base
import ./escrow.bend as E

# One witness per mutant of mutants.py: a single case of a named law,
# computed. On the model every witness is True. With its mutant put into
# the model, the witness must be False: that is what shows a law is false
# with the fault, and not merely that a proof no longer fits the text.
# run.sh (step 3) checks both.
#
# This file reads the model only, not LAWS.bend or PROOF.bend: with a
# mutant in the model the proofs do not check, and the witness must still
# run. Each witness names the law it is a case of, and computes that law's
# own statement at one input.

# Account 0 holds 10; nothing is locked; the time is 100.
def start() -> E.Vault:
  E.Vault{[10n, 0n, 0n], [], 100n, 0n}

# Entry 0: made by account 0 for account 1, holds 6, expires at 120.
def e0() -> E.Entry:
  E.Entry{0n, 0n, 1n, 6n, 120n}

# Entry 1: made by account 0 for account 2, holds 3, expires at 120.
def e1() -> E.Entry:
  E.Entry{1n, 0n, 2n, 3n, 120n}

def one() -> +List<E.Entry>:
  [e0()]

def two() -> +List<E.Entry>:
  [e0(), e1()]

# Account 0 has staked 6 until 120, and cancelled it at 120.
def cancelled() -> E.Vault:
  E.run([E.Create{0n, 0n, 6n, 120n}, E.Tick{}, E.Tick{}, E.Cancel{0n, 0n}], start())

# Readings, written here again because this file cannot read LAWS.bend.
def nothing_paid(+f: E.Found) -> Bool:
  match f:
    case E.Gone{}:
      True{}
    case E.Paid{to, amount, entries}:
      False{}

# The amount paid; or other when nothing was paid.
def paid(+f: E.Found, +other: Nat) -> Nat:
  match f:
    case E.Gone{}:
      other
    case E.Paid{to, amount, entries}:
      amount

# The number of entries after a payout, counting the paid one back in; or
# n when nothing was paid.
def left(+f: E.Found, +n: Nat) -> Nat:
  match f:
    case E.Gone{}:
      n
    case E.Paid{to, amount, entries}:
      1n+E.Entries.count(entries)

# The entries after a payout; or other when nothing was paid.
def rest(+f: E.Found, +other: +List<E.Entry>) -> +List<E.Entry>:
  match f:
    case E.Gone{}:
      other
    case E.Paid{to, amount, entries}:
      entries


def same_nats(+xs: +List<Nat>, +ys: +List<Nat>) -> Bool:
  match xs ys:
    case Nil{} Nil{}:
      True{}
    case Nil{} Con{b, u}:
      False{}
    case Con{a, t} Nil{}:
      False{}
    case Con{a, t} Con{b, u}:
      Bool.and(Nat.is_eq(a, b), same_nats(t, u))

def same_entry(+a: E.Entry, +b: E.Entry) -> Bool:
  match a b:
    case E.Entry{i, m, p, x, e} E.Entry{j, n, q, y, f}:
      Bool.and(Nat.is_eq(i, j), Bool.and(Nat.is_eq(m, n), Bool.and(Nat.is_eq(p, q), Bool.and(Nat.is_eq(x, y), Nat.is_eq(e, f)))))

def same_entries(+xs: +List<E.Entry>, +ys: +List<E.Entry>) -> Bool:
  match xs ys:
    case Nil{} Nil{}:
      True{}
    case Nil{} Con{b, u}:
      False{}
    case Con{a, t} Nil{}:
      False{}
    case Con{a, t} Con{b, u}:
      Bool.and(same_entry(a, b), same_entries(t, u))

def same_vault(+a: E.Vault, +b: E.Vault) -> Bool:
  match a b:
    case E.Vault{bs, es, t, n} E.Vault{cs, fs, u, m}:
      Bool.and(same_nats(bs, cs), Bool.and(same_entries(es, fs), Bool.and(Nat.is_eq(t, u), Nat.is_eq(n, m))))

# The call is accepted and leaves exactly the vault want.
def accepted_as(+o: E.Out, +want: E.Vault) -> Bool:
  match o:
    case E.Refused{}:
      False{}
    case E.Accepted{vault}:
      same_vault(vault, want)

# What the entry asked for holds; zero when it is not there.
def holds(+es: +List<E.Entry>, +want: Nat) -> Nat:
  match es:
    case Nil{}:
      0n
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      Bool.pick(Nat, Nat.is_eq(id, want), amount, holds(t, want))

# LAW 2: an unlock that pays leaves one entry fewer.
def unlock_keeps_entry() -> Bool:
  Nat.is_eq(left(E.Entries.unlock(one(), 0n, True{}, 100n), 1n), 1n)

# LAW 3: a reclaim that pays leaves one entry fewer.
def reclaim_keeps_entry() -> Bool:
  Nat.is_eq(left(E.Entries.reclaim(one(), 0n, 0n, 120n), 1n), 1n)

# LAW 9: a claim that pays leaves one entry fewer.
def claim_keeps_entry() -> Bool:
  Nat.is_eq(left(E.Entries.claim(one(), 0n, True{}, 2n, 100n), 1n), 1n)

# LAW 12: after a cancel that pays, the entry holds nothing.
def cancel_keeps_amount() -> Bool:
  Nat.is_eq(holds(rest(E.Entries.cancel(one(), 0n, 0n, 120n), Nil{}), 0n), 0n)

# LAW 13: a failed call of 6 by account 0 leaves the vault as it was.
def refund_doubled() -> Bool:
  same_vault(E.apply(start(), E.Failed{0n, 6n}), start())

def refund_missing() -> Bool:
  same_vault(E.apply(start(), E.Failed{0n, 6n}), start())

# LAW 14: an unlock of entry 0, which holds 6, pays 6, with entry 1 beside
# it holding 3.
def payout_from_another() -> Bool:
  Nat.is_eq(paid(E.Entries.unlock(two(), 0n, True{}, 100n), 6n), 6n)

# LAW 27: a claim at the expiry pays nothing, with a valid signature.
def claim_after_expiry() -> Bool:
  nothing_paid(E.Entries.claim(one(), 0n, True{}, 2n, 120n))

# LAW 32: a failed call of 6 that the balance of 10 covers is accepted,
# and the vault afterwards is the vault before.
def failed_call_refused() -> Bool:
  accepted_as(E.step(start(), E.Failed{0n, 6n}), start())

# LAW 33: a second cancel of the cancelled stake is accepted, pays zero
# and leaves the vault as the first cancel left it.
def second_cancel_refused() -> Bool:
  accepted_as(E.step(cancelled(), E.Cancel{0n, 0n}), E.Vault{[10n, 0n, 0n], [E.Entry{0n, 0n, 0n, 0n, 120n}], 120n, 1n})

# LAW 1: after a create of 6 and its unlock, all the value is still 10.
def payout_doubled() -> Bool:
  Nat.is_eq(E.worth(E.run([E.Create{0n, 1n, 6n, 120n}, E.Unlock{0n, True{}}], start())), 10n)

# LAW 1: after a create of 6, all the value is still 10.
def entry_doubled() -> Bool:
  Nat.is_eq(E.worth(E.run([E.Create{0n, 1n, 6n, 120n}], start())), 10n)

# LAW 18: an unlock of entry 1 leaves entry 0 as it was.
def payout_drops_another() -> Bool:
  same_entries(rest(E.Entries.unlock(two(), 1n, True{}, 100n), one()), one())

# LAW 28: a reclaim by the maker before the expiry pays nothing.
def reclaim_before_expiry() -> Bool:
  nothing_paid(E.Entries.reclaim(one(), 0n, 0n, 100n))

# LAW 30: a reclaim after the expiry by account 2, which did not make the
# entry, pays nothing.
def reclaim_by_anyone() -> Bool:
  nothing_paid(E.Entries.reclaim(one(), 0n, 2n, 120n))

# LAW 22: an unlock without the preimage pays nothing, before the expiry.
def unlock_without_preimage() -> Bool:
  nothing_paid(E.Entries.unlock(one(), 0n, False{}, 100n))

# LAW 26: an unlock at the expiry pays nothing, with the right preimage.
def unlock_after_expiry() -> Bool:
  nothing_paid(E.Entries.unlock(one(), 0n, True{}, 120n))

def main() -> IO(Unit):
  do IO<Unit>:
    IO.print("unlock-keeps-entry " ++ Bool.show(unlock_keeps_entry()))
    IO.print("reclaim-keeps-entry " ++ Bool.show(reclaim_keeps_entry()))
    IO.print("claim-keeps-entry " ++ Bool.show(claim_keeps_entry()))
    IO.print("cancel-keeps-amount " ++ Bool.show(cancel_keeps_amount()))
    IO.print("refund-doubled " ++ Bool.show(refund_doubled()))
    IO.print("refund-missing " ++ Bool.show(refund_missing()))
    IO.print("payout-from-another " ++ Bool.show(payout_from_another()))
    IO.print("claim-after-expiry " ++ Bool.show(claim_after_expiry()))
    IO.print("failed-call-refused " ++ Bool.show(failed_call_refused()))
    IO.print("second-cancel-refused " ++ Bool.show(second_cancel_refused()))
    IO.print("payout-doubled " ++ Bool.show(payout_doubled()))
    IO.print("entry-doubled " ++ Bool.show(entry_doubled()))
    IO.print("payout-drops-another " ++ Bool.show(payout_drops_another()))
    IO.print("reclaim-before-expiry " ++ Bool.show(reclaim_before_expiry()))
    IO.print("reclaim-by-anyone " ++ Bool.show(reclaim_by_anyone()))
    IO.print("unlock-without-preimage " ++ Bool.show(unlock_without_preimage()))
    IO.print("unlock-after-expiry " ++ Bool.show(unlock_after_expiry()))
