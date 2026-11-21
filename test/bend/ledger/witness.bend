import Base
import ./ledger.bend as L

# One witness per mutant of mutants.py: a single case of a named law,
# computed. On the model every witness is True. With its mutant put into
# the model, the witness must be False: that is what shows a law is false
# with the fault, and not merely that a proof no longer fits the text.
# run.sh (step 3) checks both.
#
# This file reads the model only, not LAWS.bend or PROOF.bend: with a
# mutant in the model the proofs do not check, and the witness must still
# run. Each witness names the law it is a case of.

def refused(+n: L.Next) -> Bool:
  match n:
    case L.Refused{}:
      True{}
    case L.Accepted{state}:
      False{}

# Account 0 holds 10; accounts 1 and 2 hold nothing.
def s0() -> L.State:
  L.State{[10n, 0n, 0n], [0n, 0n, 0n], [], 0n}

# Account 0 has sent 5 to account 1.
def sent() -> L.State:
  L.run([L.Snd{0n, 1n, 1n, 5n}], s0(), True{})

# ... and a momentum has confirmed it.
def confirmed() -> L.State:
  L.run([L.Snd{0n, 1n, 1n, 5n}, L.Tick{}], s0(), True{})

# ... and account 1 has received it.
def received() -> L.State:
  L.run([L.Snd{0n, 1n, 1n, 5n}, L.Tick{}, L.Rcv{1n, 1n, 0n}], s0(), True{})

def bal(+x: Nat, +s: L.State) -> Nat:
  match s:
    case L.State{bals, heights, sends, next}:
      L.Bal.get(x, bals)

# LAW 2: a send of 11 from a balance of 10 is refused.
def no_balance_check() -> Bool:
  refused(L.step(s0(), True{}, L.Snd{0n, 1n, 1n, 11n}))

# LAW 9: after sending 5 the sender holds 5.
def no_debit() -> Bool:
  Nat.is_eq(bal(0n, sent()), 5n)

# LAW 3: a second receive of the same send is refused.
def no_received_check() -> Bool:
  refused(L.step(received(), True{}, L.Rcv{1n, 2n, 0n}))

def no_received_mark() -> Bool:
  refused(L.step(received(), True{}, L.Rcv{1n, 2n, 0n}))

# LAW 5: account 2 cannot receive a send addressed to account 1.
def no_recipient_rule() -> Bool:
  refused(L.step(confirmed(), True{}, L.Rcv{2n, 1n, 0n}))

# LAW 7: the receiver of a send of 5 holds 5 afterwards.
def double_credit() -> Bool:
  Nat.is_eq(bal(1n, received()), 5n)

def credit_wrong_account() -> Bool:
  Nat.is_eq(bal(1n, received()), 5n)

# LAW 9: the 5 are taken from the sender.
def debit_wrong_account() -> Bool:
  Nat.is_eq(bal(0n, sent()), 5n)

# LAW 10: the send is addressed to account 1, which can receive it.
def send_to_wrong_account() -> Bool:
  Bool.not(refused(L.step(confirmed(), True{}, L.Rcv{1n, 1n, 0n})))

# LAW 6: before any momentum the send cannot be received.
def receive_unconfirmed() -> Bool:
  refused(L.step(sent(), True{}, L.Rcv{1n, 1n, 0n}))

# LAW 8: receiving send 1 leaves send 0 unconfirmed, so it cannot be
# received. (A ledger with an older send unconfirmed under a newer confirmed
# one is not one the model reaches from a start; the law covers it anyway.)
def receive_marks_another() -> Bool:
  refused(L.step(L.apply(L.State{[10n, 0n, 0n], [0n, 0n, 0n], [L.Send{1n, 1n, 5n, True{}, Nil{}}, L.Send{0n, 2n, 3n, False{}, Nil{}}], 2n}, True{}, L.Rcv{1n, 1n, 1n}), True{}, L.Rcv{2n, 1n, 0n}))

# LAW 10: the send is recorded with its amount, so its receiver gets 5.
def send_not_recorded() -> Bool:
  Nat.is_eq(bal(1n, received()), 5n)

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
    IO.print("receive-marks-another " ++ Bool.show(receive_marks_another()))
    IO.print("send-not-recorded " ++ Bool.show(send_not_recorded()))
