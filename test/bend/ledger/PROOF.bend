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

# LAWS 5 to 13: who is paid, and how much
# ---------------------------------------

def or_true(+a: Bool) -> {Bool.or(a, True{}) == True{} : Bool}:
  match a:
    case False{}:
      {==}
    case True{}:
      {==}

# Readings of the answer to Sends.recv: whether it was refused, what it
# paid, and the sends it left (other when it was refused).
def missed(+g: L.Got) -> Bool:
  match g:
    case L.Miss{}:
      True{}
    case L.Hit{amount, sends}:
      False{}

def paid(+g: L.Got, +other: Nat) -> Nat:
  match g:
    case L.Miss{}:
      other
    case L.Hit{amount, sends}:
      amount

def left(+g: L.Got, +other: +List<L.Send>) -> +List<L.Send>:
  match g:
    case L.Miss{}:
      other
    case L.Hit{amount, sends}:
      sends

# "cond, or refused", through the search for the send asked for.
def m_skip(+c: Bool, +s: L.Send, +g: L.Got, ih: {Bool.or(c, missed(g)) == True{} : Bool}) -> {Bool.or(c, missed(L.Sends.recv.skip(s, g))) == True{} : Bool}:
  match c g:
    case True{} g:
      {==}
    case False{} L.Miss{}:
      {==}
    case False{} L.Hit{amount, t}:
      Empty.absurd({False{} == True{} : Bool}, N.false_is_not_true(ih))

def m_pick(+hit: Bool, +a: Bool, +b: Bool, +here: L.Got, +other: L.Got, e1: {Bool.or(a, missed(here)) == True{} : Bool}, e2: {Bool.or(b, missed(other)) == True{} : Bool}) -> {Bool.or(Bool.pick(Bool, hit, a, b), missed(L.Sends.recv.pick(hit, here, other))) == True{} : Bool}:
  match hit:
    case True{}:
      e1
    case False{}:
      e2

# The send is the one asked for: it is addressed to the receiver, or the
# second check refuses.
def addr_c2(+mine: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<L.Send>) -> {Bool.or(mine, missed(L.Sends.recv.c2(mine, acc, id, to, amount, got, t))) == True{} : Bool}:
  match mine:
    case False{}:
      {==}
    case True{}:
      {==}

def addr_c1(+conf: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<L.Send>) -> {Bool.or(Nat.is_eq(to, acc), missed(L.Sends.recv.c1(conf, True{}, acc, id, to, amount, got, t))) == True{} : Bool}:
  match conf:
    case False{}:
      or_true(Nat.is_eq(to, acc))
    case True{}:
      addr_c2(Nat.is_eq(to, acc), acc, id, to, amount, got, t)

law recv_addressee:
  for +ss: +List<L.Send>
  for +acc: Nat
  for +want: Nat
  {Bool.or(Laws.addressed_to(ss, want, acc), missed(L.Sends.recv(ss, True{}, acc, want))) == True{} : Bool}

def recv_addressee(ss, acc, want):
  match ss:
    case Nil{}:
      {==}
    case Con{L.Send{id, to, amount, conf, got}, t}:
      m_pick(Nat.is_eq(id, want), Nat.is_eq(to, acc), Laws.addressed_to(t, want, acc), L.Sends.recv.c1(conf, True{}, acc, id, to, amount, got, t), L.Sends.recv.skip(L.Send{id, to, amount, conf, got}, L.Sends.recv(t, True{}, acc, want)), addr_c1(conf, acc, id, to, amount, got, t), m_skip(Laws.addressed_to(t, want, acc), L.Send{id, to, amount, conf, got}, L.Sends.recv(t, True{}, acc, want), recv_addressee(t, acc, want)))

# It is confirmed, or the first check refuses.
def conf_c1(+conf: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<L.Send>) -> {Bool.or(conf, missed(L.Sends.recv.c1(conf, True{}, acc, id, to, amount, got, t))) == True{} : Bool}:
  match conf:
    case False{}:
      {==}
    case True{}:
      {==}

law recv_confirmed:
  for +ss: +List<L.Send>
  for +acc: Nat
  for +want: Nat
  {Bool.or(Laws.confirmed(ss, want), missed(L.Sends.recv(ss, True{}, acc, want))) == True{} : Bool}

def recv_confirmed(ss, acc, want):
  match ss:
    case Nil{}:
      {==}
    case Con{L.Send{id, to, amount, conf, got}, t}:
      m_pick(Nat.is_eq(id, want), conf, Laws.confirmed(t, want), L.Sends.recv.c1(conf, True{}, acc, id, to, amount, got, t), L.Sends.recv.skip(L.Send{id, to, amount, conf, got}, L.Sends.recv(t, True{}, acc, want)), conf_c1(conf, acc, id, to, amount, got, t), m_skip(Laws.confirmed(t, want), L.Send{id, to, amount, conf, got}, L.Sends.recv(t, True{}, acc, want), recv_confirmed(t, acc, want)))

# What it pays is the send's own amount.
def paid_c3(+seen: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<L.Send>) -> {paid(L.Sends.recv.c3(seen, acc, id, to, amount, got, t), amount) == amount : Nat}:
  match seen:
    case True{}:
      {==}
    case False{}:
      {==}

def paid_c2(+mine: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<L.Send>) -> {paid(L.Sends.recv.c2(mine, acc, id, to, amount, got, t), amount) == amount : Nat}:
  match mine:
    case False{}:
      {==}
    case True{}:
      paid_c3(L.Sends.mem(acc, got), acc, id, to, amount, got, t)

def paid_c1(+conf: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<L.Send>) -> {paid(L.Sends.recv.c1(conf, True{}, acc, id, to, amount, got, t), amount) == amount : Nat}:
  match conf:
    case False{}:
      {==}
    case True{}:
      paid_c2(Nat.is_eq(to, acc), acc, id, to, amount, got, t)

def paid_skip(+g: L.Got, +s: L.Send, +h: Nat, ih: {paid(g, h) == h : Nat}) -> {paid(L.Sends.recv.skip(s, g), h) == h : Nat}:
  match g:
    case L.Miss{}:
      {==}
    case L.Hit{amount, t}:
      ih

def paid_pick(+hit: Bool, +here: L.Got, +other: L.Got, +amount: Nat, +below: Nat, e1: {paid(here, amount) == amount : Nat}, e2: {paid(other, below) == below : Nat}) -> {paid(L.Sends.recv.pick(hit, here, other), Bool.pick(Nat, hit, amount, below)) == Bool.pick(Nat, hit, amount, below) : Nat}:
  match hit:
    case True{}:
      e1
    case False{}:
      e2

law recv_pays_the_amount:
  for +ss: +List<L.Send>
  for +acc: Nat
  for +want: Nat
  {paid(L.Sends.recv(ss, True{}, acc, want), Laws.amount_of(ss, want)) == Laws.amount_of(ss, want) : Nat}

def recv_pays_the_amount(ss, acc, want):
  match ss:
    case Nil{}:
      {==}
    case Con{L.Send{id, to, amount, conf, got}, t}:
      paid_pick(Nat.is_eq(id, want), L.Sends.recv.c1(conf, True{}, acc, id, to, amount, got, t), L.Sends.recv.skip(L.Send{id, to, amount, conf, got}, L.Sends.recv(t, True{}, acc, want)), amount, Laws.amount_of(t, want), paid_c1(conf, acc, id, to, amount, got, t), paid_skip(L.Sends.recv(t, True{}, acc, want), L.Send{id, to, amount, conf, got}, Laws.amount_of(t, want), recv_pays_the_amount(t, acc, want)))

# What it leaves is the list with the one mark added.
def left_c3(+seen: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<L.Send>) -> {left(L.Sends.recv.c3(seen, acc, id, to, amount, got, t), Con{L.Send{id, to, amount, True{}, Con{acc, got}}, t}) == Con{L.Send{id, to, amount, True{}, Con{acc, got}}, t} : +List<L.Send>}:
  match seen:
    case True{}:
      {==}
    case False{}:
      {==}

def left_c2(+mine: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<L.Send>) -> {left(L.Sends.recv.c2(mine, acc, id, to, amount, got, t), Con{L.Send{id, to, amount, True{}, Con{acc, got}}, t}) == Con{L.Send{id, to, amount, True{}, Con{acc, got}}, t} : +List<L.Send>}:
  match mine:
    case False{}:
      {==}
    case True{}:
      left_c3(L.Sends.mem(acc, got), acc, id, to, amount, got, t)

def left_c1(+conf: Bool, +acc: Nat, +id: Nat, +to: Nat, +amount: Nat, +got: +List<Nat>, +t: +List<L.Send>) -> {left(L.Sends.recv.c1(conf, True{}, acc, id, to, amount, got, t), Con{L.Send{id, to, amount, True{}, Con{acc, got}}, t}) == Con{L.Send{id, to, amount, True{}, Con{acc, got}}, t} : +List<L.Send>}:
  match conf:
    case False{}:
      {==}
    case True{}:
      left_c2(Nat.is_eq(to, acc), acc, id, to, amount, got, t)

def left_skip(+g: L.Got, +s: L.Send, +b: +List<L.Send>, ih: {left(g, b) == b : +List<L.Send>}) -> {left(L.Sends.recv.skip(s, g), Con{s, b}) == Con{s, b} : +List<L.Send>}:
  match g:
    case L.Miss{}:
      {==}
    case L.Hit{amount, t}:
      %ih : {Con{s, t} == Con{s, _} : +List<L.Send>}
      {==}

def left_pick(+hit: Bool, +here: L.Got, +other: L.Got, +a: +List<L.Send>, +b: +List<L.Send>, e1: {left(here, a) == a : +List<L.Send>}, e2: {left(other, b) == b : +List<L.Send>}) -> {left(L.Sends.recv.pick(hit, here, other), Bool.pick(+List<L.Send>, hit, a, b)) == Bool.pick(+List<L.Send>, hit, a, b) : +List<L.Send>}:
  match hit:
    case True{}:
      e1
    case False{}:
      e2

law recv_marks_one:
  for +ss: +List<L.Send>
  for +acc: Nat
  for +want: Nat
  {left(L.Sends.recv(ss, True{}, acc, want), Laws.marked(ss, want, acc)) == Laws.marked(ss, want, acc) : +List<L.Send>}

def recv_marks_one(ss, acc, want):
  match ss:
    case Nil{}:
      {==}
    case Con{L.Send{id, to, amount, conf, got}, t}:
      left_pick(Nat.is_eq(id, want), L.Sends.recv.c1(conf, True{}, acc, id, to, amount, got, t), L.Sends.recv.skip(L.Send{id, to, amount, conf, got}, L.Sends.recv(t, True{}, acc, want)), Con{L.Send{id, to, amount, True{}, Con{acc, got}}, t}, Con{L.Send{id, to, amount, conf, got}, Laws.marked(t, want, acc)}, left_c1(conf, acc, id, to, amount, got, t), left_skip(L.Sends.recv(t, True{}, acc, want), L.Send{id, to, amount, conf, got}, Laws.marked(t, want, acc), recv_marks_one(t, acc, want)))

# From Sends.recv up to the block.
def rcv_or_take(+a: Bool, +g: L.Got, +bals: +List<Nat>, +heights: +List<Nat>, +next: Nat, +acc: Nat, e: {Bool.or(a, missed(g)) == True{} : Bool}) -> {Bool.or(a, Laws.refused(L.Step.rcv.take(g, bals, heights, next, acc))) == True{} : Bool}:
  match a g:
    case True{} g:
      {==}
    case False{} L.Miss{}:
      {==}
    case False{} L.Hit{amount, sends}:
      Empty.absurd({False{} == True{} : Bool}, N.false_is_not_true(e))

def rcv_or(+fits: Bool, +a: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +acc: Nat, +id: Nat, e: {Bool.or(a, missed(L.Sends.recv(sends, True{}, acc, id))) == True{} : Bool}) -> {Bool.or(a, Laws.refused(L.Step.rcv(fits, True{}, bals, heights, sends, next, acc, id))) == True{} : Bool}:
  match fits:
    case False{}:
      or_true(a)
    case True{}:
      rcv_or_take(a, L.Sends.recv(sends, True{}, acc, id), bals, heights, next, acc, e)

def Laws.receive_only_by_the_addressee(bals, heights, sends, next, acc, height, id):
  rcv_or(Nat.is_eq(height, 1n+L.Bal.get(acc, heights)), Laws.addressed_to(sends, id, acc), bals, heights, sends, next, acc, id, recv_addressee(sends, acc, id))

def Laws.receive_only_when_confirmed(bals, heights, sends, next, acc, height, id):
  rcv_or(Nat.is_eq(height, 1n+L.Bal.get(acc, heights)), Laws.confirmed(sends, id), bals, heights, sends, next, acc, id, recv_confirmed(sends, acc, id))

def rcv_bals_take(+g: L.Got, +bals: +List<Nat>, +heights: +List<Nat>, +next: Nat, +acc: Nat, +a: Nat, e: {paid(g, a) == a : Nat}) -> {Laws.bals_after(L.Step.rcv.take(g, bals, heights, next, acc), L.Bal.credit(acc, bals, a)) == L.Bal.credit(acc, bals, a) : +List<Nat>}:
  match g:
    case L.Miss{}:
      {==}
    case L.Hit{amount, sends}:
      %e : {L.Bal.credit(acc, bals, amount) == L.Bal.credit(acc, bals, _) : +List<Nat>}
      {==}

def rcv_bals(+fits: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +acc: Nat, +id: Nat) -> {Laws.bals_after(L.Step.rcv(fits, True{}, bals, heights, sends, next, acc, id), L.Bal.credit(acc, bals, Laws.amount_of(sends, id))) == L.Bal.credit(acc, bals, Laws.amount_of(sends, id)) : +List<Nat>}:
  match fits:
    case False{}:
      {==}
    case True{}:
      rcv_bals_take(L.Sends.recv(sends, True{}, acc, id), bals, heights, next, acc, Laws.amount_of(sends, id), recv_pays_the_amount(sends, acc, id))

def Laws.receive_credits_the_amount_of_the_send(bals, heights, sends, next, acc, height, id):
  rcv_bals(Nat.is_eq(height, 1n+L.Bal.get(acc, heights)), bals, heights, sends, next, acc, id)

def rcv_sends_take(+g: L.Got, +bals: +List<Nat>, +heights: +List<Nat>, +next: Nat, +acc: Nat, +m: +List<L.Send>, e: {left(g, m) == m : +List<L.Send>}) -> {Laws.sends_after(L.Step.rcv.take(g, bals, heights, next, acc), m) == m : +List<L.Send>}:
  match g:
    case L.Miss{}:
      {==}
    case L.Hit{amount, sends}:
      e

def rcv_sends(+fits: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +acc: Nat, +id: Nat) -> {Laws.sends_after(L.Step.rcv(fits, True{}, bals, heights, sends, next, acc, id), Laws.marked(sends, id, acc)) == Laws.marked(sends, id, acc) : +List<L.Send>}:
  match fits:
    case False{}:
      {==}
    case True{}:
      rcv_sends_take(L.Sends.recv(sends, True{}, acc, id), bals, heights, next, acc, Laws.marked(sends, id, acc), recv_marks_one(sends, acc, id))

def Laws.receive_marks_only_that_send(bals, heights, sends, next, acc, height, id):
  rcv_sends(Nat.is_eq(height, 1n+L.Bal.get(acc, heights)), bals, heights, sends, next, acc, id)

# Sends.
def snd_bals_pay(+m: Maybe<&2, +List<Nat>>, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +from: Nat, +to: Nat, +amount: Nat) -> {Laws.bals_after(L.Step.snd.pay(m, heights, sends, next, from, to, amount), Laws.debited(m, bals)) == Laws.debited(m, bals) : +List<Nat>}:
  match m:
    case None{}:
      {==}
    case Some{bs}:
      {==}

def snd_bals(+fits: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +from: Nat, +to: Nat, +amount: Nat) -> {Laws.bals_after(L.Step.snd(fits, bals, heights, sends, next, from, to, amount), Laws.debited(L.Bal.debit(from, bals, amount), bals)) == Laws.debited(L.Bal.debit(from, bals, amount), bals) : +List<Nat>}:
  match fits:
    case False{}:
      {==}
    case True{}:
      snd_bals_pay(L.Bal.debit(from, bals, amount), bals, heights, sends, next, from, to, amount)

def Laws.send_debits_the_sender(bals, heights, sends, next, from, height, to, amount):
  snd_bals(Nat.is_eq(height, 1n+L.Bal.get(from, heights)), bals, heights, sends, next, from, to, amount)

def snd_sends_pay(+m: Maybe<&2, +List<Nat>>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +from: Nat, +to: Nat, +amount: Nat) -> {Laws.sends_after(L.Step.snd.pay(m, heights, sends, next, from, to, amount), Con{L.Send{next, to, amount, False{}, Nil{}}, sends}) == Con{L.Send{next, to, amount, False{}, Nil{}}, sends} : +List<L.Send>}:
  match m:
    case None{}:
      {==}
    case Some{bs}:
      {==}

def snd_sends(+fits: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +from: Nat, +to: Nat, +amount: Nat) -> {Laws.sends_after(L.Step.snd(fits, bals, heights, sends, next, from, to, amount), Con{L.Send{next, to, amount, False{}, Nil{}}, sends}) == Con{L.Send{next, to, amount, False{}, Nil{}}, sends} : +List<L.Send>}:
  match fits:
    case False{}:
      {==}
    case True{}:
      snd_sends_pay(L.Bal.debit(from, bals, amount), heights, sends, next, from, to, amount)

def Laws.send_is_recorded_as_sent(bals, heights, sends, next, from, height, to, amount):
  snd_sends(Nat.is_eq(height, 1n+L.Bal.get(from, heights)), bals, heights, sends, next, from, to, amount)

# Balances, one account at a time.
def get_nil(+x: Nat) -> {L.Bal.get(x, Nil{}) == 0n : Nat}:
  match x:
    case 0n:
      {==}
    case 1n+p:
      {==}

def Laws.credit_changes_one_balance(x, a, bs, amt):
  match x a bs:
    case 0n 0n Nil{}:
      {==}
    case 0n 0n Con{h, t}:
      {==}
    case 0n 1n+ap Nil{}:
      {==}
    case 0n 1n+ap Con{h, t}:
      {==}
    case 1n+xp 0n Nil{}:
      get_nil(xp)
    case 1n+xp 0n Con{h, t}:
      {==}
    case 1n+xp 1n+ap Nil{}:
      %get_nil(xp) : {L.Bal.get(xp, L.Bal.credit(ap, Nil{}, amt)) == Bool.pick(Nat, Nat.is_eq(xp, ap), Nat.add(_, amt), _) : Nat}
      Laws.credit_changes_one_balance(xp, ap, Nil{}, amt)
    case 1n+xp 1n+ap Con{h, t}:
      Laws.credit_changes_one_balance(xp, ap, t, amt)

def debit0_head(+ok: Bool, +h: Nat, +amt: Nat, +t: +List<Nat>) -> {Laws.balance_after(L.Bal.debit.head(L.Bal.take.go(ok, h, amt), t), 0n, Nat.sub(h, amt)) == Nat.sub(h, amt) : Nat}:
  match ok:
    case False{}:
      {==}
    case True{}:
      {==}

def debit0_tail(+m: Maybe<&2, +List<Nat>>, +h: Nat) -> {Laws.balance_after(L.Bal.debit.tail(h, m), 0n, h) == h : Nat}:
  match m:
    case None{}:
      {==}
    case Some{t}:
      {==}

def debitS_head(+ok: Bool, +h: Nat, +amt: Nat, +t: +List<Nat>, +x: Nat) -> {Laws.balance_after(L.Bal.debit.head(L.Bal.take.go(ok, h, amt), t), 1n+x, L.Bal.get(x, t)) == L.Bal.get(x, t) : Nat}:
  match ok:
    case False{}:
      {==}
    case True{}:
      {==}

def debitS_tail(+m: Maybe<&2, +List<Nat>>, +h: Nat, +x: Nat, +e: Nat, ih: {Laws.balance_after(m, x, e) == e : Nat}) -> {Laws.balance_after(L.Bal.debit.tail(h, m), 1n+x, e) == e : Nat}:
  match m:
    case None{}:
      {==}
    case Some{t}:
      ih

def Laws.debit_changes_one_balance(x, a, bs, amt):
  match x a bs:
    case 0n 0n Nil{}:
      {==}
    case 0n 0n Con{h, t}:
      debit0_head(Nat.is_le(amt, h), h, amt, t)
    case 0n 1n+ap Nil{}:
      {==}
    case 0n 1n+ap Con{h, t}:
      debit0_tail(L.Bal.debit(ap, t, amt), h)
    case 1n+xp 0n Nil{}:
      {==}
    case 1n+xp 0n Con{h, t}:
      debitS_head(Nat.is_le(amt, h), h, amt, t, xp)
    case 1n+xp 1n+ap Nil{}:
      {==}
    case 1n+xp 1n+ap Con{h, t}:
      debitS_tail(L.Bal.debit(ap, t, amt), h, xp, Bool.pick(Nat, Nat.is_eq(xp, ap), Nat.sub(L.Bal.get(xp, t), amt), L.Bal.get(xp, t)), Laws.debit_changes_one_balance(xp, ap, t, amt))

# The model's confirmation is the one written in LAWS.bend.
law confirm_is_all_confirmed:
  for +ss: +List<L.Send>
  {L.Sends.confirm(ss) == Laws.all_confirmed(ss) : +List<L.Send>}

def confirm_is_all_confirmed(ss):
  match ss:
    case Nil{}:
      {==}
    case Con{L.Send{id, to, amount, conf, got}, t}:
      %confirm_is_all_confirmed(t) : {Con{L.Send{id, to, amount, True{}, got}, L.Sends.confirm(t)} == Con{L.Send{id, to, amount, True{}, got}, _} : +List<L.Send>}
      {==}

def Laws.momentum_only_confirms(bals, heights, sends, next):
  %confirm_is_all_confirmed(sends) : {L.Accepted{L.State{bals, heights, L.Sends.confirm(sends), next}} == L.Accepted{L.State{bals, heights, _, next}} : L.Next}
  {==}

# LAWS 14 to 19: heights and numbers
# ----------------------------------

def snd_fits(+fits: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +from: Nat, +to: Nat, +amount: Nat) -> {Bool.or(fits, Laws.refused(L.Step.snd(fits, bals, heights, sends, next, from, to, amount))) == True{} : Bool}:
  match fits:
    case False{}:
      {==}
    case True{}:
      {==}

def Laws.send_only_at_the_next_height(bals, heights, sends, next, from, height, to, amount):
  snd_fits(Nat.is_eq(height, 1n+L.Bal.get(from, heights)), bals, heights, sends, next, from, to, amount)

def rcv_fits(+fits: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +acc: Nat, +id: Nat) -> {Bool.or(fits, Laws.refused(L.Step.rcv(fits, True{}, bals, heights, sends, next, acc, id))) == True{} : Bool}:
  match fits:
    case False{}:
      {==}
    case True{}:
      {==}

def Laws.receive_only_at_the_next_height(bals, heights, sends, next, acc, height, id):
  rcv_fits(Nat.is_eq(height, 1n+L.Bal.get(acc, heights)), bals, heights, sends, next, acc, id)

def snd_h_pay(+m: Maybe<&2, +List<Nat>>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +from: Nat, +to: Nat, +amount: Nat) -> {Laws.heights_after(L.Step.snd.pay(m, heights, sends, next, from, to, amount), L.Bal.credit(from, heights, 1n)) == L.Bal.credit(from, heights, 1n) : +List<Nat>}:
  match m:
    case None{}:
      {==}
    case Some{bs}:
      {==}

def snd_h(+fits: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +from: Nat, +to: Nat, +amount: Nat) -> {Laws.heights_after(L.Step.snd(fits, bals, heights, sends, next, from, to, amount), L.Bal.credit(from, heights, 1n)) == L.Bal.credit(from, heights, 1n) : +List<Nat>}:
  match fits:
    case False{}:
      {==}
    case True{}:
      snd_h_pay(L.Bal.debit(from, bals, amount), heights, sends, next, from, to, amount)

def Laws.send_raises_its_height(bals, heights, sends, next, from, height, to, amount):
  snd_h(Nat.is_eq(height, 1n+L.Bal.get(from, heights)), bals, heights, sends, next, from, to, amount)

def rcv_h_take(+g: L.Got, +bals: +List<Nat>, +heights: +List<Nat>, +next: Nat, +acc: Nat) -> {Laws.heights_after(L.Step.rcv.take(g, bals, heights, next, acc), L.Bal.credit(acc, heights, 1n)) == L.Bal.credit(acc, heights, 1n) : +List<Nat>}:
  match g:
    case L.Miss{}:
      {==}
    case L.Hit{amount, sends}:
      {==}

def rcv_h(+fits: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +acc: Nat, +id: Nat) -> {Laws.heights_after(L.Step.rcv(fits, True{}, bals, heights, sends, next, acc, id), L.Bal.credit(acc, heights, 1n)) == L.Bal.credit(acc, heights, 1n) : +List<Nat>}:
  match fits:
    case False{}:
      {==}
    case True{}:
      rcv_h_take(L.Sends.recv(sends, True{}, acc, id), bals, heights, next, acc)

def Laws.receive_raises_its_height(bals, heights, sends, next, acc, height, id):
  rcv_h(Nat.is_eq(height, 1n+L.Bal.get(acc, heights)), bals, heights, sends, next, acc, id)

def snd_n_pay(+m: Maybe<&2, +List<Nat>>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +from: Nat, +to: Nat, +amount: Nat) -> {Laws.next_after(L.Step.snd.pay(m, heights, sends, next, from, to, amount), 1n+next) == 1n+next : Nat}:
  match m:
    case None{}:
      {==}
    case Some{bs}:
      {==}

def snd_n(+fits: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +from: Nat, +to: Nat, +amount: Nat) -> {Laws.next_after(L.Step.snd(fits, bals, heights, sends, next, from, to, amount), 1n+next) == 1n+next : Nat}:
  match fits:
    case False{}:
      {==}
    case True{}:
      snd_n_pay(L.Bal.debit(from, bals, amount), heights, sends, next, from, to, amount)

def Laws.send_takes_the_next_number(bals, heights, sends, next, from, height, to, amount):
  snd_n(Nat.is_eq(height, 1n+L.Bal.get(from, heights)), bals, heights, sends, next, from, to, amount)

def rcv_n_take(+g: L.Got, +bals: +List<Nat>, +heights: +List<Nat>, +next: Nat, +acc: Nat) -> {Laws.next_after(L.Step.rcv.take(g, bals, heights, next, acc), next) == next : Nat}:
  match g:
    case L.Miss{}:
      {==}
    case L.Hit{amount, sends}:
      {==}

def rcv_n(+fits: Bool, +bals: +List<Nat>, +heights: +List<Nat>, +sends: +List<L.Send>, +next: Nat, +acc: Nat, +id: Nat) -> {Laws.next_after(L.Step.rcv(fits, True{}, bals, heights, sends, next, acc, id), next) == next : Nat}:
  match fits:
    case False{}:
      {==}
    case True{}:
      rcv_n_take(L.Sends.recv(sends, True{}, acc, id), bals, heights, next, acc)

def Laws.receive_keeps_the_count(bals, heights, sends, next, acc, height, id):
  rcv_n(Nat.is_eq(height, 1n+L.Bal.get(acc, heights)), bals, heights, sends, next, acc, id)
