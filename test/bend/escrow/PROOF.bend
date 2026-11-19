import Base
import ../ledger/nat.bend as N
import ../ledger/ledger.bend as L
import ../ledger/PROOF.bend as LP
import ./escrow.bend as E
import ./LAWS.bend as Laws

# Proofs of the laws in LAWS.bend. The balance lemmas (credit_sum,
# debit_exact) are the ones of the ledger rule.

# LAW 1
# -----

# The value held after a payout plus the amount it paid, or old when
# nothing was paid.
def found_after(f: E.Found, old: Nat) -> Nat:
  match f:
    case E.Gone{}:
      old
    case E.Paid{to, amount, entries}:
      Nat.add(E.Entries.held(entries), amount)

def pick_exact(+hit: Bool, +here: E.Found, +rest: E.Found, +old: Nat, e1: {found_after(here, old) == old : Nat}, e2: {found_after(rest, old) == old : Nat}) -> {found_after(E.Entries.pick(hit, here, rest), old) == old : Nat}:
  match hit:
    case True{}:
      e1
    case False{}:
      e2

def skip_exact(+f: E.Found, +id: Nat, +maker: Nat, +payee: Nat, +amount: Nat, +expiry: Nat, +ht: Nat, e: {found_after(f, ht) == ht : Nat}) -> {found_after(E.Entries.skip(E.Entry{id, maker, payee, amount, expiry}, f), Nat.add(ht, amount)) == Nat.add(ht, amount) : Nat}:
  match f:
    case E.Gone{}:
      {==}
    case E.Paid{to, paid, es}:
      %e : {Nat.add(Nat.add(E.Entries.held(es), amount), paid) == Nat.add(_, amount) : Nat}
      N.add_swap(E.Entries.held(es), amount, paid)

def here_exact(+ok: Bool, +to: Nat, +amount: Nat, +t: +List<E.Entry>) -> {found_after(E.Entries.here(ok, to, amount, t), Nat.add(E.Entries.held(t), amount)) == Nat.add(E.Entries.held(t), amount) : Nat}:
  match ok:
    case False{}:
      {==}
    case True{}:
      {==}

# An unlock that pays takes exactly the paid amount out of the entries.
law unlock_exact:
  for +es: +List<E.Entry>
  for +want: Nat
  for +key: Bool
  for +now: Nat
  {found_after(E.Entries.unlock(es, want, key, now), E.Entries.held(es)) == E.Entries.held(es) : Nat}

def unlock_exact(es, want, key, now):
  match es:
    case Nil{}:
      {==}
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      pick_exact(Nat.is_eq(id, want), E.Entries.here(E.Entries.may_unlock(key, now, expiry), payee, amount, t), E.Entries.skip(E.Entry{id, maker, payee, amount, expiry}, E.Entries.unlock(t, want, key, now)), Nat.add(E.Entries.held(t), amount), here_exact(E.Entries.may_unlock(key, now, expiry), payee, amount, t), skip_exact(E.Entries.unlock(t, want, key, now), id, maker, payee, amount, expiry, E.Entries.held(t), unlock_exact(t, want, key, now)))

# The same for a reclaim.
law reclaim_exact:
  for +es: +List<E.Entry>
  for +want: Nat
  for +from: Nat
  for +now: Nat
  {found_after(E.Entries.reclaim(es, want, from, now), E.Entries.held(es)) == E.Entries.held(es) : Nat}

def reclaim_exact(es, want, from, now):
  match es:
    case Nil{}:
      {==}
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      pick_exact(Nat.is_eq(id, want), E.Entries.here(E.Entries.may_reclaim(from, maker, now, expiry), maker, amount, t), E.Entries.skip(E.Entry{id, maker, payee, amount, expiry}, E.Entries.reclaim(t, want, from, now)), Nat.add(E.Entries.held(t), amount), here_exact(E.Entries.may_reclaim(from, maker, now, expiry), maker, amount, t), skip_exact(E.Entries.reclaim(t, want, from, now), id, maker, payee, amount, expiry, E.Entries.held(t), reclaim_exact(t, want, from, now)))

# The same for a claim.
law claim_exact:
  for +es: +List<E.Entry>
  for +want: Nat
  for +key: Bool
  for +to: Nat
  for +now: Nat
  {found_after(E.Entries.claim(es, want, key, to, now), E.Entries.held(es)) == E.Entries.held(es) : Nat}

def claim_exact(es, want, key, to, now):
  match es:
    case Nil{}:
      {==}
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      pick_exact(Nat.is_eq(id, want), E.Entries.here(E.Entries.may_unlock(key, now, expiry), to, amount, t), E.Entries.skip(E.Entry{id, maker, payee, amount, expiry}, E.Entries.claim(t, want, key, to, now)), Nat.add(E.Entries.held(t), amount), here_exact(E.Entries.may_unlock(key, now, expiry), to, amount, t), skip_exact(E.Entries.claim(t, want, key, to, now), id, maker, payee, amount, expiry, E.Entries.held(t), claim_exact(t, want, key, to, now)))

# A cancel pays what the entry holds and leaves it holding nothing.
def cancel_here(+ok: Bool, +id: Nat, +maker: Nat, +payee: Nat, +amount: Nat, +expiry: Nat, +t: +List<E.Entry>) -> {found_after(E.Entries.here(ok, maker, amount, Con{E.Entry{id, maker, payee, 0n, expiry}, t}), Nat.add(E.Entries.held(t), amount)) == Nat.add(E.Entries.held(t), amount) : Nat}:
  match ok:
    case False{}:
      {==}
    case True{}:
      %Equal.sym(Nat, Nat.add(E.Entries.held(t), 0n), E.Entries.held(t), N.add_zero(E.Entries.held(t))) : {Nat.add(_, amount) == Nat.add(E.Entries.held(t), amount) : Nat}
      {==}

law cancel_exact:
  for +es: +List<E.Entry>
  for +want: Nat
  for +from: Nat
  for +now: Nat
  {found_after(E.Entries.cancel(es, want, from, now), E.Entries.held(es)) == E.Entries.held(es) : Nat}

def cancel_exact(es, want, from, now):
  match es:
    case Nil{}:
      {==}
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      pick_exact(Nat.is_eq(id, want), E.Entries.here(E.Entries.may_reclaim(from, maker, now, expiry), maker, amount, Con{E.Entry{id, maker, payee, 0n, expiry}, t}), E.Entries.skip(E.Entry{id, maker, payee, amount, expiry}, E.Entries.cancel(t, want, from, now)), Nat.add(E.Entries.held(t), amount), cancel_here(E.Entries.may_reclaim(from, maker, now, expiry), id, maker, payee, amount, expiry, t), skip_exact(E.Entries.cancel(t, want, from, now), id, maker, payee, amount, expiry, E.Entries.held(t), cancel_exact(t, want, from, now)))

# Refunds
# -------

# The balances after a debit and a credit of the same amount to the same
# account, or old when the debit was refused.
def restore(+m: Maybe<&2, +List<Nat>>, +a: Nat, +amt: Nat, +old: +List<Nat>) -> +List<Nat>:
  match m:
    case None{}:
      old
    case Some{bs}:
      L.Bal.credit(a, bs, amt)

def restore_head(+ok: Bool, +h: Nat, +amt: Nat, +t: +List<Nat>, e: {Nat.is_le(amt, h) == ok : Bool}) -> {restore(L.Bal.debit.head(L.Bal.take.go(ok, h, amt), t), 0n, amt, Con{h, t}) == Con{h, t} : +List<Nat>}:
  match ok:
    case False{}:
      {==}
    case True{}:
      %Equal.sym(Nat, Nat.add(Nat.sub(h, amt), amt), h, LP.sub_add(h, amt, e)) : {Con{_, t} == Con{h, t} : +List<Nat>}
      {==}

def restore_tail(+m: Maybe<&2, +List<Nat>>, +p: Nat, +h: Nat, +amt: Nat, +t: +List<Nat>, ih: {restore(m, p, amt, t) == t : +List<Nat>}) -> {restore(L.Bal.debit.tail(h, m), 1n+p, amt, Con{h, t}) == Con{h, t} : +List<Nat>}:
  match m:
    case None{}:
      {==}
    case Some{bs}:
      %Equal.sym(+List<Nat>, L.Bal.credit(p, bs, amt), t, ih) : {Con{h, _} == Con{h, t} : +List<Nat>}
      {==}

# Taking an amount from an account and giving it back leaves every
# balance as it was.
law refund_restores:
  for +a: Nat
  for +bs: +List<Nat>
  for +amt: Nat
  {restore(L.Bal.debit(a, bs, amt), a, amt, bs) == bs : +List<Nat>}

def refund_restores(a, bs, amt):
  match a bs:
    case 0n Nil{}:
      {==}
    case 0n Con{h, t}:
      restore_head(Nat.is_le(amt, h), h, amt, t, {==})
    case 1n+p Nil{}:
      {==}
    case 1n+p Con{h, t}:
      restore_tail(L.Bal.debit(p, t, amt), p, h, amt, t, refund_restores(p, t, amt))

def fail_same(+m: Maybe<&2, +List<Nat>>, +bals: +List<Nat>, +entries: +List<E.Entry>, +now: Nat, +next: Nat, +from: Nat, +amount: Nat, e: {restore(m, from, amount, bals) == bals : +List<Nat>}) -> {E.keep(E.Vault{bals, entries, now, next}, E.Step.fail(m, entries, now, next, from, amount)) == E.Vault{bals, entries, now, next} : E.Vault}:
  match m:
    case None{}:
      {==}
    case Some{bs}:
      %Equal.sym(+List<Nat>, L.Bal.credit(from, bs, amount), bals, e) : {E.Vault{_, entries, now, next} == E.Vault{bals, entries, now, next} : E.Vault}
      {==}

# LAW 13.
def Laws.failed_call_changes_nothing(bals, entries, now, next, from, amount):
  fail_same(L.Bal.debit(from, bals, amount), bals, entries, now, next, from, amount, refund_restores(from, bals, amount))

# The worth after a call, or old when it was refused.
def worth_of(o: E.Out, old: Nat) -> Nat:
  match o:
    case E.Refused{}:
      old
    case E.Accepted{vault}:
      E.worth(vault)

def fail_exact(+m: Maybe<&2, +List<Nat>>, +bals: +List<Nat>, +entries: +List<E.Entry>, +now: Nat, +next: Nat, +from: Nat, +amount: Nat, e: {restore(m, from, amount, bals) == bals : +List<Nat>}) -> {worth_of(E.Step.fail(m, entries, now, next, from, amount), Nat.add(L.Bal.sum(bals), E.Entries.held(entries))) == Nat.add(L.Bal.sum(bals), E.Entries.held(entries)) : Nat}:
  match m:
    case None{}:
      {==}
    case Some{bs}:
      %Equal.sym(+List<Nat>, L.Bal.credit(from, bs, amount), bals, e) : {Nat.add(L.Bal.sum(_), E.Entries.held(entries)) == Nat.add(L.Bal.sum(bals), E.Entries.held(entries)) : Nat}
      {==}

def pay_exact(+f: E.Found, +bals: +List<Nat>, +now: Nat, +next: Nat, +h: Nat, e: {found_after(f, h) == h : Nat}) -> {worth_of(E.Step.pay(f, bals, now, next), Nat.add(L.Bal.sum(bals), h)) == Nat.add(L.Bal.sum(bals), h) : Nat}:
  match f:
    case E.Gone{}:
      {==}
    case E.Paid{to, amount, es}:
      %e : {Nat.add(L.Bal.sum(L.Bal.credit(to, bals, amount)), E.Entries.held(es)) == Nat.add(L.Bal.sum(bals), _) : Nat}
      %Equal.sym(Nat, L.Bal.sum(L.Bal.credit(to, bals, amount)), Nat.add(L.Bal.sum(bals), amount), LP.credit_sum(to, bals, amount)) : {Nat.add(_, E.Entries.held(es)) == Nat.add(L.Bal.sum(bals), Nat.add(E.Entries.held(es), amount)) : Nat}
      %N.add_swap(L.Bal.sum(bals), E.Entries.held(es), amount) : {_ == Nat.add(L.Bal.sum(bals), Nat.add(E.Entries.held(es), amount)) : Nat}
      N.add_assoc(L.Bal.sum(bals), E.Entries.held(es), amount)

def create_pay(+m: Maybe<&2, +List<Nat>>, +entries: +List<E.Entry>, +now: Nat, +next: Nat, +from: Nat, +payee: Nat, +amount: Nat, +expiry: Nat, +sb: Nat, e: {LP.sum_after(m, amount, sb) == sb : Nat}) -> {worth_of(E.Step.create.pay(m, entries, now, next, from, payee, amount, expiry), Nat.add(sb, E.Entries.held(entries))) == Nat.add(sb, E.Entries.held(entries)) : Nat}:
  match m:
    case None{}:
      {==}
    case Some{bals}:
      %e : {Nat.add(L.Bal.sum(bals), Nat.add(E.Entries.held(entries), amount)) == Nat.add(_, E.Entries.held(entries)) : Nat}
      %N.add_swap(L.Bal.sum(bals), E.Entries.held(entries), amount) : {Nat.add(L.Bal.sum(bals), Nat.add(E.Entries.held(entries), amount)) == _ : Nat}
      Equal.sym(Nat, Nat.add(Nat.add(L.Bal.sum(bals), E.Entries.held(entries)), amount), Nat.add(L.Bal.sum(bals), Nat.add(E.Entries.held(entries), amount)), N.add_assoc(L.Bal.sum(bals), E.Entries.held(entries), amount))

def create(+live: Bool, +bals: +List<Nat>, +entries: +List<E.Entry>, +now: Nat, +next: Nat, +from: Nat, +payee: Nat, +amount: Nat, +expiry: Nat) -> {worth_of(E.Step.create(live, bals, entries, now, next, from, payee, amount, expiry), Nat.add(L.Bal.sum(bals), E.Entries.held(entries))) == Nat.add(L.Bal.sum(bals), E.Entries.held(entries)) : Nat}:
  match live:
    case False{}:
      {==}
    case True{}:
      create_pay(L.Bal.debit(from, bals, amount), entries, now, next, from, payee, amount, expiry, L.Bal.sum(bals), LP.debit_exact(from, bals, amount))

# One call, accepted or refused, leaves the worth as it was.
law step_exact:
  for +v: E.Vault
  for +c: E.Call
  {worth_of(E.step(v, c), E.worth(v)) == E.worth(v) : Nat}

def step_exact(v, c):
  match v c:
    case E.Vault{bals, entries, now, next} E.Create{from, payee, amount, expiry}:
      create(Bool.and(Nat.is_gt(amount, 0n), Nat.is_lt(now, expiry)), bals, entries, now, next, from, payee, amount, expiry)
    case E.Vault{bals, entries, now, next} E.Unlock{id, key}:
      pay_exact(E.Entries.unlock(entries, id, key, now), bals, now, next, E.Entries.held(entries), unlock_exact(entries, id, key, now))
    case E.Vault{bals, entries, now, next} E.Claim{id, key, to}:
      pay_exact(E.Entries.claim(entries, id, key, to, now), bals, now, next, E.Entries.held(entries), claim_exact(entries, id, key, to, now))
    case E.Vault{bals, entries, now, next} E.Reclaim{from, id}:
      pay_exact(E.Entries.reclaim(entries, id, from, now), bals, now, next, E.Entries.held(entries), reclaim_exact(entries, id, from, now))
    case E.Vault{bals, entries, now, next} E.Cancel{from, id}:
      pay_exact(E.Entries.cancel(entries, id, from, now), bals, now, next, E.Entries.held(entries), cancel_exact(entries, id, from, now))
    case E.Vault{bals, entries, now, next} E.Failed{from, amount}:
      fail_exact(L.Bal.debit(from, bals, amount), bals, entries, now, next, from, amount, refund_restores(from, bals, amount))
    case E.Vault{bals, entries, now, next} E.Tick{}:
      {==}

law keep_worth:
  for +v: E.Vault
  for +o: E.Out
  {E.worth(E.keep(v, o)) == worth_of(o, E.worth(v)) : Nat}

def keep_worth(v, o):
  match o:
    case E.Refused{}:
      {==}
    case E.Accepted{vault}:
      {==}

law apply_exact:
  for +v: E.Vault
  for +c: E.Call
  {E.worth(E.apply(v, c)) == E.worth(v) : Nat}

def apply_exact(v, c):
  Equal.trans(Nat, E.worth(E.apply(v, c)), worth_of(E.step(v, c), E.worth(v)), E.worth(v), keep_worth(v, E.step(v, c)), step_exact(v, c))

def Laws.vault_keeps_value(cs, v):
  match cs:
    case Nil{}:
      {==}
    case Con{c, t}:
      Equal.trans(Nat, E.worth(E.run(t, E.apply(v, c))), E.worth(E.apply(v, c)), E.worth(v), Laws.vault_keeps_value(t, E.apply(v, c)), apply_exact(v, c))

# LAWS 2 and 3
# ------------

def pick_left(+hit: Bool, +here: E.Found, +rest: E.Found, +n: Nat, e1: {Laws.left(here, n) == n : Nat}, e2: {Laws.left(rest, n) == n : Nat}) -> {Laws.left(E.Entries.pick(hit, here, rest), n) == n : Nat}:
  match hit:
    case True{}:
      e1
    case False{}:
      e2

def skip_left(+f: E.Found, +x: E.Entry, +n: Nat, e: {Laws.left(f, n) == n : Nat}) -> {Laws.left(E.Entries.skip(x, f), 1n+n) == 1n+n : Nat}:
  match f:
    case E.Gone{}:
      {==}
    case E.Paid{to, paid, es}:
      %e : {2n+E.Entries.count(es) == 1n+_ : Nat}
      {==}

def here_left(+ok: Bool, +to: Nat, +amount: Nat, +t: +List<E.Entry>) -> {Laws.left(E.Entries.here(ok, to, amount, t), 1n+E.Entries.count(t)) == 1n+E.Entries.count(t) : Nat}:
  match ok:
    case False{}:
      {==}
    case True{}:
      {==}

def Laws.unlock_takes_the_entry(es, want, key, now):
  match es:
    case Nil{}:
      {==}
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      pick_left(Nat.is_eq(id, want), E.Entries.here(E.Entries.may_unlock(key, now, expiry), payee, amount, t), E.Entries.skip(E.Entry{id, maker, payee, amount, expiry}, E.Entries.unlock(t, want, key, now)), 1n+E.Entries.count(t), here_left(E.Entries.may_unlock(key, now, expiry), payee, amount, t), skip_left(E.Entries.unlock(t, want, key, now), E.Entry{id, maker, payee, amount, expiry}, E.Entries.count(t), Laws.unlock_takes_the_entry(t, want, key, now)))

def Laws.reclaim_takes_the_entry(es, want, from, now):
  match es:
    case Nil{}:
      {==}
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      pick_left(Nat.is_eq(id, want), E.Entries.here(E.Entries.may_reclaim(from, maker, now, expiry), maker, amount, t), E.Entries.skip(E.Entry{id, maker, payee, amount, expiry}, E.Entries.reclaim(t, want, from, now)), 1n+E.Entries.count(t), here_left(E.Entries.may_reclaim(from, maker, now, expiry), maker, amount, t), skip_left(E.Entries.reclaim(t, want, from, now), E.Entry{id, maker, payee, amount, expiry}, E.Entries.count(t), Laws.reclaim_takes_the_entry(t, want, from, now)))

# LAW 4
# -----

# Whatever the comparison says, "less" and "not less" are not both true.
def windows(+c: Cmp) -> {Bool.and(Cmp.is_lt(c), Cmp.is_ge(c)) == False{} : Bool}:
  match c:
    case LT{}:
      {==}
    case EQ{}:
      {==}
    case GT{}:
      {==}

def Laws.windows_do_not_overlap(now, expiry):
  windows(Nat.cmp(now, expiry))

# LAWS 5 and 6: both sides compute.
def Laws.unlock_pays_once():
  {==}

def Laws.reclaim_pays_once():
  {==}

# LAWS 7 and 8: both sides compute.
def Laws.wrong_preimage_pays_nothing():
  {==}

def Laws.only_the_maker_reclaims():
  {==}

# LAWS 9 and 10
# -------------

def Laws.claim_takes_the_entry(es, want, key, to, now):
  match es:
    case Nil{}:
      {==}
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      pick_left(Nat.is_eq(id, want), E.Entries.here(E.Entries.may_unlock(key, now, expiry), to, amount, t), E.Entries.skip(E.Entry{id, maker, payee, amount, expiry}, E.Entries.claim(t, want, key, to, now)), 1n+E.Entries.count(t), here_left(E.Entries.may_unlock(key, now, expiry), to, amount, t), skip_left(E.Entries.claim(t, want, key, to, now), E.Entry{id, maker, payee, amount, expiry}, E.Entries.count(t), Laws.claim_takes_the_entry(t, want, key, to, now)))

def Laws.claim_pays_once():
  {==}

# LAWS 11 and 12
# --------------

def Laws.second_cancel_pays_zero():
  {==}

# The entry is the one asked for: after the cancel it is the first of the
# list, with the same number, holding zero.
def empties_here(+ok: Bool, +id: Nat, +maker: Nat, +payee: Nat, +amount: Nat, +expiry: Nat, +t: +List<E.Entry>, +want: Nat, hit: {Nat.is_eq(id, want) == True{} : Bool}) -> {Laws.holds_after(E.Entries.here(ok, maker, amount, Con{E.Entry{id, maker, payee, 0n, expiry}, t}), want) == 0n : Nat}:
  match ok:
    case False{}:
      {==}
    case True{}:
      %Equal.sym(Bool, Nat.is_eq(id, want), True{}, hit) : {Bool.pick(Nat, _, 0n, Laws.holds(t, want)) == 0n : Nat}
      {==}

# The entry is another one: it is passed over before and after.
def empties_skip(+f: E.Found, +id: Nat, +maker: Nat, +payee: Nat, +amount: Nat, +expiry: Nat, +want: Nat, miss: {Nat.is_eq(id, want) == False{} : Bool}, ih: {Laws.holds_after(f, want) == 0n : Nat}) -> {Laws.holds_after(E.Entries.skip(E.Entry{id, maker, payee, amount, expiry}, f), want) == 0n : Nat}:
  match f:
    case E.Gone{}:
      {==}
    case E.Paid{to, paid, es}:
      %Equal.sym(Bool, Nat.is_eq(id, want), False{}, miss) : {Bool.pick(Nat, _, amount, Laws.holds(es, want)) == 0n : Nat}
      ih

def empties_pick(+hit: Bool, +from: Nat, +now: Nat, +id: Nat, +maker: Nat, +payee: Nat, +amount: Nat, +expiry: Nat, +t: +List<E.Entry>, +want: Nat, e: {Nat.is_eq(id, want) == hit : Bool}, ih: {Laws.holds_after(E.Entries.cancel(t, want, from, now), want) == 0n : Nat}) -> {Laws.holds_after(E.Entries.pick(hit, E.Entries.here(E.Entries.may_reclaim(from, maker, now, expiry), maker, amount, Con{E.Entry{id, maker, payee, 0n, expiry}, t}), E.Entries.skip(E.Entry{id, maker, payee, amount, expiry}, E.Entries.cancel(t, want, from, now))), want) == 0n : Nat}:
  match hit:
    case True{}:
      empties_here(E.Entries.may_reclaim(from, maker, now, expiry), id, maker, payee, amount, expiry, t, want, e)
    case False{}:
      empties_skip(E.Entries.cancel(t, want, from, now), id, maker, payee, amount, expiry, want, e, ih)

def Laws.cancel_empties_the_entry(es, want, from, now):
  match es:
    case Nil{}:
      {==}
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      empties_pick(Nat.is_eq(id, want), from, now, id, maker, payee, amount, expiry, t, want, {==}, Laws.cancel_empties_the_entry(t, want, from, now))

# LAWS 14 to 17
# -------------

# The entry is the one asked for: it pays its own amount, or nothing.
def holds_here(+ok: Bool, +to: Nat, +amount: Nat, +left: +List<E.Entry>) -> {Laws.paid(E.Entries.here(ok, to, amount, left), amount) == amount : Nat}:
  match ok:
    case False{}:
      {==}
    case True{}:
      {==}

# The entry is another one: what is paid comes from further down the list.
def holds_skip(+f: E.Found, +x: E.Entry, +h: Nat, ih: {Laws.paid(f, h) == h : Nat}) -> {Laws.paid(E.Entries.skip(x, f), h) == h : Nat}:
  match f:
    case E.Gone{}:
      {==}
    case E.Paid{to, amount, es}:
      ih

# Both cases at once. here pays from this entry (amount), rest from the
# others (below is what the entry asked for holds among them).
def holds_pick(+hit: Bool, +here: E.Found, +rest: E.Found, +amount: Nat, +below: Nat, e1: {Laws.paid(here, amount) == amount : Nat}, e2: {Laws.paid(rest, below) == below : Nat}) -> {Laws.paid(E.Entries.pick(hit, here, rest), Bool.pick(Nat, hit, amount, below)) == Bool.pick(Nat, hit, amount, below) : Nat}:
  match hit:
    case True{}:
      e1
    case False{}:
      e2

def Laws.unlock_pays_what_the_entry_holds(es, want, key, now):
  match es:
    case Nil{}:
      {==}
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      holds_pick(Nat.is_eq(id, want), E.Entries.here(E.Entries.may_unlock(key, now, expiry), payee, amount, t), E.Entries.skip(E.Entry{id, maker, payee, amount, expiry}, E.Entries.unlock(t, want, key, now)), amount, Laws.holds(t, want), holds_here(E.Entries.may_unlock(key, now, expiry), payee, amount, t), holds_skip(E.Entries.unlock(t, want, key, now), E.Entry{id, maker, payee, amount, expiry}, Laws.holds(t, want), Laws.unlock_pays_what_the_entry_holds(t, want, key, now)))

def Laws.claim_pays_what_the_entry_holds(es, want, key, to, now):
  match es:
    case Nil{}:
      {==}
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      holds_pick(Nat.is_eq(id, want), E.Entries.here(E.Entries.may_unlock(key, now, expiry), to, amount, t), E.Entries.skip(E.Entry{id, maker, payee, amount, expiry}, E.Entries.claim(t, want, key, to, now)), amount, Laws.holds(t, want), holds_here(E.Entries.may_unlock(key, now, expiry), to, amount, t), holds_skip(E.Entries.claim(t, want, key, to, now), E.Entry{id, maker, payee, amount, expiry}, Laws.holds(t, want), Laws.claim_pays_what_the_entry_holds(t, want, key, to, now)))

def Laws.reclaim_pays_what_the_entry_holds(es, want, from, now):
  match es:
    case Nil{}:
      {==}
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      holds_pick(Nat.is_eq(id, want), E.Entries.here(E.Entries.may_reclaim(from, maker, now, expiry), maker, amount, t), E.Entries.skip(E.Entry{id, maker, payee, amount, expiry}, E.Entries.reclaim(t, want, from, now)), amount, Laws.holds(t, want), holds_here(E.Entries.may_reclaim(from, maker, now, expiry), maker, amount, t), holds_skip(E.Entries.reclaim(t, want, from, now), E.Entry{id, maker, payee, amount, expiry}, Laws.holds(t, want), Laws.reclaim_pays_what_the_entry_holds(t, want, from, now)))

def Laws.cancel_pays_what_the_entry_holds(es, want, from, now):
  match es:
    case Nil{}:
      {==}
    case Con{E.Entry{id, maker, payee, amount, expiry}, t}:
      holds_pick(Nat.is_eq(id, want), E.Entries.here(E.Entries.may_reclaim(from, maker, now, expiry), maker, amount, Con{E.Entry{id, maker, payee, 0n, expiry}, t}), E.Entries.skip(E.Entry{id, maker, payee, amount, expiry}, E.Entries.cancel(t, want, from, now)), amount, Laws.holds(t, want), holds_here(E.Entries.may_reclaim(from, maker, now, expiry), maker, amount, Con{E.Entry{id, maker, payee, 0n, expiry}, t}), holds_skip(E.Entries.cancel(t, want, from, now), E.Entry{id, maker, payee, amount, expiry}, Laws.holds(t, want), Laws.cancel_pays_what_the_entry_holds(t, want, from, now)))
