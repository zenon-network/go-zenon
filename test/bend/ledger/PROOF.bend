import Base
import ./nat.bend as N
import ./ledger.bend as L
import ./LAWS.bend as Laws

# Proofs of the laws in LAWS.bend. Each step function of the model answers
# "refused" or a new value; for each there is a measure below that reads the
# value of the answer, or the old value when it was refused, and a lemma
# that the measure equals the old value.

# Balances
# --------

law credit_sum:
  for +a: Nat
  for +bs: +List<Nat>
  for +amt: Nat
  {L.Bal.sum(L.Bal.credit(a, bs, amt)) == Nat.add(L.Bal.sum(bs), amt) : Nat}

def credit_sum(a, bs, amt):
  match a bs:
    case 0n Nil{}:
      {==}
    case 0n Con{h, t}:
      Equal.sym(Nat, Nat.add(Nat.add(L.Bal.sum(t), h), amt), Nat.add(L.Bal.sum(t), Nat.add(h, amt)), N.add_assoc(L.Bal.sum(t), h, amt))
    case 1n+p Nil{}:
      %Equal.sym(Nat, L.Bal.sum(L.Bal.credit(p, Nil{}, amt)), amt, credit_sum(p, Nil{}, amt)) : {Nat.add(_, 0n) == amt : Nat}
      N.add_zero(amt)
    case 1n+p Con{h, t}:
      %Equal.sym(Nat, L.Bal.sum(L.Bal.credit(p, t, amt)), Nat.add(L.Bal.sum(t), amt), credit_sum(p, t, amt)) : {Nat.add(_, h) == Nat.add(Nat.add(L.Bal.sum(t), h), amt) : Nat}
      N.add_swap(L.Bal.sum(t), amt, h)

# What Bal.take answers, put back: r + amt, or h itself when it refused.
def take_back(m: Maybe<&2, Nat>, amt: Nat, h: Nat) -> Nat:
  match m:
    case None{}:
      h
    case Some{r}:
      Nat.add(r, amt)

# (h - amt) + amt == h when amt <= h.
def sub_add(+h: Nat, +amt: Nat, e: {Nat.is_le(amt, h) == True{} : Bool}) -> {Nat.add(Nat.sub(h, amt), amt) == h : Nat}:
  match h amt:
    case 0n 0n:
      {==}
    case 0n 1n+a:
      Empty.absurd({Nat.add(Nat.sub(0n, 1n+a), 1n+a) == 0n : Nat}, N.false_is_not_true(e))
    case 1n+p 0n:
      N.add_zero(1n+p)
    case 1n+p 1n+a:
      %sub_add(p, a, e) : {Nat.add(Nat.sub(p, a), 1n+a) == 1n+_ : Nat}
      N.add_succ(Nat.sub(p, a), a)

def take_go(+ok: Bool, +h: Nat, +amt: Nat, e: {Nat.is_le(amt, h) == ok : Bool}) -> {take_back(L.Bal.take.go(ok, h, amt), amt, h) == h : Nat}:
  match ok:
    case False{}:
      {==}
    case True{}:
      sub_add(h, amt, e)

law take_exact:
  for +h: Nat
  for +amt: Nat
  {take_back(L.Bal.take(h, amt), amt, h) == h : Nat}

def take_exact(h, amt):
  take_go(Nat.is_le(amt, h), h, amt, {==})

# The sum of what Bal.debit answers plus amt, or old when it refused.
def sum_after(m: Maybe<&2, +List<Nat>>, amt: Nat, old: Nat) -> Nat:
  match m:
    case None{}:
      old
    case Some{bs}:
      Nat.add(L.Bal.sum(bs), amt)

def debit_head(+m: Maybe<&2, Nat>, +t: +List<Nat>, +amt: Nat, +h: Nat, e: {take_back(m, amt, h) == h : Nat}) -> {sum_after(L.Bal.debit.head(m, t), amt, Nat.add(L.Bal.sum(t), h)) == Nat.add(L.Bal.sum(t), h) : Nat}:
  match m:
    case None{}:
      {==}
    case Some{r}:
      %e : {Nat.add(Nat.add(L.Bal.sum(t), r), amt) == Nat.add(L.Bal.sum(t), _) : Nat}
      N.add_assoc(L.Bal.sum(t), r, amt)

def debit_tail(+m: Maybe<&2, +List<Nat>>, +h: Nat, +amt: Nat, +st: Nat, e: {sum_after(m, amt, st) == st : Nat}) -> {sum_after(L.Bal.debit.tail(h, m), amt, Nat.add(st, h)) == Nat.add(st, h) : Nat}:
  match m:
    case None{}:
      {==}
    case Some{t}:
      %e : {Nat.add(Nat.add(L.Bal.sum(t), h), amt) == Nat.add(_, h) : Nat}
      N.add_swap(L.Bal.sum(t), h, amt)

# A debit that is accepted takes exactly amt out of the balances.
law debit_exact:
  for +a: Nat
  for +bs: +List<Nat>
  for +amt: Nat
  {sum_after(L.Bal.debit(a, bs, amt), amt, L.Bal.sum(bs)) == L.Bal.sum(bs) : Nat}

def debit_exact(a, bs, amt):
  match a bs:
    case 0n Nil{}:
      {==}
    case 0n Con{h, t}:
      debit_head(L.Bal.take(h, amt), t, amt, h, take_exact(h, amt))
    case 1n+p Nil{}:
      {==}
    case 1n+p Con{h, t}:
      debit_tail(L.Bal.debit(p, t, amt), h, amt, L.Bal.sum(t), debit_exact(p, t, amt))

# Sends
# -----

law confirm_pending:
  for +ss: +List<L.Send>
  {L.Sends.pending(L.Sends.confirm(ss)) == L.Sends.pending(ss) : Nat}

def confirm_pending(ss):
  match ss:
    case Nil{}:
      {==}
    case Con{L.Send{id, to, amount, conf, got}, t}:
      %Equal.sym(Nat, L.Sends.pending(L.Sends.confirm(t)), L.Sends.pending(t), confirm_pending(t)) : {Nat.add(_, L.Sends.open.go(L.Sends.mem(to, got), amount)) == Nat.add(L.Sends.pending(t), L.Sends.open.go(L.Sends.mem(to, got), amount)) : Nat}
      {==}

# The value in transit after a receive plus the amount it paid, or old
# when it was refused.
def pend_after(g: L.Got, old: Nat) -> Nat:
  match g:
    case L.Miss{}:
      old
    case L.Hit{amount, sends}:
      Nat.add(L.Sends.pending(sends), amount)

# Third check passed: the receiver has no mark yet. Adding the mark closes
# the send, because the receiver is the account it is addressed to (eq).
def recv_c3(+seen: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<L.Send>, e: {L.Sends.mem(acc, got) == seen : Bool}, +eq: {to == acc : Nat}) -> {pend_after(L.Sends.recv.c3(seen, acc, id, to, amount, got, t), Nat.add(L.Sends.pending(t), L.Sends.open.go(L.Sends.mem(to, got), amount))) == Nat.add(L.Sends.pending(t), L.Sends.open.go(L.Sends.mem(to, got), amount)) : Nat}:
  match seen:
    case True{}:
      {==}
    case False{}:
      %eq : {Nat.add(Nat.add(L.Sends.pending(t), L.Sends.open.go(Bool.or(Nat.is_eq(to, _), L.Sends.mem(to, got)), amount)), amount) == Nat.add(L.Sends.pending(t), L.Sends.open.go(L.Sends.mem(to, got), amount)) : Nat}
      %Equal.sym(Bool, Nat.is_eq(to, to), True{}, N.is_eq_same(to)) : {Nat.add(Nat.add(L.Sends.pending(t), L.Sends.open.go(Bool.or(_, L.Sends.mem(to, got)), amount)), amount) == Nat.add(L.Sends.pending(t), L.Sends.open.go(L.Sends.mem(to, got), amount)) : Nat}
      %Equal.sym(Nat, to, acc, eq) : {Nat.add(Nat.add(L.Sends.pending(t), 0n), amount) == Nat.add(L.Sends.pending(t), L.Sends.open.go(L.Sends.mem(_, got), amount)) : Nat}
      %Equal.sym(Bool, L.Sends.mem(acc, got), False{}, e) : {Nat.add(Nat.add(L.Sends.pending(t), 0n), amount) == Nat.add(L.Sends.pending(t), L.Sends.open.go(_, amount)) : Nat}
      %Equal.sym(Nat, Nat.add(L.Sends.pending(t), 0n), L.Sends.pending(t), N.add_zero(L.Sends.pending(t))) : {Nat.add(_, amount) == Nat.add(L.Sends.pending(t), amount) : Nat}
      {==}

def recv_c2(+mine: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<L.Send>, e: {Nat.is_eq(to, acc) == mine : Bool}) -> {pend_after(L.Sends.recv.c2(mine, acc, id, to, amount, got, t), Nat.add(L.Sends.pending(t), L.Sends.open.go(L.Sends.mem(to, got), amount))) == Nat.add(L.Sends.pending(t), L.Sends.open.go(L.Sends.mem(to, got), amount)) : Nat}:
  match mine:
    case False{}:
      {==}
    case True{}:
      recv_c3(L.Sends.mem(acc, got), acc, id, to, amount, got, t, {==}, N.is_eq_sound(to, acc, e))

def recv_c1(+conf: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<L.Send>) -> {pend_after(L.Sends.recv.c1(conf, True{}, acc, id, to, amount, got, t), Nat.add(L.Sends.pending(t), L.Sends.open.go(L.Sends.mem(to, got), amount))) == Nat.add(L.Sends.pending(t), L.Sends.open.go(L.Sends.mem(to, got), amount)) : Nat}:
  match conf:
    case False{}:
      {==}
    case True{}:
      recv_c2(Nat.is_eq(to, acc), acc, id, to, amount, got, t, {==})

def recv_skip(+g: L.Got, +s: L.Send, +pt: Nat, e: {pend_after(g, pt) == pt : Nat}) -> {pend_after(L.Sends.recv.skip(s, g), Nat.add(pt, L.Sends.open(s))) == Nat.add(pt, L.Sends.open(s)) : Nat}:
  match g:
    case L.Miss{}:
      {==}
    case L.Hit{amount, t}:
      %e : {Nat.add(Nat.add(L.Sends.pending(t), L.Sends.open(s)), amount) == Nat.add(_, L.Sends.open(s)) : Nat}
      N.add_swap(L.Sends.pending(t), L.Sends.open(s), amount)

def recv_pick(+hit: Bool, +here: L.Got, +rest: L.Got, +old: Nat, e1: {pend_after(here, old) == old : Nat}, e2: {pend_after(rest, old) == old : Nat}) -> {pend_after(L.Sends.recv.pick(hit, here, rest), old) == old : Nat}:
  match hit:
    case True{}:
      e1
    case False{}:
      e2

# A receive that is accepted takes exactly its amount out of transit.
law recv_exact:
  for +ss: +List<L.Send>
  for +acc: Nat
  for +want: Nat
  {pend_after(L.Sends.recv(ss, True{}, acc, want), L.Sends.pending(ss)) == L.Sends.pending(ss) : Nat}

def recv_exact(ss, acc, want):
  match ss:
    case Nil{}:
      {==}
    case Con{L.Send{id, to, amount, conf, got}, t}:
      recv_pick(Nat.is_eq(id, want), L.Sends.recv.c1(conf, True{}, acc, id, to, amount, got, t), L.Sends.recv.skip(L.Send{id, to, amount, conf, got}, L.Sends.recv(t, True{}, acc, want)), Nat.add(L.Sends.pending(t), L.Sends.open.go(L.Sends.mem(to, got), amount)), recv_c1(conf, acc, id, to, amount, got, t), recv_skip(L.Sends.recv(t, True{}, acc, want), L.Send{id, to, amount, conf, got}, L.Sends.pending(t), recv_exact(t, acc, want)))

# Blocks
# ------

# The total after a block, or old when it was refused.
def total_of(n: L.Next, old: Nat) -> Nat:
  match n:
    case L.Refused{}:
      old
    case L.Accepted{state}:
      L.total(state)

def snd_pay(+m: Maybe<&2, +List<Nat>>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +from: Nat, +to: Nat, +amount: Nat, +sb: Nat, e: {sum_after(m, amount, sb) == sb : Nat}) -> {total_of(L.Step.snd.pay(m, heights, sends, next, from, to, amount), Nat.add(sb, L.Sends.pending(sends))) == Nat.add(sb, L.Sends.pending(sends)) : Nat}:
  match m:
    case None{}:
      {==}
    case Some{bals}:
      %e : {Nat.add(L.Bal.sum(bals), Nat.add(L.Sends.pending(sends), amount)) == Nat.add(_, L.Sends.pending(sends)) : Nat}
      %N.add_swap(L.Bal.sum(bals), L.Sends.pending(sends), amount) : {Nat.add(L.Bal.sum(bals), Nat.add(L.Sends.pending(sends), amount)) == _ : Nat}
      Equal.sym(Nat, Nat.add(Nat.add(L.Bal.sum(bals), L.Sends.pending(sends)), amount), Nat.add(L.Bal.sum(bals), Nat.add(L.Sends.pending(sends), amount)), N.add_assoc(L.Bal.sum(bals), L.Sends.pending(sends), amount))

def snd(+fits: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +from: Nat, +to: Nat, +amount: Nat) -> {total_of(L.Step.snd(fits, bals, heights, sends, next, from, to, amount), Nat.add(L.Bal.sum(bals), L.Sends.pending(sends))) == Nat.add(L.Bal.sum(bals), L.Sends.pending(sends)) : Nat}:
  match fits:
    case False{}:
      {==}
    case True{}:
      snd_pay(L.Bal.debit(from, bals, amount), heights, sends, next, from, to, amount, L.Bal.sum(bals), debit_exact(from, bals, amount))

def rcv_take(+g: L.Got, +bals: +List<Nat>, +heights: +List<Nat>, +next: Nat, +acc: Nat, +ps: Nat, e: {pend_after(g, ps) == ps : Nat}) -> {total_of(L.Step.rcv.take(g, bals, heights, next, acc), Nat.add(L.Bal.sum(bals), ps)) == Nat.add(L.Bal.sum(bals), ps) : Nat}:
  match g:
    case L.Miss{}:
      {==}
    case L.Hit{amount, sends}:
      %e : {Nat.add(L.Bal.sum(L.Bal.credit(acc, bals, amount)), L.Sends.pending(sends)) == Nat.add(L.Bal.sum(bals), _) : Nat}
      %Equal.sym(Nat, L.Bal.sum(L.Bal.credit(acc, bals, amount)), Nat.add(L.Bal.sum(bals), amount), credit_sum(acc, bals, amount)) : {Nat.add(_, L.Sends.pending(sends)) == Nat.add(L.Bal.sum(bals), Nat.add(L.Sends.pending(sends), amount)) : Nat}
      %N.add_swap(L.Bal.sum(bals), L.Sends.pending(sends), amount) : {_ == Nat.add(L.Bal.sum(bals), Nat.add(L.Sends.pending(sends), amount)) : Nat}
      N.add_assoc(L.Bal.sum(bals), L.Sends.pending(sends), amount)

def rcv(+fits: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +acc: Nat, +id: Nat) -> {total_of(L.Step.rcv(fits, True{}, bals, heights, sends, next, acc, id), Nat.add(L.Bal.sum(bals), L.Sends.pending(sends))) == Nat.add(L.Bal.sum(bals), L.Sends.pending(sends)) : Nat}:
  match fits:
    case False{}:
      {==}
    case True{}:
      rcv_take(L.Sends.recv(sends, True{}, acc, id), bals, heights, next, acc, L.Sends.pending(sends), recv_exact(sends, acc, id))

# One block, accepted or refused, leaves the total as it was.
law step_exact:
  for +s: L.State
  for +b: L.Block
  {total_of(L.step(s, True{}, b), L.total(s)) == L.total(s) : Nat}

def step_exact(s, b):
  match s b:
    case L.State{bals, heights, sends, next} L.Snd{from, height, to, amount}:
      snd(Nat.is_eq(height, 1n+L.Bal.get(from, heights)), bals, heights, sends, next, from, to, amount)
    case L.State{bals, heights, sends, next} L.Rcv{acc, height, id}:
      rcv(Nat.is_eq(height, 1n+L.Bal.get(acc, heights)), bals, heights, sends, next, acc, id)
    case L.State{bals, heights, sends, next} L.Tick{}:
      %Equal.sym(Nat, L.Sends.pending(L.Sends.confirm(sends)), L.Sends.pending(sends), confirm_pending(sends)) : {Nat.add(L.Bal.sum(bals), _) == Nat.add(L.Bal.sum(bals), L.Sends.pending(sends)) : Nat}
      {==}

law keep_total:
  for +s: L.State
  for +n: L.Next
  {L.total(L.keep(s, n)) == total_of(n, L.total(s)) : Nat}

def keep_total(s, n):
  match n:
    case L.Refused{}:
      {==}
    case L.Accepted{state}:
      {==}

law apply_exact:
  for +s: L.State
  for +b: L.Block
  {L.total(L.apply(s, True{}, b)) == L.total(s) : Nat}

def apply_exact(s, b):
  Equal.trans(Nat, L.total(L.apply(s, True{}, b)), total_of(L.step(s, True{}, b), L.total(s)), L.total(s), keep_total(s, L.step(s, True{}, b)), step_exact(s, b))

# LAW 1, by induction on the blocks.
def Laws.no_value_created(bs, s):
  match bs:
    case Nil{}:
      {==}
    case Con{b, t}:
      Equal.trans(Nat, L.total(L.run(t, L.apply(s, True{}, b), True{})), L.total(L.apply(s, True{}, b)), L.total(s), Laws.no_value_created(t, L.apply(s, True{}, b)), apply_exact(s, b))

# LAW 4: both sides compute.
def Laws.recipient_rule_holds_the_total():
  {==}

def Laws.without_recipient_rule_value_appears():
  {==}

# LAW 2
# -----

# h < amt means amt <= h is false.
def lt_not_le(+h: Nat, +amt: Nat, e: {Nat.is_lt(h, amt) == True{} : Bool}) -> {Nat.is_le(amt, h) == False{} : Bool}:
  match h amt:
    case 0n 0n:
      Empty.absurd({Nat.is_le(0n, 0n) == False{} : Bool}, N.false_is_not_true(e))
    case 0n 1n+a:
      {==}
    case 1n+p 0n:
      Empty.absurd({Nat.is_le(0n, 1n+p) == False{} : Bool}, N.false_is_not_true(e))
    case 1n+p 1n+a:
      lt_not_le(p, a, e)

def take_none(+h: Nat, +amt: Nat, e: {Nat.is_lt(h, amt) == True{} : Bool}) -> {L.Bal.take(h, amt) == None{} : Maybe<&2, Nat>}:
  %Equal.sym(Bool, Nat.is_le(amt, h), False{}, lt_not_le(h, amt, e)) : {L.Bal.take.go(_, h, amt) == None{} : Maybe<&2, Nat>}
  {==}

def debit_none(+a: Nat, +bs: +List<Nat>, +amt: Nat, e: {Nat.is_lt(L.Bal.get(a, bs), amt) == True{} : Bool}) -> {L.Bal.debit(a, bs, amt) == None{} : Maybe<&2, +List<Nat>>}:
  match a bs:
    case 0n Nil{}:
      {==}
    case 0n Con{h, t}:
      %Equal.sym(Maybe<&2, Nat>, L.Bal.take(h, amt), None{}, take_none(h, amt, e)) : {L.Bal.debit.head(_, t) == None{} : Maybe<&2, +List<Nat>>}
      {==}
    case 1n+p Nil{}:
      {==}
    case 1n+p Con{h, t}:
      %Equal.sym(Maybe<&2, +List<Nat>>, L.Bal.debit(p, t, amt), None{}, debit_none(p, t, amt, e)) : {L.Bal.debit.tail(h, _) == None{} : Maybe<&2, +List<Nat>>}
      {==}

def snd_refused(+fits: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +from: Nat, +to: Nat, +amount: Nat, e: {Nat.is_lt(L.Bal.get(from, bals), amount) == True{} : Bool}) -> {L.Step.snd(fits, bals, heights, sends, next, from, to, amount) == L.Refused{} : L.Next}:
  match fits:
    case False{}:
      {==}
    case True{}:
      %Equal.sym(Maybe<&2, +List<Nat>>, L.Bal.debit(from, bals, amount), None{}, debit_none(from, bals, amount, e)) : {L.Step.snd.pay(_, heights, sends, next, from, to, amount) == L.Refused{} : L.Next}
      {==}

def Laws.overspend_refused(bals, heights, sends, next, from, height, to, amount, short):
  snd_refused(Nat.is_eq(height, 1n+L.Bal.get(from, heights)), bals, heights, sends, next, from, to, amount, short)

# LAW 3
# -----

# The receive was accepted at this send: it now carries the receiver's
# mark, so the third check refuses the second receive.
def once_c3(+seen: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<L.Send>, +want: Nat, hit: {Nat.is_eq(id, want) == True{} : Bool}, mine: {Nat.is_eq(to, acc) == True{} : Bool}) -> {Laws.again(L.Sends.recv.c3(seen, acc, id, to, amount, got, t), acc, want) == L.Miss{} : L.Got}:
  match seen:
    case True{}:
      {==}
    case False{}:
      %Equal.sym(Bool, Nat.is_eq(id, want), True{}, hit) : {L.Sends.recv.pick(_, L.Sends.recv.c1(True{}, True{}, acc, id, to, amount, Con{acc, got}, t), L.Sends.recv.skip(L.Send{id, to, amount, True{}, Con{acc, got}}, L.Sends.recv(t, True{}, acc, want))) == L.Miss{} : L.Got}
      %Equal.sym(Bool, Nat.is_eq(to, acc), True{}, mine) : {L.Sends.recv.c2(_, acc, id, to, amount, Con{acc, got}, t) == L.Miss{} : L.Got}
      %Equal.sym(Bool, Nat.is_eq(acc, acc), True{}, N.is_eq_same(acc)) : {L.Sends.recv.c3(Bool.or(_, L.Sends.mem(acc, got)), acc, id, to, amount, Con{acc, got}, t) == L.Miss{} : L.Got}
      {==}

def once_c2(+mine: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<L.Send>, +want: Nat, hit: {Nat.is_eq(id, want) == True{} : Bool}, e: {Nat.is_eq(to, acc) == mine : Bool}) -> {Laws.again(L.Sends.recv.c2(mine, acc, id, to, amount, got, t), acc, want) == L.Miss{} : L.Got}:
  match mine:
    case False{}:
      {==}
    case True{}:
      once_c3(L.Sends.mem(acc, got), acc, id, to, amount, got, t, want, hit, e)

def once_c1(+conf: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<L.Send>, +want: Nat, hit: {Nat.is_eq(id, want) == True{} : Bool}) -> {Laws.again(L.Sends.recv.c1(conf, True{}, acc, id, to, amount, got, t), acc, want) == L.Miss{} : L.Got}:
  match conf:
    case False{}:
      {==}
    case True{}:
      once_c2(Nat.is_eq(to, acc), acc, id, to, amount, got, t, want, hit, {==})

# The receive was accepted further down the list: this send is not the one
# asked for, so the second receive passes it again and meets the mark below.
def once_skip(+g: L.Got, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +conf: Bool, +got: +List<Nat>, +want: Nat, miss: {Nat.is_eq(id, want) == False{} : Bool}, ih: {Laws.again(g, acc, want) == L.Miss{} : L.Got}) -> {Laws.again(L.Sends.recv.skip(L.Send{id, to, amount, conf, got}, g), acc, want) == L.Miss{} : L.Got}:
  match g:
    case L.Miss{}:
      {==}
    case L.Hit{paid, t}:
      %Equal.sym(Bool, Nat.is_eq(id, want), False{}, miss) : {L.Sends.recv.pick(_, L.Sends.recv.c1(conf, True{}, acc, id, to, amount, got, t), L.Sends.recv.skip(L.Send{id, to, amount, conf, got}, L.Sends.recv(t, True{}, acc, want))) == L.Miss{} : L.Got}
      %Equal.sym(L.Got, L.Sends.recv(t, True{}, acc, want), L.Miss{}, ih) : {L.Sends.recv.skip(L.Send{id, to, amount, conf, got}, _) == L.Miss{} : L.Got}
      {==}

def once_pick(+hit: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +conf: Bool, +got: +List<Nat>, +t: +List<L.Send>, +want: Nat, e: {Nat.is_eq(id, want) == hit : Bool}, ih: {Laws.again(L.Sends.recv(t, True{}, acc, want), acc, want) == L.Miss{} : L.Got}) -> {Laws.again(L.Sends.recv.pick(hit, L.Sends.recv.c1(conf, True{}, acc, id, to, amount, got, t), L.Sends.recv.skip(L.Send{id, to, amount, conf, got}, L.Sends.recv(t, True{}, acc, want))), acc, want) == L.Miss{} : L.Got}:
  match hit:
    case True{}:
      once_c1(conf, acc, id, to, amount, got, t, want, e)
    case False{}:
      once_skip(L.Sends.recv(t, True{}, acc, want), acc, id, to, amount, conf, got, want, e, ih)

def Laws.receive_once(ss, acc, want):
  match ss:
    case Nil{}:
      {==}
    case Con{L.Send{id, to, amount, conf, got}, t}:
      once_pick(Nat.is_eq(id, want), acc, id, to, amount, conf, got, t, want, {==}, Laws.receive_once(t, acc, want))
