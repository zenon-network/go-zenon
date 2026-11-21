import Base
import ./ledger.bend as L

# One witness per mutant of mutants.py. A witness is the statement of the
# law it names, computed at one fixed input: the same expression as in
# LAWS.bend, with the law's variables given values, and its answer compared
# with the answer the law states. On the model every witness is True. With
# its mutant put into the model, the witness must be False: that is what
# shows the named law is false with the fault, and not merely that a proof
# no longer fits the text. run.sh (step 3) checks both.
#
# This file reads the model only, not LAWS.bend or PROOF.bend: with a
# mutant in the model the proofs do not check, and the witness must still
# run. So the readings the laws use are written here again, word for word.
#
# Where a law reads "the result afterwards, or the expected one when the
# block was refused", its witness uses an input on which the model accepts
# the block, so that True is not the refusal speaking.

# Readings, as in LAWS.bend
# -------------------------

def again(+g: L.Got, +acc: Nat, +want: Nat) -> L.Got:
  match g:
    case L.Miss{}:
      L.Miss{}
    case L.Hit{amount, sends}:
      L.Sends.recv(sends, True{}, acc, want)

def addressed_to(+ss: +List<L.Send>, +want: Nat, +acc: Nat) -> Bool:
  match ss:
    case Nil{}:
      False{}
    case Con{L.Send{id, to, amount, conf, got}, t}:
      Bool.pick(Bool, Nat.is_eq(id, want), Nat.is_eq(to, acc), addressed_to(t, want, acc))

def confirmed(+ss: +List<L.Send>, +want: Nat) -> Bool:
  match ss:
    case Nil{}:
      False{}
    case Con{L.Send{id, to, amount, conf, got}, t}:
      Bool.pick(Bool, Nat.is_eq(id, want), conf, confirmed(t, want))

def amount_of(+ss: +List<L.Send>, +want: Nat) -> Nat:
  match ss:
    case Nil{}:
      0n
    case Con{L.Send{id, to, amount, conf, got}, t}:
      Bool.pick(Nat, Nat.is_eq(id, want), amount, amount_of(t, want))

def marked(+ss: +List<L.Send>, +want: Nat, +acc: Nat) -> +List<L.Send>:
  match ss:
    case Nil{}:
      Nil{}
    case Con{L.Send{id, to, amount, conf, got}, t}:
      Bool.pick(+List<L.Send>, Nat.is_eq(id, want), Con{L.Send{id, to, amount, True{}, Con{acc, got}}, t}, Con{L.Send{id, to, amount, conf, got}, marked(t, want, acc)})

def refused(+n: L.Next) -> Bool:
  match n:
    case L.Refused{}:
      True{}
    case L.Accepted{state}:
      False{}

def bals_after(+n: L.Next, +other: +List<Nat>) -> +List<Nat>:
  match n:
    case L.Refused{}:
      other
    case L.Accepted{L.State{bals, heights, sends, next}}:
      bals

def sends_after(+n: L.Next, +other: +List<L.Send>) -> +List<L.Send>:
  match n:
    case L.Refused{}:
      other
    case L.Accepted{L.State{bals, heights, sends, next}}:
      sends

def debited(+m: Maybe<&2, +List<Nat>>, +other: +List<Nat>) -> +List<Nat>:
  match m:
    case None{}:
      other
    case Some{bs}:
      bs

def balance_after(+m: Maybe<&2, +List<Nat>>, +x: Nat, +other: Nat) -> Nat:
  match m:
    case None{}:
      other
    case Some{bs}:
      L.Bal.get(x, bs)

def all_confirmed(+ss: +List<L.Send>) -> +List<L.Send>:
  match ss:
    case Nil{}:
      Nil{}
    case Con{L.Send{id, to, amount, conf, got}, t}:
      Con{L.Send{id, to, amount, True{}, got}, all_confirmed(t)}

def heights_after(+n: L.Next, +other: +List<Nat>) -> +List<Nat>:
  match n:
    case L.Refused{}:
      other
    case L.Accepted{L.State{bals, heights, sends, next}}:
      heights

def next_after(+n: L.Next, +other: Nat) -> Nat:
  match n:
    case L.Refused{}:
      other
    case L.Accepted{L.State{bals, heights, sends, next}}:
      next

# Equality, computed: the "==" of a law, as a Bool
# ------------------------------------------------

def same_bool(+a: Bool, +b: Bool) -> Bool:
  match a b:
    case True{} True{}:
      True{}
    case False{} False{}:
      True{}
    case True{} False{}:
      False{}
    case False{} True{}:
      False{}

def same(+xs: +List<Nat>, +ys: +List<Nat>) -> Bool:
  match xs ys:
    case Nil{} Nil{}:
      True{}
    case Nil{} Con{b, u}:
      False{}
    case Con{a, t} Nil{}:
      False{}
    case Con{a, t} Con{b, u}:
      Bool.and(Nat.is_eq(a, b), same(t, u))

def same_send(+a: L.Send, +b: L.Send) -> Bool:
  match a b:
    case L.Send{i, t, x, c, g} L.Send{j, u, y, d, h}:
      Bool.and(Nat.is_eq(i, j), Bool.and(Nat.is_eq(t, u), Bool.and(Nat.is_eq(x, y), Bool.and(same_bool(c, d), same(g, h)))))

def same_sends(+xs: +List<L.Send>, +ys: +List<L.Send>) -> Bool:
  match xs ys:
    case Nil{} Nil{}:
      True{}
    case Nil{} Con{b, u}:
      False{}
    case Con{a, t} Nil{}:
      False{}
    case Con{a, t} Con{b, u}:
      Bool.and(same_send(a, b), same_sends(t, u))

def same_state(+a: L.State, +b: L.State) -> Bool:
  match a b:
    case L.State{bs, hs, ss, n} L.State{cs, js, ts, m}:
      Bool.and(same(bs, cs), Bool.and(same(hs, js), Bool.and(same_sends(ss, ts), Nat.is_eq(n, m))))

def same_next(+a: L.Next, +b: L.Next) -> Bool:
  match a b:
    case L.Refused{} L.Refused{}:
      True{}
    case L.Refused{} L.Accepted{t}:
      False{}
    case L.Accepted{s} L.Refused{}:
      False{}
    case L.Accepted{s} L.Accepted{t}:
      same_state(s, t)

def missed(+g: L.Got) -> Bool:
  match g:
    case L.Miss{}:
      True{}
    case L.Hit{amount, sends}:
      False{}

# Inputs
# ------

# Balances and heights before anything has happened: account 0 holds 10.
def b0() -> +List<Nat>:
  [10n, 0n, 0n]

def h0() -> +List<Nat>:
  [0n, 0n, 0n]

# After account 0 has sent 5 to account 1 as send 0.
def b1() -> +List<Nat>:
  [5n, 0n, 0n]

def h1() -> +List<Nat>:
  [1n, 0n, 0n]

# That send, not yet confirmed, and confirmed.
def pending() -> +List<L.Send>:
  [L.Send{0n, 1n, 5n, False{}, Nil{}}]

def ready() -> +List<L.Send>:
  [L.Send{0n, 1n, 5n, True{}, Nil{}}]

# Send 1, to account 1 and confirmed, above send 0, to account 2 and not
# confirmed. (A ledger the model does not reach from a start; LAW 8 is about
# every ledger.)
def mixed() -> +List<L.Send>:
  [L.Send{1n, 1n, 5n, True{}, Nil{}}, L.Send{0n, 2n, 3n, False{}, Nil{}}]

# The send block of account 0, at height 1: 5 to account 1.
def snd() -> L.Next:
  L.step(L.State{b0(), h0(), Nil{}, 0n}, True{}, L.Snd{0n, 1n, 1n, 5n})

# The receive block of account 1, at height 1, of the confirmed send 0.
def rcv() -> L.Next:
  L.step(L.State{b1(), h1(), ready(), 1n}, True{}, L.Rcv{1n, 1n, 0n})

# Witnesses
# ---------

# LAW 2 at balance 10, amount 11: the send is refused.
def no_balance_check() -> Bool:
  same_next(L.step(L.State{b0(), h0(), Nil{}, 0n}, True{}, L.Snd{0n, 1n, 1n, 11n}), L.Refused{})

# LAW 12 at x = 0, a = 0, balances 10, 0, 0 and amount 5.
def no_debit() -> Bool:
  Nat.is_eq(balance_after(L.Bal.debit(0n, b0(), 5n), 0n, Bool.pick(Nat, Nat.is_eq(0n, 0n), Nat.sub(L.Bal.get(0n, b0()), 5n), L.Bal.get(0n, b0()))), Bool.pick(Nat, Nat.is_eq(0n, 0n), Nat.sub(L.Bal.get(0n, b0()), 5n), L.Bal.get(0n, b0())))

# LAW 3 at the confirmed send 0, account 1: the second receive misses.
def no_received_check() -> Bool:
  missed(again(L.Sends.recv(ready(), True{}, 1n, 0n), 1n, 0n))

def no_received_mark() -> Bool:
  missed(again(L.Sends.recv(ready(), True{}, 1n, 0n), 1n, 0n))

# LAW 5 at the confirmed send 0, which is to account 1, and account 2.
def no_recipient_rule() -> Bool:
  Bool.or(addressed_to(ready(), 0n, 2n), refused(L.step(L.State{b1(), h1(), ready(), 1n}, True{}, L.Rcv{2n, 1n, 0n})))

# LAW 7 at the receive of send 0 by account 1.
def double_credit() -> Bool:
  same(bals_after(rcv(), L.Bal.credit(1n, b1(), amount_of(ready(), 0n))), L.Bal.credit(1n, b1(), amount_of(ready(), 0n)))

def credit_wrong_account() -> Bool:
  same(bals_after(rcv(), L.Bal.credit(1n, b1(), amount_of(ready(), 0n))), L.Bal.credit(1n, b1(), amount_of(ready(), 0n)))

# LAW 9 where both accounts 0 and 1 hold 10 and account 0 sends 5 to
# account 1.
def both_rich() -> +List<Nat>:
  [10n, 10n, 0n]

def debit_wrong_account() -> Bool:
  same(bals_after(L.step(L.State{both_rich(), h0(), Nil{}, 0n}, True{}, L.Snd{0n, 1n, 1n, 5n}), debited(L.Bal.debit(0n, both_rich(), 5n), both_rich())), debited(L.Bal.debit(0n, both_rich(), 5n), both_rich()))

# LAW 10 at the send of 5 from account 0 to account 1, with next = 0.
def send_to_wrong_account() -> Bool:
  same_sends(sends_after(snd(), Con{L.Send{0n, 1n, 5n, False{}, Nil{}}, Nil{}}), Con{L.Send{0n, 1n, 5n, False{}, Nil{}}, Nil{}})

def send_not_recorded() -> Bool:
  same_sends(sends_after(snd(), Con{L.Send{0n, 1n, 5n, False{}, Nil{}}, Nil{}}), Con{L.Send{0n, 1n, 5n, False{}, Nil{}}, Nil{}})

# LAW 6 at send 0 before any momentum, and its addressee.
def receive_unconfirmed() -> Bool:
  Bool.or(confirmed(pending(), 0n), refused(L.step(L.State{b1(), h1(), pending(), 1n}, True{}, L.Rcv{1n, 1n, 0n})))

# LAW 8 at the receive of send 1 by account 1, with send 0 beside it.
def receive_confirms_others() -> Bool:
  same_sends(sends_after(L.step(L.State{b0(), h0(), mixed(), 2n}, True{}, L.Rcv{1n, 1n, 1n}), marked(mixed(), 1n, 1n)), marked(mixed(), 1n, 1n))

# LAW 13 at the ledger with send 0 pending.
def momentum_redirects() -> Bool:
  same_next(L.step(L.State{b1(), h1(), pending(), 1n}, True{}, L.Tick{}), L.Accepted{L.State{b1(), h1(), all_confirmed(pending()), 1n}})

# LAW 18 at the send, with next = 0.
def send_keeps_number() -> Bool:
  Nat.is_eq(next_after(snd(), 1n+0n), 1n+0n)

# LAW 14 at a send of height 5 on a chain of height 0.
def no_height_check() -> Bool:
  Bool.or(Nat.is_eq(5n, 1n+L.Bal.get(0n, h0())), refused(L.step(L.State{b0(), h0(), Nil{}, 0n}, True{}, L.Snd{0n, 5n, 1n, 1n})))

# LAW 16 at the send.
def height_not_raised() -> Bool:
  same(heights_after(snd(), L.Bal.credit(0n, h0(), 1n)), L.Bal.credit(0n, h0(), 1n))

def main() -> IO(Unit):
  do IO<Unit>:
    IO.print("no-balance-check " ++ Bool.show(no_balance_check()))
    IO.print("no-debit " ++ Bool.show(no_debit()))
    IO.print("no-received-check " ++ Bool.show(no_received_check()))
    IO.print("no-received-mark " ++ Bool.show(no_received_mark()))
    IO.print("no-recipient-rule " ++ Bool.show(no_recipient_rule()))
    IO.print("double-credit " ++ Bool.show(double_credit()))
    IO.print("credit-wrong-account " ++ Bool.show(credit_wrong_account()))
    IO.print("debit-wrong-account " ++ Bool.show(debit_wrong_account()))
    IO.print("send-to-wrong-account " ++ Bool.show(send_to_wrong_account()))
    IO.print("receive-unconfirmed " ++ Bool.show(receive_unconfirmed()))
    IO.print("receive-confirms-others " ++ Bool.show(receive_confirms_others()))
    IO.print("momentum-redirects " ++ Bool.show(momentum_redirects()))
    IO.print("send-keeps-number " ++ Bool.show(send_keeps_number()))
    IO.print("no-height-check " ++ Bool.show(no_height_check()))
    IO.print("height-not-raised " ++ Bool.show(height_not_raised()))
    IO.print("send-not-recorded " ++ Bool.show(send_not_recorded()))
