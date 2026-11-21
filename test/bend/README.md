# Bend laws

Rules about go-zenon written in the [Bend](https://github.com/bendlang/bend) language, proven for every input, and compared with the node. The two rules so far: nothing can be spent twice on the ledger, and a contract (HTLC, PTLC, stake, fusion) pays each entry once and gives a refused amount back once.

Read [docs/BEND_LAWS.md](../../docs/BEND_LAWS.md) first. To run everything, from the repository root, with Go, docker and python3:

```sh
test/bend/run.sh
```

| Path | What it is |
|---|---|
| `ledger/`, `escrow/` | one rule each: `LAWS.bend` (the laws, with a comment in plain words above each: the file to read), the model (`ledger.bend`, `escrow.bend`), the proofs (`PROOF.bend`, `nat.bend`), `trace.bend`, and `witness.bend` (one computed case of a law per mutant) |
| `*_test.go` | the comparison with the node, behind the `bendrules` build tag: `go test ./...` never starts it |
| `run.sh` | the six steps: proofs, second check, faults in the model, comparison, faults in the node, every short sequence |
| `mutants.py`, `node_faults.py` | the deliberate faults for the model and for the node |
| `Dockerfile`, `Dockerfile.verdict` | the Bend checker, and the same with Lean for the second check |
| `plus.py` | marks every parameter of a Bend file as reusable |

Nothing here is built into `znnd`, and nothing is installed on the host: Bend runs in a container without a network.
