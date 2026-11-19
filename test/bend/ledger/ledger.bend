import Base

# A model of the part of the go-zenon ledger that decides whether value can
# be spent twice. One token; accounts and sends are numbered.
#
#   node                                       model
#   ----                                       -----
#   vm.applyTransfer, enoughFunds              Bal.debit
#   verifier accountBlockVerifier.previous     the height check in step
#   verifier accountBlockVerifier.fromHash     Sends.recv
#   accountStore.MarkAsReceived / IsReceived   Send.got
#   a momentum confirming account blocks       Tick
#
# In the node the "already received" mark is kept in the store of the
# account that receives, so two accounts have two separate marks for one
# send. The model keeps that: Send.got lists the accounts that hold a mark.

# One send block: who it is addressed to, how much, whether a momentum has
# confirmed it, and which accounts have received it.
type Send is Data:
  Send{id: Nat, to: Nat, amount: Nat, conf: Bool, got: +List<Nat>}

# The answer to a receive: refused, or the amount and the sends afterwards.
type Got is Data:
  Miss{}
  Hit{amount: Nat, sends: +List<Send>}

# An account block, or a momentum.
type Block is Data:
  Snd{from: Nat, height: Nat, to: Nat, amount: Nat}
  Rcv{acc: Nat, height: Nat, id: Nat}
  Tick{}

# The ledger: a balance and a chain height per account, every send so far,
# and the number the next send gets.
type State is Data:
  State{bals: +List<Nat>, heights: +List<Nat>, sends: +List<Send>, next: Nat}

# Balances and heights
# --------------------
# A list indexed by account number; an account past the end has zero.

def Bal.sum(+bs: +List<Nat>) -> Nat:
  match bs:
    case Nil{}:
      0n
    case Con{h, t}:
      Nat.add(Bal.sum(t), h)

def Bal.get(+a: Nat, +bs: +List<Nat>) -> Nat:
  match a bs:
    case 0n Nil{}:
      0n
    case 0n Con{h, t}:
      h
    case 1n+p Nil{}:
      0n
    case 1n+p Con{h, t}:
      Bal.get(p, t)

def Bal.credit(+a: Nat, +bs: +List<Nat>, +amt: Nat) -> +List<Nat>:
  match a bs:
    case 0n Nil{}:
      Con{amt, Nil{}}
    case 0n Con{h, t}:
      Con{Nat.add(h, amt), t}
    case 1n+p Nil{}:
      Con{0n, Bal.credit(p, Nil{}, amt)}
    case 1n+p Con{h, t}:
      Con{h, Bal.credit(p, t, amt)}

def Bal.take.go(+ok: Bool, +h: Nat, +amt: Nat) -> Maybe<&2, Nat>:
  match ok:
    case False{}:
      None{}
    case True{}:
      Some{Nat.sub(h, amt)}

# h - amt, or None when amt is more than h: the node's enoughFunds check
# followed by its SubBalance.
def Bal.take(+h: Nat, +amt: Nat) -> Maybe<&2, Nat>:
  Bal.take.go(Nat.is_le(amt, h), h, amt)

def Bal.debit.head(+m: Maybe<&2, Nat>, +t: +List<Nat>) -> Maybe<&2, +List<Nat>>:
  match m:
    case None{}:
      None{}
    case Some{r}:
      Some{Con{r, t}}

def Bal.debit.tail(+h: Nat, +m: Maybe<&2, +List<Nat>>) -> Maybe<&2, +List<Nat>>:
  match m:
    case None{}:
      None{}
    case Some{t}:
      Some{Con{h, t}}

# The balances with amt taken from account a, or None when a has less.
def Bal.debit(+a: Nat, +bs: +List<Nat>, +amt: Nat) -> Maybe<&2, +List<Nat>>:
  match a bs:
    case 0n Nil{}:
      None{}
    case 0n Con{h, t}:
      Bal.debit.head(Bal.take(h, amt), t)
    case 1n+p Nil{}:
      None{}
    case 1n+p Con{h, t}:
      Bal.debit.tail(h, Bal.debit(p, t, amt))

# Sends
# -----

def Sends.mem(+x: Nat, +xs: +List<Nat>) -> Bool:
  match xs:
    case Nil{}:
      False{}
    case Con{h, t}:
      Bool.or(Nat.is_eq(x, h), Sends.mem(x, t))

def Sends.open.go(+m: Bool, +amount: Nat) -> Nat:
  match m:
    case True{}:
      0n
    case False{}:
      amount

# What a send still owes: its amount until the account it is addressed to
# has received it, nothing afterwards.
def Sends.open(+s: Send) -> Nat:
  match s:
    case Send{id, to, amount, conf, got}:
      Sends.open.go(Sends.mem(to, got), amount)

# The value in transit: sent, not yet received by its recipient.
def Sends.pending(+ss: +List<Send>) -> Nat:
  match ss:
    case Nil{}:
      0n
    case Con{s, t}:
      Nat.add(Sends.pending(t), Sends.open(s))

def Sends.confirm.one(+s: Send) -> Send:
  match s:
    case Send{id, to, amount, conf, got}:
      Send{id, to, amount, True{}, got}

# A momentum confirms every send made so far.
def Sends.confirm(+ss: +List<Send>) -> +List<Send>:
  match ss:
    case Nil{}:
      Nil{}
    case Con{s, t}:
      Con{Sends.confirm.one(s), Sends.confirm(t)}

# The three checks of the verifier's fromHash rule, in one Bool each:
# the send is confirmed, the receiver is the account it was sent to
# (skipped when strict is False), the receiver has not received it before.
def Sends.recv.c3(+seen: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<Send>) -> Got:
  match seen:
    case True{}:
      Miss{}
    case False{}:
      Hit{amount, Con{Send{id, to, amount, True{}, Con{acc, got}}, t}}

def Sends.recv.c2(+mine: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<Send>) -> Got:
  match mine:
    case False{}:
      Miss{}
    case True{}:
      Sends.recv.c3(Sends.mem(acc, got), acc, id, to, amount, got, t)

def Sends.recv.c1(+conf: Bool, +strict: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<Send>) -> Got:
  match conf:
    case False{}:
      Miss{}
    case True{}:
      Sends.recv.c2(Bool.or(Bool.not(strict), Nat.is_eq(to, acc)), acc, id, to, amount, got, t)

def Sends.recv.skip(+s: Send, +m: Got) -> Got:
  match m:
    case Miss{}:
      Miss{}
    case Hit{amount, t}:
      Hit{amount, Con{s, t}}

def Sends.recv.pick(+hit: Bool, +here: Got, +rest: Got) -> Got:
  match hit:
    case True{}:
      here
    case False{}:
      rest

# Account acc receives send number want: the amount and the sends with the
# mark added, or None when the receive is refused.
def Sends.recv(+ss: +List<Send>, +strict: Bool, +acc: Nat, +want: Nat) -> Got:
  match ss:
    case Nil{}:
      Miss{}
    case Con{Send{id, to, amount, conf, got}, t}:
      Sends.recv.pick(Nat.is_eq(id, want), Sends.recv.c1(conf, strict, acc, id, to, amount, got, t), Sends.recv.skip(Send{id, to, amount, conf, got}, Sends.recv(t, strict, acc, want)))

# Blocks
# ------
# The answer to a block: refused, or the ledger afterwards.

type Next is Data:
  Refused{}
  Accepted{state: State}

def Step.snd.pay(+m: Maybe<&2, +List<Nat>>, +heights: +List<Nat>, +sends: +List<Send>, +next: Nat, +from: Nat, +to: Nat, +amount: Nat) -> Next:
  match m:
    case None{}:
      Refused{}
    case Some{bals}:
      Accepted{State{bals, Bal.credit(from, heights, 1n), Con{Send{next, to, amount, False{}, Nil{}}, sends}, 1n+next}}

# A send: the height must be the next one of the account chain, and the
# balance must cover the amount.
def Step.snd(+fits: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<Send>, +next: Nat, +from: Nat, +to: Nat, +amount: Nat) -> Next:
  match fits:
    case False{}:
      Refused{}
    case True{}:
      Step.snd.pay(Bal.debit(from, bals, amount), heights, sends, next, from, to, amount)

def Step.rcv.take(+g: Got, +bals: +List<Nat>, +heights: +List<Nat>, +next: Nat, +acc: Nat) -> Next:
  match g:
    case Miss{}:
      Refused{}
    case Hit{amount, sends}:
      Accepted{State{Bal.credit(acc, bals, amount), Bal.credit(acc, heights, 1n), sends, next}}

# A receive: the height must be the next one, and the send must pass the
# three checks of Sends.recv.
def Step.rcv(+fits: Bool, +strict: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<Send>, +next: Nat, +acc: Nat, +id: Nat) -> Next:
  match fits:
    case False{}:
      Refused{}
    case True{}:
      Step.rcv.take(Sends.recv(sends, strict, acc, id), bals, heights, next, acc)

# One block against the ledger. strict is the node's rule that only the
# account a send is addressed to may receive it; every chain enforces it,
# and the laws are stated with strict = True.
def step(+s: State, +strict: Bool, +b: Block) -> Next:
  match s b:
    case State{bals, heights, sends, next} Snd{from, height, to, amount}:
      Step.snd(Nat.is_eq(height, 1n+Bal.get(from, heights)), bals, heights, sends, next, from, to, amount)
    case State{bals, heights, sends, next} Rcv{acc, height, id}:
      Step.rcv(Nat.is_eq(height, 1n+Bal.get(acc, heights)), strict, bals, heights, sends, next, acc, id)
    case State{bals, heights, sends, next} Tick{}:
      Accepted{State{bals, heights, Sends.confirm(sends), next}}

def keep(+s: State, +n: Next) -> State:
  match n:
    case Refused{}:
      s
    case Accepted{state}:
      state

# The node drops a refused block and carries on.
def apply(+s: State, +strict: Bool, +b: Block) -> State:
  keep(s, step(s, strict, b))

def run(+bs: +List<Block>, +s: State, +strict: Bool) -> State:
  match bs:
    case Nil{}:
      s
    case Con{b, t}:
      run(t, apply(s, strict, b), strict)

# All the value there is: what the accounts hold plus what is in transit.
def total(+s: State) -> Nat:
  match s:
    case State{bals, heights, sends, next}:
      Nat.add(Bal.sum(bals), Sends.pending(sends))
