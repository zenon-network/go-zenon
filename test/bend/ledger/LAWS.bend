import Base
import ./ledger.bend as L

# The rule: nothing can be spent twice on the go-zenon ledger.
#
# LAW 1. No value is created. Take any ledger and any sequence of the
# model's blocks: sends, receives and momentums, valid or not, in any order.
# After the model has applied the ones it accepts and dropped the others,
# the value held by the accounts plus the value in transit is what it was
# before.
#
# This is a statement about the model (L.run). That the node treats the
# same blocks the same way is what the comparison test checks, and one thing
# the node does is not in the model at all: a second block at a height whose
# block is still pending may replace it (TestReplacementSpendsOnce).
#
# A double spend, in any of its forms, would break this: a send above the
# balance, two sends that each fit the balance but not together, a send
# received twice, or a send received by two accounts would each leave more
# value in the ledger than it started with.
law no_value_created:
  for +bs: +List<L.Block>
  for +s: L.State
  {L.total(L.run(bs, s, True{})) == L.total(s) : Nat}

# LAW 2. A send above the balance is refused, whatever else is true of the
# ledger and of the block.
law overspend_refused:
  for +bals: +List<Nat>
  for +heights: +List<Nat>
  for +sends: +List<L.Send>
  for +next: Nat
  for +from: Nat
  for +height: Nat
  for +to: Nat
  for +amount: Nat
  for short: {Nat.is_lt(L.Bal.get(from, bals), amount) == True{} : Bool}
  {L.step(L.State{bals, heights, sends, next}, True{}, L.Snd{from, height, to, amount}) == L.Refused{} : L.Next}

# What comes of asking for the same receive a second time, straight after
# the first: refused if the first was refused, else the second answer.
def again(+g: L.Got, +acc: Nat, +want: Nat) -> L.Got:
  match g:
    case L.Miss{}:
      L.Miss{}
    case L.Hit{amount, sends}:
      L.Sends.recv(sends, True{}, acc, want)

# LAW 3. A send is received once. If an account's receive of a send is
# accepted, the same receive is refused on the ledger that results.
law receive_once:
  for +ss: +List<L.Send>
  for +acc: Nat
  for +want: Nat
  {again(L.Sends.recv(ss, True{}, acc, want), acc, want) == L.Miss{} : L.Got}

# The ledger of LAW 4: account 0 holds 10, accounts 1 and 2 hold nothing.
def start() -> L.State:
  L.State{[10n, 0n, 0n], [0n, 0n, 0n], [], 0n}

# Account 0 sends 5 to account 1; a momentum confirms it; account 1
# receives it; account 2 tries to receive it as well.
def two_receivers() -> +List<L.Block>:
  [L.Snd{0n, 1n, 1n, 5n}, L.Tick{}, L.Rcv{1n, 1n, 0n}, L.Rcv{2n, 1n, 0n}]

# LAW 4. The rule that only the account a send is addressed to may receive
# it is what LAW 1 rests on. With the rule (strict) the 10 stay 10; without
# it the same four blocks leave 15. The "already received" mark alone does
# not stop this, because each account keeps its own.
law recipient_rule_holds_the_total:
  {L.total(L.run(two_receivers(), start(), True{})) == 10n : Nat}

law without_recipient_rule_value_appears:
  {L.total(L.run(two_receivers(), start(), False{})) == 15n : Nat}

# Who is paid, and how much
# -------------------------
# LAW 1 counts one total. A ledger that paid a send to the wrong account
# would keep that total: that is theft, not creation, and LAW 1 does not see
# it. The laws below say where every unit goes.

# About the send asked for (the first with that number): whether it is
# addressed to acc, whether a momentum has confirmed it, and its amount.
# False and zero when there is no such send.
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

# The sends with acc's mark added to the one asked for, and nothing else
# changed.
def marked(+ss: +List<L.Send>, +want: Nat, +acc: Nat) -> +List<L.Send>:
  match ss:
    case Nil{}:
      Nil{}
    case Con{L.Send{id, to, amount, conf, got}, t}:
      Bool.pick(+List<L.Send>, Nat.is_eq(id, want), Con{L.Send{id, to, amount, True{}, Con{acc, got}}, t}, Con{L.Send{id, to, amount, conf, got}, marked(t, want, acc)})

# Readings of the answer to a block: whether it was refused, and the
# balances and the sends afterwards (other when it was refused).
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

# LAW 5. Only the addressee receives. For every ledger, account, height and
# send: the send is addressed to the account, or the receive is refused.
law receive_only_by_the_addressee:
  for +bals: +List<Nat>
  for +heights: +List<Nat>
  for +sends: +List<L.Send>
  for +next: Nat
  for +acc: Nat
  for +height: Nat
  for +id: Nat
  {Bool.or(addressed_to(sends, id, acc), refused(L.step(L.State{bals, heights, sends, next}, True{}, L.Rcv{acc, height, id}))) == True{} : Bool}

# LAW 6. Only a confirmed send is received.
law receive_only_when_confirmed:
  for +bals: +List<Nat>
  for +heights: +List<Nat>
  for +sends: +List<L.Send>
  for +next: Nat
  for +acc: Nat
  for +height: Nat
  for +id: Nat
  {Bool.or(confirmed(sends, id), refused(L.step(L.State{bals, heights, sends, next}, True{}, L.Rcv{acc, height, id}))) == True{} : Bool}

# LAW 7. A receive credits the receiver with the send's amount, exactly.
# Whenever a receive is accepted, the balances afterwards are the balances
# before with the amount of that send added to the receiver.
law receive_credits_the_amount_of_the_send:
  for +bals: +List<Nat>
  for +heights: +List<Nat>
  for +sends: +List<L.Send>
  for +next: Nat
  for +acc: Nat
  for +height: Nat
  for +id: Nat
  {bals_after(L.step(L.State{bals, heights, sends, next}, True{}, L.Rcv{acc, height, id}), L.Bal.credit(acc, bals, amount_of(sends, id))) == L.Bal.credit(acc, bals, amount_of(sends, id)) : +List<Nat>}

# LAW 8. A receive closes that send and touches no other. Whenever a
# receive is accepted, the sends afterwards are the sends before with the
# receiver's mark on the one asked for: every other send keeps its number,
# addressee, amount, confirmation and marks.
law receive_marks_only_that_send:
  for +bals: +List<Nat>
  for +heights: +List<Nat>
  for +sends: +List<L.Send>
  for +next: Nat
  for +acc: Nat
  for +height: Nat
  for +id: Nat
  {sends_after(L.step(L.State{bals, heights, sends, next}, True{}, L.Rcv{acc, height, id}), marked(sends, id, acc)) == marked(sends, id, acc) : +List<L.Send>}

# The balances after a debit, or other when it was refused.
def debited(+m: Maybe<&2, +List<Nat>>, +other: +List<Nat>) -> +List<Nat>:
  match m:
    case None{}:
      other
    case Some{bs}:
      bs

# LAW 9. A send debits the sender with its amount, exactly. Whenever a send
# is accepted, the balances afterwards are the balances before with the
# amount taken from the sender.
law send_debits_the_sender:
  for +bals: +List<Nat>
  for +heights: +List<Nat>
  for +sends: +List<L.Send>
  for +next: Nat
  for +from: Nat
  for +height: Nat
  for +to: Nat
  for +amount: Nat
  {bals_after(L.step(L.State{bals, heights, sends, next}, True{}, L.Snd{from, height, to, amount}), debited(L.Bal.debit(from, bals, amount), bals)) == debited(L.Bal.debit(from, bals, amount), bals) : +List<Nat>}

# LAW 10. A send is recorded as what it is. Whenever a send is accepted,
# the sends afterwards are the sends before plus one, addressed to the
# account the block names, with the amount the block names, unconfirmed and
# unreceived.
law send_is_recorded_as_sent:
  for +bals: +List<Nat>
  for +heights: +List<Nat>
  for +sends: +List<L.Send>
  for +next: Nat
  for +from: Nat
  for +height: Nat
  for +to: Nat
  for +amount: Nat
  {sends_after(L.step(L.State{bals, heights, sends, next}, True{}, L.Snd{from, height, to, amount}), Con{L.Send{next, to, amount, False{}, Nil{}}, sends}) == Con{L.Send{next, to, amount, False{}, Nil{}}, sends} : +List<L.Send>}

# LAW 11. A credit changes one balance. After a credit to account a, the
# balance of any account x is what it was, plus the amount when x is a.
law credit_changes_one_balance:
  for +x: Nat
  for +a: Nat
  for +bs: +List<Nat>
  for +amt: Nat
  {L.Bal.get(x, L.Bal.credit(a, bs, amt)) == Bool.pick(Nat, Nat.is_eq(x, a), Nat.add(L.Bal.get(x, bs), amt), L.Bal.get(x, bs)) : Nat}

# The balance of x after a debit, or other when the debit was refused.
def balance_after(+m: Maybe<&2, +List<Nat>>, +x: Nat, +other: Nat) -> Nat:
  match m:
    case None{}:
      other
    case Some{bs}:
      L.Bal.get(x, bs)

# LAW 12. A debit changes one balance. After a debit from account a that is
# accepted, the balance of any account x is what it was, less the amount
# when x is a.
law debit_changes_one_balance:
  for +x: Nat
  for +a: Nat
  for +bs: +List<Nat>
  for +amt: Nat
  {balance_after(L.Bal.debit(a, bs, amt), x, Bool.pick(Nat, Nat.is_eq(x, a), Nat.sub(L.Bal.get(x, bs), amt), L.Bal.get(x, bs))) == Bool.pick(Nat, Nat.is_eq(x, a), Nat.sub(L.Bal.get(x, bs), amt), L.Bal.get(x, bs)) : Nat}

# LAW 13. A momentum moves no balance.
law momentum_moves_no_balance:
  for +bals: +List<Nat>
  for +heights: +List<Nat>
  for +sends: +List<L.Send>
  for +next: Nat
  {L.step(L.State{bals, heights, sends, next}, True{}, L.Tick{}) == L.Accepted{L.State{bals, heights, L.Sends.confirm(sends), next}} : L.Next}
