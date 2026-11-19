import Base
import ../ledger/trace.bend as T
import ./escrow.bend as E

# Prints what the model does with a sequence of calls, in the form the Go
# comparison test (../escrow_test.go) prints for the node: one letter per
# call, A for accepted and R for refused, then the balances and the number
# of entries at the end. A momentum prints nothing.

def Trace.mark(c: E.Call, o: E.Out) -> String:
  match c o:
    case E.Tick{} o:
      ""
    case c E.Refused{}:
      "R"
    case c E.Accepted{vault}:
      "A"

def Trace.steps(+cs: +List<E.Call>, +v: E.Vault) -> String:
  match cs:
    case Nil{}:
      ""
    case Con{c, t}:
      Trace.mark(c, E.step(v, c)) ++ Trace.steps(t, E.apply(v, c))

def Trace.end(v: E.Vault) -> String:
  match v:
    case E.Vault{bals, entries, now, next}:
      T.Trace.nats(bals) ++ " entries " ++ Nat.show(E.Entries.count(entries))

def Trace.run(+cs: +List<E.Call>, +v: E.Vault) -> String:
  Trace.steps(cs, v) ++ Trace.end(E.run(cs, v))
