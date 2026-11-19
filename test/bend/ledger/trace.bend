import Base
import ./ledger.bend as L

# Prints what the model does with a sequence of blocks, in the form the Go
# comparison test (../ledger_test.go) prints for the node: one letter per
# block, A for accepted and R for refused, then the balances at the end.

def Trace.mark(n: L.Next) -> String:
  match n:
    case L.Refused{}:
      "R"
    case L.Accepted{state}:
      "A"

def Trace.steps(+bs: +List<L.Block>, +s: L.State) -> String:
  match bs:
    case Nil{}:
      ""
    case Con{b, t}:
      Trace.mark(L.step(s, True{}, b)) ++ Trace.steps(t, L.apply(s, True{}, b))

def Trace.nats(xs: +List<Nat>) -> String:
  match xs:
    case Nil{}:
      ""
    case Con{h, t}:
      " " ++ Nat.show(h) ++ Trace.nats(t)

def Trace.bals(s: L.State) -> String:
  match s:
    case L.State{bals, heights, sends, next}:
      Trace.nats(bals)

def Trace.run(+bs: +List<L.Block>, +s: L.State) -> String:
  Trace.steps(bs, s) ++ Trace.bals(L.run(bs, s, True{}))
