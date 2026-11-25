# Bend laws: proving that value cannot be spent twice

`test/bend/` holds rules about go-zenon written in the [Bend](https://github.com/bendlang/bend) language, their proofs, and a test that compares them with the node. This document says what they are, how to run them, and what a pass does and does not mean.

Nothing here is built into `znnd`. The Go tests sit behind the `bendrules` build tag, so `go build ./...` and `go test ./...` never start them.

## What you need

| Tool | Notes |
|---|---|
| Go | the version in `go.mod`, with cgo |
| docker | Bend runs only in a container: `test/bend/Dockerfile` fetches the Bend 2.0.40 release archive and checks its SHA-256. Nothing is installed on the host |
| python3 | 3.9 or later, standard library only |

With Colima the repository must be under your home directory, because only that is shared into the docker VM.

## The idea

A test tries inputs: the ones somebody thought of, or random ones. A Bend rule states a property for all inputs and has it checked by proof. [Bend](https://github.com/bendlang/bend) is a language whose compiler is a proof checker: a `law` is a claim, and the compiler refuses the file until a proof of the claim is supplied.

Bend proves things about Bend code, and `znnd` is Go. So a rule has two halves, and a result is only as strong as the weaker one:

1. **A model with laws.** The part of the node that the rule is about is written again in Bend, as small as it can be made while keeping the node's structure. The laws are proven about this model, for every input.
2. **A comparison with the node.** A Go test gives the same blocks to the model and to the real chain, verifier and VM, and fails when they disagree. This is evidence that the model describes the node, on the inputs tried. It is not a proof.

## Running it

```sh
test/bend/run.sh                                   # about 10 minutes once the images exist
OUT=<dir> BEND_RANDOM=300 test/bend/run.sh              # more random scenarios, logs kept in <dir>
VERDICT=0 test/bend/run.sh                         # without the second check and its image
DEPTH=4 SHARDS=6 test/bend/run.sh                  # step 6 to length 4, in six processes: about an hour
```

In CI the same script runs from `.github/workflows/bend.yml` on every pull request to `dev` or to a `release/**` branch: steps 1 and 3 to 6 always, step 2 when a file under `test/bend/` changed, and step 6 at the default length. The workflow can also be started by hand with a `depth` and a number of `shards`, to take step 6 further.

The first run builds two images. `go-zenon-bend:2.0.40` (290 MB) is the Bend release archive, checked against the SHA-256 that Bend's own installer carries for it. `go-zenon-bend:2.0.40-verdict` (4.4 GB, a few minutes) adds Lean 4.34.0, which Bend's second check needs. Bend always runs without a network and with the rule directory mounted read-only.

| Variable | Default | Meaning |
|---|---|---|
| `OUT` | `test/bend/.scratch/out` | where the logs go. Two runs at the same time need an `OUT` each; what a run writes while it works is in a directory of its own under `test/bend/.scratch`, removed when it ends |
| `RULES` | `ledger escrow` | the rules steps 1 to 3 run on; step 4 always runs every case |
| `VERDICT` | `1` | `0` skips step 2 |
| `BEND_RANDOM` | `40` | number of random scenarios per rule in step 4; `0` runs the written ones only. A value that is not a whole number fails the test |
| `BEND_SEED` | `1` | seed of the first random scenario; scenario `random-N` is seed N |
| `BEND_IMAGE` | `go-zenon-bend:2.0.40` | the image with `bend` |
| `NODE` | `1` | `0` skips step 4 |
| `FAULTS` | `1` | `0` skips step 5 |
| `DEPTH` | `2` | the length step 6 goes to; `0` skips it. The two lock cases go one further |
| `SHARDS` | `1` | how many processes share step 6 |

The script runs six steps and exits 0 only when all pass:

| Step | What it does | Pass |
|---|---|---|
| 1. proofs | `bend PROOF.bend --check-only` | prints `ALL PROOFS CHECK` |
| 2. verdict | `bend PROOF.bend --verdict`: the proofs again, through Bend's small kernel, which has a soundness proof in Lean | prints `ALL PROOFS CHECK` |
| 3. mutants | puts one fault at a time into a copy of the model (`test/bend/mutants.py`), checks the proofs, and computes the mutant's witness | every mutant is `caught`: the proofs fail and, for a rule that has witnesses, a law is false at the witness. `mutants.log` names the first proof that broke |
| 4. node | `go test -tags bendrules ./test/bend/` | every scenario is `same`, and the node-only cases pass. The tests are named in `run.sh` (`NODE_TESTS`): the go tool must find exactly those in the package, and exactly those must be in the output as passed, so a run that started no test is a failure |
| 5. node faults | removes one check at a time from a copy of the node's Go code (`test/bend/node_faults.py`) and runs the comparison of step 4 that covers it | every fault is `caught`: the comparison fails |
| 6. exhaustive | runs every sequence of up to `DEPTH` steps from a short list of steps, on the node and on the model | no sequence differs, and each exhaustive test named in `run.sh` (`EXHAUSTIVE_TESTS`) reports, for every shard, the sequences it compared, as a count and a digest of their names, and they are that shard's share of the sequences there are of that length |

A mutant is a fault that step 3 puts into a copy of the model on purpose, and removes again. It is not a fault of the node and there is nothing to fix when one is `caught`; `caught` is the passing result. A fault of the node shows up in step 4, as a scenario marked `DIFFERENT` or a node-only case that fails.

Step 3 is there because a law can be proven and still say nothing, when it is stated too weakly. Each mutant is a way to double spend or to get paid without the right to; if the proofs still passed with it in the model, the laws would not be about that. It has already earned its place: the first set of escrow laws let two mutants through (a reclaim by any account, an unlock with the wrong preimage), and laws 7 and 8 were added for them.

Step 5 is the same idea turned on the node. Steps 1 to 3 show that the laws hold for the model and would notice a faulty model; they say nothing about whether step 4 would notice a faulty node. So step 5 makes the node faulty: it lets an overspend through, removes the received check, the recipient check, the deletion of a paid entry, the refund and so on, one at a time, and requires the comparison to fail. Each fault names one written scenario of its test and the two lines that scenario must end in with the fault in the node: what the node did and what the model says. Where the test also checks the node directly for what the fault breaks (the entry still held after an unlock, the stake not emptied, the refund as it came back), the fault names that line as well, with the scenario and the step it must come from. A fault counts as `caught` only when the named test fails and prints exactly that difference and that line, in lines of its own; the node's log lines do not count, whatever they say. A call whose answer could not be read is marked `?` in the node's line, neither accepted nor refused, so it can pass for no fault. A test that fails in another way, on another scenario, with a crash, or because a call could not be read back, is reported as `ERROR`. A fault name the script does not know, or one given twice, stops it. The whole output of each fault's test is kept in `node-faults-output/`. (Removing the balance check alone is not among the faults for that reason: the VM then panics on the negative balance and still refuses the block.) No file of the node is edited. The faulty copy is written under the run's own directory in `test/bend/.scratch` and handed to the Go tool with `go test -overlay`, which compiles it in place of the original for that one test run. `node-faults.log` has one line per fault with the number of scenarios that differ; `node-faults-detail.log` has the line of the node, the line that replaced it, and the first lines of the failing test.

Step 6 answers "only the scenarios tried". A random scenario is a sample. Step 6 takes a short list of steps for each comparison (its alphabet) and runs every sequence of them up to a length. Within that bound nothing is sampled: if the node and the model can disagree on some order of those steps of that length, step 6 finds it. What it does not cover is longer sequences and steps that are not in the list.

| Case | Steps in the list | Sequences at `DEPTH=4` |
|---|---|---|
| BX-1 `TestExhaustiveLedger`, BX-6 `TestExhaustiveLedgerAfterSend` | 11: three sends from account 0 (a small amount, the whole balance, one more than the balance), a send back of everything, five receives (the right account, the wrong ones, a send that does not exist yet), a momentum, a send at a taken height. BX-1 starts from a ledger with no send; BX-6 from one where account 0 has sent 7 to account 1 and a momentum has confirmed it | 16,104 each, of up to 4 steps |
| BX-2 `TestExhaustiveHtlc`, BX-3 `TestExhaustivePtlc` | 9: a create with a long and with a short expiry, an unlock with the right and with the wrong preimage or signature, an unlock of a second entry, a reclaim by the maker and by another account, a momentum, a call that fails with an amount | 7,380 each, of up to 4 steps |
| BX-4 `TestExhaustiveStake`, BX-5 `TestExhaustiveFusion` | 6: a short and a long lock by two accounts, a cancel by the maker and by another account, a cancel of the second entry, a momentum | 9,330 each, of up to 5 steps |

At the default `DEPTH=2` the lists are short: 132 sequences for each ledger case, 90 for HTLC and for PTLC, 258 for stake and for fusion (three steps). From a ledger with no send, a receive that is accepted takes three steps (send, momentum, receive), so BX-1 at that length has none. BX-6 is there for that: on its ledger the first step can be an accepted receive and the second a receive of the same send again. Two sends in flight are within reach at that length (BX-1, a send twice), and so is spending on what was received (BX-6, the receive and then a send of everything); from an empty ledger that takes four steps. A rollback is in no list at any length: `TestRollbackKeepsTotal` is the only test of it.

A proof that stops checking does not by itself show that a law is false. A proof is written against the model's text, and it can stop fitting when the text changes while every law still holds. That happened: a model that credits a receive to the wrong account keeps the total, so `no_value_created` is still true of it, and yet its proof no longer checked and the mutant counted as caught. A rule with a `witness.bend` is therefore held to more. For each mutant that file has one witness: the statement of a named law computed at one fixed input, the same expression as in `LAWS.bend` with values for the law's variables. It must be `True` on the model and `False` with the mutant in it, and a mutant whose proofs break while its witness still holds is `MISSED`. Both rules have witnesses for all of their mutants. Which rules must have them is said by name in `run.sh` (`WITNESSED`), not read off the presence of the file: if a named rule's `witness.bend` is missing, every one of its mutants is `MISSED`. A rule that is not named there and has no `witness.bend` is judged by its proofs alone, and its lines in `mutants.log` say so; one that has the file without being named is a mistake in the script, and its mutants are `MISSED` too. Every mutant gets exactly one line in `mutants.log`, also when the mutant's text no longer fits the model or its witness gives no single answer; the raw output of the proof check and of both witness runs is in `mutants-detail.log`. The verdict is made by `mutants.py --judge` from the output as Bend wrote it, compared byte for byte, and the step fails unless every listed mutant has one line and every line is `caught`. The witnesses cannot read `LAWS.bend`, which does not load beside a broken proof, so the readings the laws use are written in `witness.bend` a second time; that the two copies agree is checked by reading, not by the tools.

To see where a mutant breaks, read `mutants-detail.log`. For each mutant it has the line of the model and the line that replaced it, and then the checker's report: the proof that stopped, what that proof needed (`expected`) and what the faulty model gives (`observed`). For example, with the balance check removed the proof that a debit takes exactly the amount stops in the refused case, where it now has to show that the amount equals the balance:

```
=== ledger/no-balance-check: a send above the balance is accepted
  model :       None{}
  mutant:       Some{0n}
SOME PROOFS FAIL
- expected : amt
- observed : h
Location: take_go
```

The checker stops at the first proof that fails, so the report names one place, not every law the fault would break. A mutant that only changes who may call or when (the last four of the escrow rule) leaves the general proofs intact and breaks one of the laws that compute a whole story; the report then shows the two end states side by side.

## Rule 1: nothing can be spent twice on the ledger

`test/bend/ledger/`.

| File | What it is | Who changes it |
|---|---|---|
| `ledger.bend` | the model: balances, account-chain heights, sends, receives, momentums. Its header maps each part to the node's code | changes when the node's rule changes |
| `LAWS.bend` | the laws, each with a comment in plain words | a person. This is the file to read and to review |
| `PROOF.bend`, `nat.bend` | the proofs | whoever makes step 1 pass; they need no review beyond steps 1 and 2 |
| `trace.bend` | prints what the model does with a list of blocks, for step 4 | |

| Law | Says |
|---|---|
| `no_value_created` | for every ledger and every sequence of sends, receives and momentums, valid or not: after the accepted ones are applied and the refused ones dropped, balances plus value in transit is what it was |
| `overspend_refused` | a send above the sender's balance is refused, whatever else is true |
| `receive_once` | when a receive is accepted, the same receive is refused on the ledger that results |
| `recipient_rule_holds_the_total`, `without_recipient_rule_value_appears` | four blocks in which two accounts receive one send: with the rule that only the addressee may receive, 10 stays 10; without it, 15. The "already received" mark alone does not stop this, because the node keeps it per receiving account |
| `receive_only_by_the_addressee`, `receive_only_when_confirmed` | for every ledger, account and send: the send is addressed to the account and a momentum has confirmed it, or the receive is refused |
| `receive_credits_the_amount_of_the_send` | whenever a receive is accepted, the balances afterwards are the balances before with that send's amount added to the receiver: not another amount, not another account |
| `receive_marks_only_that_send` | whenever a receive is accepted, the sends afterwards are the sends before with the receiver's mark on the one asked for; every other send is as it was |
| `send_debits_the_sender`, `send_is_recorded_as_sent` | whenever a send is accepted, the balances afterwards are the balances before with the amount taken from the sender, and one send is added, addressed to the account and with the amount the block names |
| `credit_changes_one_balance`, `debit_changes_one_balance` | a credit or a debit to one account leaves the balance of every other account as it was |
| `momentum_only_confirms` | a momentum leaves balances, heights and the count of sends as they were, and every send as it was apart from being confirmed: none changes its addressee or amount, gains or loses a mark, moves, appears or disappears. What "confirmed and otherwise unchanged" means is written out in `LAWS.bend`, not taken from the model |
| `send_only_at_the_next_height`, `receive_only_at_the_next_height` | a block is accepted only at the next height of its account's chain |
| `send_raises_its_height`, `receive_raises_its_height` | an accepted block raises its own account's height by one and no other |
| `send_takes_the_next_number`, `receive_keeps_the_count` | an accepted send takes the next number and moves the count on by one; a receive leaves the count alone |

The first law, `no_value_created`, counts one total. A ledger that paid a send to the wrong account would keep that total: that is theft, not creation, and that law does not see it. The laws from `receive_only_by_the_addressee` on are there for that: they say where every unit goes.

One thing these laws do not reach: that every send in a ledger has a number of its own. Two sends accepted one after the other get different numbers, but that a ledger reached from a start never holds two sends with one number needs an argument over whole histories, which is not made. The laws about a receive speak of "the first send with that number", as the model does.

| Case | What step 4 does | Places covered |
|---|---|---|
| BL-1 `TestLedgerModelMatchesNode` | six written scenarios and `BEND_RANDOM` random ones of 16 blocks, each on a fresh in-process chain (`zenon/mock`). Blocks are built and signed as a client would and go through `Supervisor.ApplyBlock` and the account pool, the path of a published block. Compares, block by block, accepted or refused, and the balances at the end | a send above the balance; two unconfirmed sends that fit one by one but not together; a send received twice; a send received by two accounts; a receive before the send is confirmed; a second block at a confirmed height; a gap in heights |
| BL-2 `TestReplacementSpendsOnce` | node only. A second block at a height whose block is still unconfirmed: the pool refuses it or drops the pending one for it. Checks that exactly one of the two sends takes effect and the other cannot be received | two blocks at one account height, before confirmation |
| BL-3 `TestRollbackKeepsTotal` | node only. A send is confirmed and received, then the chain is rolled back to before it. Checks that balances plus transit are unchanged and no receive survives without its send | a rollback |

## Rule 2: a contract pays each entry once

`test/bend/escrow/`. The same four files. The model is the HTLC and the PTLC contract: an entry is created with an amount, a payee and an expiry; before the expiry it is unlocked; from the expiry on its maker reclaims it. HTLC unlocks with the right preimage and pays the entry's payee (`Unlock`). PTLC unlocks with a signature of the entry's point over the entry and an account, and pays that account (`Claim`). The same model covers the two locks: a stake and a fusion are entries whose payee is their maker, and are paid back to the maker from the expiry on. A fusion cancel removes the entry (`Reclaim`, as in HTLC). Before `DynamicPlasmaSpork`, a stake cancel leaves the entry in place holding nothing and accepts repeat cancels (`Cancel`, which is what the Stake contract does pre-spork); from `DynamicPlasmaSpork`, repeat cancels are refused (`ErrStakeAlreadyRevoked`), matching `Reclaim` on active entries. A call that carries an amount and that a contract refuses is in the model too (`Failed`): the amount leaves the caller with the call and comes back with the refund, which is what `VM.rollbackEmbedded` does for every contract. The model uses the balances of rule 1 and that rule's proofs about them.

| Law | Says |
|---|---|
| `vault_keeps_value` | for every set of balances and entries, every time and every sequence of creates, unlocks, reclaims and momentums, valid or not: balances plus the value held in entries is what it was |
| `unlock_takes_the_entry`, `reclaim_takes_the_entry`, `claim_takes_the_entry` | whenever a payout is made the contract holds exactly one entry fewer; there is no paying and keeping |
| `windows_do_not_overlap` | "before the expiry" and "from the expiry on" never hold together. A fact about the two comparisons; the next row is about the contract |
| `unlock_only_in_time`, `claim_only_in_time`, `reclaim_only_when_due`, `cancel_only_when_due` | for every set of entries, entry asked for, caller and time: an unlock or a claim pays only before the entry's expiry, a reclaim or a stake cancel only from it on |
| `reclaim_only_by_the_maker`, `cancel_only_by_the_maker` | a reclaim or a stake cancel pays only when the caller made the entry, at any time |
| `unlock_pays_once`, `reclaim_pays_once` | two stories computed to the end: an entry unlocked, then asked for again by unlock and by reclaim; an entry reclaimed, then asked for again. Each pays once |
| `wrong_preimage_pays_nothing`, `only_the_maker_reclaims` | the wrong preimage moves nothing; after the expiry an account that did not make the entry gets nothing and the entry stays |
| `second_cancel_pays_zero` | a stake computed to the end: a cancel before the expiry and one by another account are refused; the maker's cancel pays it back; a second cancel of the cancelled stake pays zero |
| `cancel_empties_the_entry` | after any stake cancel that pays, the entry holds nothing, whatever it held and whatever else the contract holds; no later cancel of it can pay anything but zero |
| `failed_call_changes_nothing` | for every vault, account and amount: a call that carries the amount and is refused by the contract leaves every balance, every entry and the time exactly as they were |
| `unlock_pays_what_the_entry_holds`, and the same for `claim`, `reclaim` and `cancel` | whenever a payout is made, the amount is exactly what the entry asked for holds at that moment: not more and not less |
| `unlock_leaves_the_others`, and the same for `claim`, `reclaim` and `cancel` | a payout touches its own entry and no other: afterwards the contract holds exactly the entries it held before without the one asked for (for a stake cancel: with that one emptied), each with its maker, payee, amount and expiry as they were |
| `unlock_needs_the_preimage`, `claim_needs_the_signature` | without the preimage, or without a valid signature, no unlock pays, whatever the entries and the time |
| `early_cancel_refused`, `cancel_by_another_refused` | the two refusals of the stake story, each stated on its own, so that its end state cannot hide a cancel that paid too early or to the wrong caller |
| `covered_failed_call_is_accepted`, `second_cancel_is_accepted` | two single examples, each computed, for outcomes that the end states above do not fix. In the example vault, one failed call that the balance covers is accepted and leaves the vault as it was; and in the pre-`DynamicPlasmaSpork` stake story, the second cancel is accepted and pays zero. They show that the model does not get those end states by refusing; they are not statements about every covered call or every cancelled stake |
| `claim_pays_once` | a PTLC unlock without a valid signature is refused; with one it pays the account the signature names, once; a second claim and a reclaim find nothing |

| Case | What step 4 does | Places covered |
|---|---|---|
| BE-1 `TestEscrowModelMatchesHtlc` | twelve written scenarios and `BEND_RANDOM` random ones of 14 calls, each on a fresh in-process chain with the HTLC spork activated. Calls are real blocks to the HTLC contract, confirmed by momentums; payouts and refunds are received. Compares, call by call, accepted or refused, the four balances and the number of entries at the end. Accepted or refused is the status the contract writes into its receive block for the call; what the contract holds afterwards is checked against that status separately, so a call executed without effect, or refused with one, fails the test | unlock twice; unlock then reclaim; reclaim twice; reclaim then unlock; wrong preimage; both edges of the expiry; a create that is expired, empty or above the balance; several entries at once; calls that fail with an amount, between the others |
| BE-2 `TestEscrowModelMatchesPtlc` | the same scenarios on the PTLC contract. An entry's point is the payee's ed25519 key with no fixed destination; an unlock is that key's signature over the entry and the caller, and the wrong one is a signature over a message one bit off | the same, with the payout going to the caller the signature names |
| in BE-1 and BE-2 | the failing call is a sentinel registration (5,000 ZNN) by an account that has deposited no QSR, which the Sentinel contract refuses. Its mark is the one exception to "the mark is the contract's status": the model's `Failed` call is accepted when the call was made and the amount came back, so the mark is `A` when the contract's status is *failed* and the refund checks pass, and `R` when the balance did not cover the call and it was never made. For each one the test also checks on the node that exactly one block comes back and carries the amount, that the contract holds what it held before, and that the refund cannot be received a second time | a refund paid twice, kept, or short; a refund to an account that could not pay |
| BE-3 `TestEscrowModelMatchesStake` | six written scenarios and `BEND_RANDOM` random ones on the Stake contract before `DynamicPlasmaSpork` (matched against `Cancel`), with the stake unit set to ten seconds for the test (`constants.StakeTimeUnitSec` is a variable; the node's own unit tests shorten locks the same way). A cancel is accepted when the contract's receive block says so, and it must then be answered with exactly one block to the caller, whatever that block carries; a refused one with none | cancel before the expiry; cancel twice and three times; cancel by another account; cancel of an unknown entry; several stakes; a stake above the balance |
| BE-4 `TestEscrowModelMatchesFusion` | the same scenarios on the fusions of the Plasma contract, in QSR, with `constants.FuseExpiration` set to four momentums | the same |
| BE-5 `TestEscrowModelMatchesStakeAfterSpork` | the same stake scenarios with `DynamicPlasmaSpork` enforced (matched against `Reclaim` on active entries): the first cancel pays back the stake, empties `Amount` and stamps `RevokeTime`; any repeat cancel is refused (`ErrStakeAlreadyRevoked`), emits no block and leaves `RevokeTime` unchanged | the same, with repeat cancels refused after activation |

## Reading a result

- In `node.log` each scenario is one line: a letter per block, `A` accepted and `R` refused, then the four balances. `same` means the model printed the same line. On `DIFFERENT` the test prints both lines and every block with the node's reason. A disagreement is a finding: either the model is wrong about the node, or the node breaks a law. Decide which before changing either.
- A refusal of the node for a reason that is not one of the seven the rule is about fails the case. The scenario then tested something else.
- A scenario can be run again from its seed: `BEND_SEED=17 BEND_RANDOM=1`.

## What a pass means

A pass means: the laws hold for the model, for all inputs; on this commit the node did what the model does on every sequence within the bound of step 6 and on the random scenarios of step 4; and the comparison fails when the node is made faulty in the ways of step 5. What it does not mean:

- It is not a proof about `znnd`. A difference between the model and the node that needs more steps than `DEPTH`, or a step that is in no list, is found only if a random scenario happens on it.
- The models have one token, numbered accounts and one node. They do not describe signatures, plasma, the network, or two nodes that see different blocks.
- Rule 2 is compared with HTLC and with the plain form of PTLC: `Create`, `Unlock` with an ed25519 point, `Reclaim`. Not compared: PTLC's BIP340 and secp256k1 points, a fixed destination, `ProxyUnlock`, `CreateWithReclaimLock` and `ReclaimWithSignature`; HTLC's proxy-unlock setting (`DenyProxyUnlock`; any account may unlock, which is the default). The refund is compared on three failing calls: the sentinel registration without its deposit, and the HTLC and PTLC creates that are already expired. The code path is one for all contracts, but a failing call of another contract is not run here. A refund from one contract to another, which the EmbeddedRefund spork changes, is not modelled. Pillar, sentinel, accelerator, bridge and liquidity payouts are not modelled, and neither are rewards of any kind: the rules are about value that was put in and is paid back.
- BE-3, BE-4 and BE-5 run with shortened locks. They show what a cancel does once its lock has run out, not that the stock lock lengths are what they should be. Minimum amounts (1 ZNN to stake, 10 QSR in whole coins to fuse) are respected by the scenarios and not modelled. A stake entry that was cancelled is removed by the contract's reward update at the end of the epoch; no scenario runs that long.
- BL-2 and BL-3 are ordinary tests of the node. No law stands behind them.
- The model can only be run on numbers below 2^48 (Bend stops with an error above). The mock chain's balances are about 10^12 and fit. The proofs have no such limit; the node's amounts go up to 2^255.
- Bend 2 is young and its checker has no proof of its own. Step 2 is the answer to that: it accepts a proof only if the Lean-proven kernel accepts it too.

## Adding a law or a rule

To add a law to a rule: write it in `LAWS.bend` with its plain-words comment, run step 1, which now reports the law as open, and write the proof in `PROOF.bend` as `def Laws.<name>`. Add a mutant that the new law should catch, and its witness in `witness.bend` under the mutant's name with `-` written as `_`, printed by `main` as `<name> True`; the witness is the new law's own statement at one input. To add a rule: a new directory beside `ledger/` with the same four files (a rule may import another, as `escrow` imports `../ledger/ledger.bend`), a list of mutants under its name in `mutants.py` (a mutant is named within its rule, so names need not differ between rules), its name in `RULES` and, once it has a `witness.bend`, in `WITNESSED` in `run.sh`, and a Go test behind the `bendrules` tag. Step 3 copies every directory that has a `LAWS.bend` into each mutant's workspace, so a rule that imports another finds it there. Bend parameters can be used once unless marked `+`; `test/bend/plus.py <file>` marks them all. Bend has no proof search, so a proof is written out step by step; anything with division needs its own lemmas first.
