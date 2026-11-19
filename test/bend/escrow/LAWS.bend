import Base
import ./escrow.bend as E

# The rule: a contract that holds value in entries pays each entry once.
#
# LAW 1. The vault keeps its value. Take any balances, any entries, any
# time, and any sequence of calls: creates, unlocks with the right or the
# wrong preimage, reclaims by anybody, momentums, valid or not, in any
# order. After the contract has done the ones it accepts and refused the
# others, the value held by the accounts plus the value held in entries is
# what it was before.
#
# Paying one entry twice, in any of its forms, would break this: an unlock
# twice, a reclaim twice, an unlock and then a reclaim, or a payout larger
# than the entry would each leave more value than there was.
law vault_keeps_value:
  for +cs: +List<E.Call>
  for +v: E.Vault
  {E.worth(E.run(cs, v)) == E.worth(v) : Nat}

# The number of entries after a payout, counting the paid one back in; or
# n when nothing was paid.
def left(+f: E.Found, +n: Nat) -> Nat:
  match f:
    case E.Gone{}:
      n
    case E.Paid{to, amount, entries}:
      1n+E.Entries.count(entries)

# LAW 2. A payout takes its entry with it. Whenever an unlock pays, the
# contract holds exactly one entry fewer; whenever it does not pay, the
# entries are as many as before. There is no paying and keeping.
law unlock_takes_the_entry:
  for +es: +List<E.Entry>
  for +want: Nat
  for +key: Bool
  for +now: Nat
  {left(E.Entries.unlock(es, want, key, now), E.Entries.count(es)) == E.Entries.count(es) : Nat}

# LAW 3. The same for a reclaim.
law reclaim_takes_the_entry:
  for +es: +List<E.Entry>
  for +want: Nat
  for +from: Nat
  for +now: Nat
  {left(E.Entries.reclaim(es, want, from, now), E.Entries.count(es)) == E.Entries.count(es) : Nat}

# LAW 4. At no time can an entry be both unlocked and reclaimed: unlock
# needs the time to be before the expiry, reclaim needs it not to be.
law windows_do_not_overlap:
  for +now: Nat
  for +expiry: Nat
  {Bool.and(Nat.is_lt(now, expiry), Nat.is_ge(now, expiry)) == False{} : Bool}

# The vault of LAWS 5 and 6: account 0 holds 10, nothing is locked, the
# time is 100.
def start() -> E.Vault:
  E.Vault{[10n, 0n, 0n], [], 100n, 0n}

# Account 0 locks 6 for account 1 until 120. A reclaim before the expiry
# and an unlock with the wrong preimage are refused. The unlock with the
# right one pays account 1. A second unlock, and a reclaim after the
# expiry, find nothing to pay.
def unlocked_then_tried_again() -> +List<E.Call>:
  [E.Create{0n, 1n, 6n, 120n}, E.Reclaim{0n, 0n}, E.Unlock{0n, False{}}, E.Unlock{0n, True{}}, E.Unlock{0n, True{}}, E.Tick{}, E.Tick{}, E.Reclaim{0n, 0n}]

# LAW 5. Unlocked once: 4 and 6, and no entry left.
law unlock_pays_once:
  {E.run(unlocked_then_tried_again(), start()) == E.Vault{[4n, 6n, 0n], [], 120n, 1n} : E.Vault}

# The same entry, left to expire. An unlock after the expiry and a reclaim
# by an account that did not make the entry are refused. The maker's
# reclaim pays the 6 back. A second reclaim, and an unlock, find nothing.
def reclaimed_then_tried_again() -> +List<E.Call>:
  [E.Create{0n, 1n, 6n, 120n}, E.Tick{}, E.Tick{}, E.Unlock{0n, True{}}, E.Reclaim{2n, 0n}, E.Reclaim{0n, 0n}, E.Reclaim{0n, 0n}, E.Unlock{0n, True{}}]

# LAW 6. Reclaimed once: the 10 are back, and no entry left.
law reclaim_pays_once:
  {E.run(reclaimed_then_tried_again(), start()) == E.Vault{[10n, 0n, 0n], [], 120n, 1n} : E.Vault}

# LAW 7. The wrong preimage pays nothing. The entry is asked for with the
# wrong preimage only, expires, and goes back to its maker whole.
law wrong_preimage_pays_nothing:
  {E.run([E.Create{0n, 1n, 6n, 120n}, E.Unlock{0n, False{}}, E.Tick{}, E.Tick{}, E.Reclaim{0n, 0n}], start()) == E.Vault{[10n, 0n, 0n], [], 120n, 1n} : E.Vault}

# LAW 8. Only the maker reclaims. After the expiry an account that did not
# make the entry asks for it: nothing moves and the entry stays.
law only_the_maker_reclaims:
  {E.run([E.Create{0n, 1n, 6n, 120n}, E.Tick{}, E.Tick{}, E.Reclaim{2n, 0n}], start()) == E.Vault{[4n, 0n, 0n], [E.Entry{0n, 0n, 1n, 6n, 120n}], 120n, 1n} : E.Vault}

# LAW 9. The same as LAW 2 for a claim, the unlock of PTLC.
law claim_takes_the_entry:
  for +es: +List<E.Entry>
  for +want: Nat
  for +key: Bool
  for +to: Nat
  for +now: Nat
  {left(E.Entries.claim(es, want, key, to, now), E.Entries.count(es)) == E.Entries.count(es) : Nat}

# LAW 10. A claim pays once, and pays the account the signature names. A
# claim without a valid signature is refused. Account 2 claims with one and
# is paid the 6. Account 1 then claims with a valid signature of its own,
# and the maker reclaims after the expiry: both find nothing.
law claim_pays_once:
  {E.run([E.Create{0n, 1n, 6n, 120n}, E.Claim{0n, False{}, 2n}, E.Claim{0n, True{}, 2n}, E.Claim{0n, True{}, 1n}, E.Tick{}, E.Tick{}, E.Reclaim{0n, 0n}], start()) == E.Vault{[4n, 0n, 6n], [], 120n, 1n} : E.Vault}

# LAW 11. A stake is paid back once. Account 0 locks 6 until 120. A cancel
# before the expiry, and one by another account after it, are refused. The
# maker's cancel pays the 6 back and leaves the entry holding nothing. A
# second cancel of the cancelled stake pays zero.
law second_cancel_pays_zero:
  {E.run([E.Create{0n, 0n, 6n, 120n}, E.Cancel{0n, 0n}, E.Tick{}, E.Tick{}, E.Cancel{2n, 0n}, E.Cancel{0n, 0n}, E.Cancel{0n, 0n}, E.Cancel{0n, 0n}], start()) == E.Vault{[10n, 0n, 0n], [E.Entry{0n, 0n, 0n, 0n, 120n}], 120n, 1n} : E.Vault}

# The value an entry holds, by its number; zero when there is none.
def holds(+es: +List<E.Entry>, +want: Nat) -> Nat:
  match es:
    case Nil{}:
      0n
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      Bool.pick(Nat, Nat.is_eq(id, want), amount, holds(t, want))

# What the entry asked for holds after a cancel; zero when it was refused.
def holds_after(+f: E.Found, +want: Nat) -> Nat:
  match f:
    case E.Gone{}:
      0n
    case E.Paid{to, amount, entries}:
      holds(entries, want)

# LAW 12. After any cancel that pays, the entry it was asked for holds
# nothing, whatever it held and whatever else the contract holds. So no
# later cancel of it can pay anything but zero.
law cancel_empties_the_entry:
  for +es: +List<E.Entry>
  for +want: Nat
  for +from: Nat
  for +now: Nat
  {holds_after(E.Entries.cancel(es, want, from, now), want) == 0n : Nat}

# LAW 13. A call that fails changes nothing. Take any vault, any account
# and any amount. A call that carries the amount to a contract and is
# refused there leaves every balance, every entry and the time exactly as
# they were: the amount comes back whole, once, to the account it came
# from. (When the balance does not cover the amount the call is not made,
# and nothing changes either.)
law failed_call_changes_nothing:
  for +bals: +List<Nat>
  for +entries: +List<E.Entry>
  for +now: Nat
  for +next: Nat
  for +from: Nat
  for +amount: Nat
  {E.keep(E.Vault{bals, entries, now, next}, E.step(E.Vault{bals, entries, now, next}, E.Failed{from, amount})) == E.Vault{bals, entries, now, next} : E.Vault}

# What a payout pays; other when nothing was paid.
def paid(+f: E.Found, +other: Nat) -> Nat:
  match f:
    case E.Gone{}:
      other
    case E.Paid{to, amount, entries}:
      amount

# LAWS 14 to 17. No more comes out of an entry than it holds. Whenever an
# unlock, a claim, a reclaim or a cancel pays, the amount is exactly what
# the entry asked for holds at that moment: not more and not less. (That
# the other entries are left alone is LAWS 18 to 21.)
law unlock_pays_what_the_entry_holds:
  for +es: +List<E.Entry>
  for +want: Nat
  for +key: Bool
  for +now: Nat
  {paid(E.Entries.unlock(es, want, key, now), holds(es, want)) == holds(es, want) : Nat}

law claim_pays_what_the_entry_holds:
  for +es: +List<E.Entry>
  for +want: Nat
  for +key: Bool
  for +to: Nat
  for +now: Nat
  {paid(E.Entries.claim(es, want, key, to, now), holds(es, want)) == holds(es, want) : Nat}

law reclaim_pays_what_the_entry_holds:
  for +es: +List<E.Entry>
  for +want: Nat
  for +from: Nat
  for +now: Nat
  {paid(E.Entries.reclaim(es, want, from, now), holds(es, want)) == holds(es, want) : Nat}

law cancel_pays_what_the_entry_holds:
  for +es: +List<E.Entry>
  for +want: Nat
  for +from: Nat
  for +now: Nat
  {paid(E.Entries.cancel(es, want, from, now), holds(es, want)) == holds(es, want) : Nat}

# The entries without the one asked for.
def without(+es: +List<E.Entry>, +want: Nat) -> +List<E.Entry>:
  match es:
    case Nil{}:
      Nil{}
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      Bool.pick(+List<E.Entry>, Nat.is_eq(id, want), t, Con{E.Entry{id, maker, payee, amount, expiry}, without(t, want)})

# The entries with the one asked for holding nothing.
def emptied(+es: +List<E.Entry>, +want: Nat) -> +List<E.Entry>:
  match es:
    case Nil{}:
      Nil{}
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      Bool.pick(+List<E.Entry>, Nat.is_eq(id, want), Con{E.Entry{id, maker, payee, 0n, expiry}, t}, Con{E.Entry{id, maker, payee, amount, expiry}, emptied(t, want)})

# The entries a payout leaves; other when nothing was paid.
def rest(+f: E.Found, +other: +List<E.Entry>) -> +List<E.Entry>:
  match f:
    case E.Gone{}:
      other
    case E.Paid{to, amount, entries}:
      entries

# LAWS 18 to 21. A payout touches its own entry and no other. After an
# unlock, a claim or a reclaim that pays, the contract holds exactly the
# entries it held before without the one asked for: every other entry is
# still there, in place, with its maker, payee, amount and expiry as they
# were. After a stake cancel that pays, it holds exactly the entries it held
# before with the one asked for emptied.
law unlock_leaves_the_others:
  for +es: +List<E.Entry>
  for +want: Nat
  for +key: Bool
  for +now: Nat
  {rest(E.Entries.unlock(es, want, key, now), without(es, want)) == without(es, want) : +List<E.Entry>}

law claim_leaves_the_others:
  for +es: +List<E.Entry>
  for +want: Nat
  for +key: Bool
  for +to: Nat
  for +now: Nat
  {rest(E.Entries.claim(es, want, key, to, now), without(es, want)) == without(es, want) : +List<E.Entry>}

law reclaim_leaves_the_others:
  for +es: +List<E.Entry>
  for +want: Nat
  for +from: Nat
  for +now: Nat
  {rest(E.Entries.reclaim(es, want, from, now), without(es, want)) == without(es, want) : +List<E.Entry>}

law cancel_leaves_the_others:
  for +es: +List<E.Entry>
  for +want: Nat
  for +from: Nat
  for +now: Nat
  {rest(E.Entries.cancel(es, want, from, now), emptied(es, want)) == emptied(es, want) : +List<E.Entry>}

# LAWS 22 and 23. Without the preimage, or without a valid signature, no
# unlock pays: whatever the entries, the time and the entry asked for.
law unlock_needs_the_preimage:
  for +es: +List<E.Entry>
  for +want: Nat
  for +now: Nat
  {E.Entries.unlock(es, want, False{}, now) == E.Gone{} : E.Found}

law claim_needs_the_signature:
  for +es: +List<E.Entry>
  for +want: Nat
  for +to: Nat
  for +now: Nat
  {E.Entries.claim(es, want, False{}, to, now) == E.Gone{} : E.Found}

# LAWS 24 and 25. The two refusals of LAW 11, each on its own, so that the
# end state of LAW 11 cannot hide a cancel that paid too early or to the
# wrong caller: a cancel before the expiry is refused, and so is a cancel
# after it by an account that did not make the stake.
law early_cancel_refused:
  {E.step(E.run([E.Create{0n, 0n, 6n, 120n}], start()), E.Cancel{0n, 0n}) == E.Refused{} : E.Out}

law cancel_by_another_refused:
  {E.step(E.run([E.Create{0n, 0n, 6n, 120n}, E.Tick{}, E.Tick{}], start()), E.Cancel{2n, 0n}) == E.Refused{} : E.Out}
