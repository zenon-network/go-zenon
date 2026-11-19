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
