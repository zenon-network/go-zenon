# Testing go-zenon: framework and procedures

This document is the how. [TEST-PLAN.md](TEST-PLAN.md) is the what: the levels, the cases and the per-PR matrix. Anyone with the repository, Go and docker should be able to repeat every procedure here and get a result that can be compared with ours.

## 1. The rule that keeps gaps visible

No test effort can prove that everything was tested. What can be done is to make every untested item show up in a list that is generated from the code, so nothing is missed by forgetting it.

That is what `test/inventory/` does:

- `inventory.sh` reads the source and prints every testable item it can find by pattern: embedded-contract methods per contract, RPC methods (including `String` and `rpc.modules`, which the server also serves), subscription topics, sporks, verifier rules by receiver type, wire messages, protocol error codes, declared error values, command-line flags, and the fields of the two config structures. 535 items at `203ccc6`.
- `coverage.tsv` records, for each item, the cases that exercise it on a running node, as `class:case` tokens.
- `dispositions.tsv` lists the items that can never have a passing case, with the reason. Today that is one: `bridge.SetRedeemDelay`, declared in the ABI and implemented nowhere.
- `check.sh` compares them. It fails when an item has neither a case nor a disposition, when the source has an item the map does not list or the reverse, and on a duplicate, malformed or mis-tokened row.

```sh
test/inventory/check.sh          # counts per kind; exit 1 while anything is open
test/inventory/check.sh --list   # also names every open item
test/inventory/check.sh --sync   # after a code change: prints what it adds and drops, then rewrites the map
```

The map is filled by the runs, not by hand: a case that passes writes the items it exercised (P9), the smoke test writes the getters whose replies passed their shape check (P4), and `record.py` merges those files. `record.py` checks every row of every file first and writes nothing if one is malformed or names an item the inventory does not have. By default it only adds tokens, so a case that stops exercising an item keeps its token; `record.py --replace` rebuilds every item's tokens from the files of one complete baseline run and prints each token it drops. `check.sh` fails on a token whose case id no test defines, as left by a renamed or deleted case.

What the count means, and what it does not. "has-case" says that at least one passing case names the item. It does not say every edge class below is covered, and it does not say the case's assertion is strong: that is what review of the cases is for. The inventory is found by text patterns, so it can miss an item written in an unusual way; kinds it does not try to list at all are functions as such, branches inside a method, and anything only reachable by a misbehaving peer. Unit tests are not counted here on purpose: the map tracks what has been shown on a live network, and `go test -cover` tracks unit coverage separately.

A change that adds a contract method, an RPC method, an error value, a flag or a config field makes `check.sh` report it until someone runs `--sync` and a case covers it. That is the mechanism that stops the plan from going stale.

### What counts as covered

An item is covered when cases exist for every edge class that applies to it. One passing call is not coverage.

| Class | Applies to | What to try |
|---|---|---|
| E1 Happy path | everything | the normal call, result read back through RPC on all nodes |
| E2 Boundaries | every numeric, length and list input | zero, one, the limit, the limit plus one, the type maximum, negative where the encoding allows it |
| E3 Wrong caller | contract methods, admin RPC | non-owner, non-administrator, a contract address, an address with no chain |
| E4 Wrong state | contract methods | before the precondition, after it has expired, twice in a row, after revoke or cancel |
| E5 Wrong asset or amount | payable methods | wrong token, zero amount, amount above balance, amount that overflows a total |
| E6 Malformed encoding | every decoder: ABI, JSON, protobuf, wire frames, key files, config | truncated, trailing bytes, wrong types, null where an object is expected, deep nesting, huge strings |
| E7 Spork edge | everything a spork gates | one momentum before enforcement, at enforcement, one after; a block that acknowledges a momentum from before it |
| E8 Restart | anything with stored state | stop every node in the middle of the flow, start again, finish the flow |
| E9 Rollback | anything a momentum can undo | fork the network so the block is rolled back, then re-applied or dropped |
| E10 Concurrency | pools, caches, subscriptions, servers | the same action from many clients at once; conflicting actions in one momentum |
| E11 Resource limits | servers, pools, queues | at the cap, one over the cap, a client that never reads, a client that disconnects mid-request |
| E12 Time | anything with a height or time window | one tick before the window opens, the last tick inside, the first tick after; clock skew between nodes |

Tokens in `coverage.tsv` are the class and the case id, for example `E1:PL-1 E4:PL-3 E7:LAD-2`. An item with only `E1` is partly covered.

## 2. What you need

| Tool | Version used here | Notes |
|---|---|---|
| Go | 1.26 or later on the host, with cgo | `go.mod` minimum is 1.26.0 with `toolchain go1.27.4`. The driver (P9) runs on the host |
| docker with compose | any current | about 2 GB of free disk in the docker VM for the stock build |
| python3 | 3.9 or later | standard library only |
| git | any | the commit under test must be checked out |

With Colima the repository must be under your home directory, because only that is shared into the VM.

## 3. Procedures

Every procedure starts by writing down the commit: `git rev-parse HEAD`. A result is valid for that commit only.

### P1. Static checks and unit tests (levels L0 and L1)

```sh
go build ./...
go vet ./...                                  # six known warnings in p2p/legacy at 924e6b8
go test ./... -count=1                        # about 5 minutes, 45 packages
go test -race ./... -count=1                  # on an idle machine
go test ./... -count=5 -shuffle=on            # finds order-dependent tests
go build -tags libznn ./... && go test -tags libznn ./cmd/libznn/...
go run ./cmd/devnet-keygen --verify-genesis docker/devnet/genesis.json
```

Fuzzing, one target at a time:

```sh
go test ./vm/embedded/implementation -run '^$' -fuzz '^FuzzName$' -fuzztime 10m
```

List the targets with `grep -rn '^func Fuzz' --include='*_test.go' .`.

Pass: every command exits 0, except `go vet`, which passes when it reports nothing new.

### P2. Build the devnet image

Normal path:

```sh
make devnet-up        # builds with the repository Dockerfile and starts everything
```

When the docker VM is short on disk, or to pin the Go toolchain:

```sh
GOIMG=golang:1.27.4-alpine test/devnet/build-image.sh
```

The script compiles `znnd` in a Go container with the source mounted read-only (cgo is required), wraps it in a small alpine image tagged `go-zenon-devnet:latest`, and prints the binary's SHA-256 and `znnd version`. Check that the commit hash it prints is the one you wrote down.

### P3. Start the devnet

```sh
test/devnet/up.sh stock manifest.txt      # after P2's script; see P10 for the other variants
```

`up.sh` removes the previous chain and checks that it is gone, starts the five nodes, waits up to three minutes for each to answer RPC, and then proves what is running: every container's image id, the SHA-256 of its `znnd` against the binary P2 built, the SHA-256 of the genesis it was seeded with against the chosen file, the genesis momentum of every node against the one that `test/devnet/genesishash` computes from the genesis file without asking a node, one version on all five, and a height that shows the chain is new. The height recorded is that of one frontier reply, whose hash the node must serve at that height and whose parent it must serve one height below (the zero hash for the genesis momentum); the manifest records that hash and parent. These are checks of each node against itself; that the nodes agree with each other is the driver's settle wait, and `down.sh` at the end. Node answers are parsed as JSON, never matched by pattern. It exits non-zero and says why if any of that fails. The manifest file keeps those values for the results. (`make devnet-up` is the stock path with the explorer; it proves none of this.)

Five nodes on `172.30.0.0/24`, three pillars, chain id 69. RPC on `http://localhost:35997` and `ws://localhost:35998`; pillar 1 also on `35991`; the late joiner, when started, on `35999` and `36001`. On a machine where those host ports are taken (a znnd of its own runs there), set `DEVNET_HTTP_PORT`, `DEVNET_WS_PORT`, `DEVNET_PILLAR_HTTP_PORT`, `DEVNET_SYNC_HTTP_PORT` and `DEVNET_SYNC_WS_PORT` before P3: `docker-compose.yml` publishes those, `smoke.py` reads the HTTP ones (or `--port` and `--sync-port`), the driver builds its default `E2E_RPC` from them, and `baseline.sh` records them in `environment.txt`. Only the host side changes; inside the network every node still listens on 35997 and 35998. Topology, addresses and the dev mnemonic are in [docker/devnet/README.md](../docker/devnet/README.md).

Genesis sporks: Accelerator and HTLC from height 0, libp2p at 20, dynamic plasma at 30. The `bridge-liq` spork is listed as not activated, but the Bridge and Liquidity contracts are live all the same: HTLC is on, and the spork ladder treats HTLC as implying them. PTLC, EmbeddedRefund and EpochRewardTerms are not in this genesis, so nothing they gate can be tested on the stock devnet (see TEST-PLAN.md section 8, genesis variants).

### P4. Smoke test

```sh
python3 test/devnet/smoke.py --height 45 --out smoke.json --cov "$PWD/smoke.tsv"
python3 test/devnet/smoke.py --genesis test/devnet/genesis/all-on.json     # on another variant
```

It waits for the height, for at most 15 minutes, and stops with exit 2 if the frontier does not move for 90 seconds or a node answers with something that is not a JSON-RPC reply. Then it checks, against the genesis file the devnet was started on:

- all five nodes have the same momentum at every height up to the frontier minus two, with no hole, every one with a well-formed hash and pointing to the hash of the one before;
- every momentum was produced by a producer address of the genesis pillars, and all of them produced;
- no slot is empty, with two exceptions that are counted and reported instead of failed: the first 100 seconds after the first produced momentum (`--settle`), while the pillars are still connecting, and a slot that its pillar logged dropping as early (`current time is before start time`), which is a known issue of the node. The second is accepted only with that log line for exactly that slot, and only up to one judged slot in ten; `--strict-cadence` fails those too, and the baseline always runs with it: that log line is also what a pillar woken too early on a healthy clock would print, so accepting it can hide a scheduling defect. Any other empty slot fails the check;
- every spork of the genesis is listed with its id, activation flag and enforcement height, and nothing else is; momentums are version 2 exactly from the height after dynamic-plasma enforcement;
- every node reports sync done with at least two peers;
- one getter per RPC namespace returns a well-formed reply whose result has the expected fields, types and content.

Exit 0 means all passed, 1 that a check failed. `--cov` writes a coverage row for each getter whose reply passed.

The known issue behind the second exception: a pillar is woken for its slot by a timer set once, then compares the wall clock with the slot's start; if the wall clock is still before the start, because it was stepped back in between or ran slow against the timer, the pillar drops the slot and is not woken again. On a machine whose clock is being corrected the chain loses many slots.

With Colima that happens when the host's own clock is about 100 ms or more away from internet time (measured on 2026-11-22). Two services set the docker VM's clock: `systemd-timesyncd` in the VM steers it to internet time, and the Lima guest agent sets it to the host's clock whenever the two differ by more than about 100 ms. When the host is off by that much they fight, and the VM's clock is set back about 100 ms every 20 seconds. Restarting the VM does not cure it. Before a run:

```sh
sntp time.apple.com                                  # the host's offset; it should be a few milliseconds
sudo sntp -sS time.apple.com                         # sets the host's clock if it is not
colima ssh -- journalctl -u lima-guestagent --since "10 minutes ago" | grep -c SyncTime   # 0 when the VM's clock is left alone
```

A host's clock drifts again after it is set (the test machine's by about 44 ppm, so 100 ms in well under an hour), and then the fight starts again. What stopped it on the test machine, from attempt 7 of the rc1 baseline on, was to turn off the VM's own time service, so that only the agent sets the VM's clock and the VM follows the host. This changes the VM; it is undone when the VM is recreated and may be undone when it restarts:

```sh
test/devnet/vm-time-service-off.sh    # exits 0 only with NTP=no and the frequency correction read back as 0
```

The script runs `timedatectl set-ntp false` in the VM and then sets the kernel's frequency correction to 0: `systemd-timesyncd` steers by changing that frequency, and the change stays after the service stops (292 ppm on the test machine). It first appears in the framework at `5b8a36a`. To repeat a published run made at an earlier harness commit (attempt 7 was made at `e39b084`), prepare the VM from a checkout of the current framework branch first, then check out the harness commit for the run; the script changes only the VM, not the checkout.

With the service off the VM still drifts from the host, more slowly: on the test machine the agent corrected it by about 100 ms at 02:22 and again at 13:22 UTC on 2026-11-23. That is one observation, not a schedule; a correction can come during any run. A correction that stays shows in `clock-run` as a step between two readings; a step and its reversal within one interval can fall between them. The agent's log says when the last one was:

```sh
colima ssh -- journalctl -u lima-guestagent --utc --no-pager | grep SyncTime | tail -3
```

The node still needs a clock close to real time; with this setting the VM keeps the host's, so keep the host's set.

Also do not let the host sleep during a run. `test/baseline.sh` measures the clock before it starts a devnet and refuses to go on if it moves, samples it every 20 s for as long as the devnet stages last, and fails the `clock-run` stage if it moved. The number of empty slots is in `smoke.json` under `empty-slots` and belongs in the results.

### P5. Late joiner

Start the sixth node after the libp2p switch (height 20), give it 90 seconds, and check it:

```sh
DEVNET_GENESIS="$PWD/docker/devnet/genesis.json" DEVNET_IMAGE=go-zenon-devnet:latest \
  docker compose -f docker-compose.yml -f test/devnet/compose.variant.yml --profile sync up -d --no-build --no-deps syncnode
test/devnet/pin-start.sh syncnode   # pins its container ID and first start; down.sh refuses to remove a chain without it
sleep 90
python3 test/devnet/smoke.py --height 45 --expect-105 --out joiner-105.json
```

**Known failure at `203ccc6`, issue #105:** the late joiner stays at height 1 with no peers. It starts on the legacy transport because its own chain has not reached the libp2p spork, while every other node has already switched. `--expect-105` passes only on exactly that signature: zero peers, height 1, not synced, and a healthy reference chain. If the joiner syncs, the check fails; that is the day the expectation is removed here and in `test/baseline.sh`.

Then force the transport and check that the same node syncs:

```sh
docker exec znnd-devnet-syncnode sed -i 's/"Net": {/"Net": {\n        "P2PBackend": "libp2p",/' /root/.znn/config.json
docker exec znnd-devnet-syncnode grep '"P2PBackend": "libp2p"' /root/.znn/config.json    # the edit took
docker restart znnd-devnet-syncnode
python3 test/devnet/smoke.py --height 45 --sync --out joiner-libp2p.json                 # repeat for up to two minutes
```

Pass: state 2, at least one peer, and the same momentum as the RPC node at every height. Record both results.

### P6 and P7. Collect the evidence and tear down

```sh
test/devnet/down.sh logs-stock      # a new, empty directory per run
```

Before it removes anything, `down.sh` writes every node's final height and hash to `final.txt`, copies each node's `zenon.log` and container output, puts every error and critical line in `error-lines.txt`, and counts them by message, with hashes, addresses and heights blanked, in `error-histogram.txt`. It reads the final state up to three times, 12 s apart, and the last reading counts: heights must be positive and hashes 64 hex digits, each node's momentum at its own frontier height must be its frontier, each node's chain from just below the lowest frontier up to its own must link (unique hashes, each momentum naming as its parent the one the node serves one height below, the frontier reply included), at every height that is some node's frontier all nodes that have reached it must have one momentum (two tips above a common height are a fork), and a node asked twice about one height must answer the same. After that reading every node found at the start, the late joiner included, is stopped (each stop checked, its time recorded in `final.txt` as that node's cutoff), and only then are its output and whole log directory copied, so the logs run through shutdown. Rotated log segments are joined in order, and the joined log must hold exactly the starts the container's output (never rotated) records with `current time is …`, in order, each within 5 s, at least one more than the restarts docker counted, and the first must be the one pinned for that container ID: `up.sh` pins each core node at launch, and whoever starts the joiner pins it (`test/devnet/pin-start.sh`), which refuses a container that docker reports as restarted or as started more than 60 s after it was created. A log that begins at a later restart, or misses a session, stops the removal. It also writes every rollback of committed momentums (`rollbacking momentums`) in the logs it collected, with the time and the height it went back to, to `rollbacks.txt` and prints their count; that is a record of start-up and reorganisations, not a verdict, and belongs in the results. If a node's log cannot be copied, a node does not answer, a node contradicts itself or the five do not share a chain, it stops and removes nothing: a missing log must not read as zero errors. The late joiner is recorded as absent when it was not started; a joiner pinned on this chain that no longer exists is lost, and stops the removal. Then the chain is removed. Without a directory it only removes the chain.

Every kind of line in `error-histogram.txt` needs an explanation in the results file, and a kind that the previous baseline did not have needs one before anything else.

### P8. Record the result

One directory per run: `docs/test-results/<date>-<short commit>/`. It holds a `README.md` with the commit, the environment, each case id with pass or fail, every deviation from the procedure, and the evidence files. Then update `test/inventory/coverage.tsv` for the items the run exercised and run `test/inventory/check.sh`.

A failed case is recorded as failed, with the log lines that show it and an issue number once one exists. It is not re-run until it passes and then recorded as a pass.

### P9. End-to-end driver

The driver in `test/e2e/` signs and publishes account blocks and reads the result back over RPC. It is Go, built on the repository's own `wallet`, `chain/nom`, `pow`, `vm/abi` and `rpc/server` packages, Its cases (the `*_test.go` files) sit behind the `e2e` build tag, so `go test ./...` never starts them; `driver.go` itself has no tag, so `go build ./...` and `go vet ./...` compile and check it.

Start the devnet (P3), then:

```sh
E2E_GENESIS="$PWD/docker/devnet/genesis.json" E2E_NODES=pillar,pillar2,pillar3,rpc,observer E2E_COVERAGE="$PWD/run.tsv" \
  go test -tags e2e ./test/e2e/ -count=1 -v -timeout 60m > e2e.log 2>&1   # the exit status is the result
test/inventory/record.py run.tsv     # merge the passed cases into coverage.tsv
test/inventory/check.sh              # the new gap count
```

Redirect the output instead of piping it to `tee`: a pipe reports `tee`'s exit code, not the test's. `E2E_COVERAGE` must be an absolute path, because the test process runs in `test/e2e`; the driver refuses a relative one.

| Variable | Default | Meaning |
|---|---|---|
| `E2E_RPC` | `http://localhost:${DEVNET_HTTP_PORT:-35997},http://localhost:${DEVNET_PILLAR_HTTP_PORT:-35991}` | endpoints, comma separated. Blocks are published to the first; agreement checks read all of them |
| `E2E_CHAIN_ID` | `69` | chain identifier put into every block |
| `E2E_MIN_HEIGHT` | three past the last scheduled spork | the driver waits for this height before it starts, so the cases see one fixed spork state: 33 on the stock devnet, 3 where every spork is at height 0 |
| `E2E_GENESIS` | none, required | the genesis file the devnet was started on. Every endpoint must list exactly its sporks, and what the cases expect to be enforced comes from it, not from the node. The driver also takes the pillars from it: it publishes nothing until every endpoint and container has the same frontier, the history below it is one linked chain in which each genesis pillar has produced one of the last 60 momentums, and the next reading shows a higher frontier built on that momentum: two settled readings in a row, so any unsettled reading in between (a disagreement, a step back, to genesis included, a failed read) starts the observation over. Every read is bounded by the time left (up to 10 minutes in all). This is a precondition for the cases, not a verdict on start-up: what happened at start-up is in `rollbacks.txt`. A freshly started network can fork for about a minute, and a block published on the branch that loses is gone |
| `E2E_NODES` | unset | devnet node names, read through `docker exec`, so that the checks that say "every node" include the nodes with no published RPC port. The destructive case requires all five |
| `E2E_COVERAGE` | unset | file to write the coverage rows of passed cases to |

Before it runs a case, the driver waits until every node answers, and checks that all of them run the same version and that it was built with the same constants as they were. It takes about ten minutes. The groups run in parallel, each on its own fresh accounts, and every run derives new accounts and names from the start height, so the driver can be run again on the same chain. It uses the committed devnet mnemonic and must not be pointed at a network with real funds.

How to read a result:

- A case id is the part of the subtest name before the first underscore: `TestPlasma/PL-3_cancel_before_expiry_fails` is case `PL-3`. Ledger cases use the `B-` ids of TEST-PLAN.md section 5.2.
- A contract method can refuse in two places, and a case states which one it expects. **Refused at publish** means `ledger.publishRawTransaction` returned an error; the case requires that the block reached the node (an error while the driver prepared it does not count) and checks the message. **Failed at the contract** means the block was accepted and the contract's receive block carries status 2. What the node shows as the send must be, field for field, the block the driver signed; everything the driver then expects for it, a refund included, is worked out from the signed block. The case then checks that the receive is the called contract's block for that send, that a call without an amount produced no descendant block and left the caller's ZNN and QSR alone, and that the getters it watches answer exactly as before the call. A watched getter must read successfully, or, with `watchMissing`, fail with one named error: two failed reads are not evidence of unchanged state. The reason for the refusal is not on chain; which error the contract returned is for the VM unit tests to pin.
- A failed call that carried an amount must refund it: exactly one descendant block, a contract send from the called contract to the caller, of the same token and amount. Every failed call is checked for this and for the refund being a block of its own on every endpoint, and the cases that cover a refund also check the balance after receiving it.
- When the run ends, every block that any case had refused at publish must be unknown to every endpoint and every container, both to a lookup by hash (committed blocks) and in the account's unconfirmed blocks (the node's pool, read as one snapshot in a single page of 1,024, every hash returned inspected; a pool larger than that is not established and fails); the driver exits non-zero otherwise. `receiveAll` takes the exact number of sends a case expects. It receives only sends the driver knows the account is owed, each with the content the driver knows it has: the sends the driver itself signed, the blocks that contract receives it waited for sent in answer (followed through a second contract, as rewards are minted through the token contract), and payments a case announced in advance with `net.Expect` by payer, recipient, token, amount, the method of the call whose receive must make them and a group whose payments must all come from one and the same receive (an accelerator payout that a pillar's update triggers). It does not finish until every send the account is owed and has not yet received has arrived and been received, and every recorded contract-to-contract send has been followed, with the send, its receive and what that receive made reading the same on every endpoint, so another payment cannot stand in for the expected one; a recorded contract-to-contract send that no endpoint knows any more is an error. A send is reserved while its receive is published, so two calls cannot both receive it; a send listed twice in one page, or listed again after the driver's receive of it was accepted, fails: whether that is a replay or a receive lost in a rollback, the driver does not receive it again. It reads each send back by hash on every endpoint, waiting for an endpoint that does not have it yet until its deadline, counts before it publishes a receive, checks each confirmed receive against the one it signed and each confirmation against the momentum the node has at that height, which must list the block, and requires the mailbox to be a whole, empty page afterwards. A pending send of unknown origin is not received and fails the case. Every lookup of a block by hash fails if the node answers with a block of another hash.
- A case records the inventory items it exercised and the edge class, and only when it passes. That is where the rows in the coverage file come from; nobody types them.

One case is destructive and runs alone:

```sh
E2E_DESTRUCTIVE=1 E2E_GENESIS="$PWD/docker/devnet/genesis.json" E2E_NODES=pillar,pillar2,pillar3,rpc,observer \
  go test -tags e2e ./test/e2e/ -run TestHaltOnUnknownSpork -count=1 -v
```

It activates a spork that no binary implements, checks that the enforcement height is exactly the momentum the contract's receive acknowledges plus the delay, and then reads all five nodes three times over a minute and a half: each must be stopped one below that height, on the same momentum. It needs `E2E_NODES` set to all five. The chain cannot continue afterwards: run it last, collect the logs (P6), then wipe (P7). Never give a normal case the right to activate a spork.

To add a case: add a `t.Run("<id>_<what>", ...)` in the group it belongs to, use `callOK`, `callFail` or `callReject` for the outcome (give `callFail` a `watch(...)` for each getter the call must not change), read the state back with `get`, and name what it covers with `cover(t, "<class>", "<kind>", "<item>")`. A negative block that a wallet would never build is made with a `Mutator`, which changes the block before hashing, after the plasma fields are set, or after signing.

### P10. Genesis variants

The spork state of a chain is fixed by its genesis, so the same driver is run on several of them. A variant is the committed `docker/devnet/genesis.json` with a different spork list and nothing else changed: same accounts, pillars, balances and keys.

| Variant | Sporks enforced | What it stands for |
|---|---|---|
| `stock` | Accelerator and HTLC from 0, libp2p at 20, dynamic plasma at 30 | the committed devnet |
| `all-off` | none | the chain before its first spork |
| `mainnet-like` | Accelerator, HTLC, Bridge and Liquidity from 0 | what mainnet enforces today: legacy transport, no dynamic plasma |
| `all-on` | all eight; libp2p at 20 and dynamic plasma at 30, the rest from 0 | everything this binary implements |
| `staged` | as `all-on`, with PTLC at 60, EmbeddedRefund at 70, EpochRewardTerms at 80 | the new sporks arriving on a running network |

```sh
test/devnet/genesis.py --list                 # the variants and their heights
test/devnet/up.sh all-on                      # wipe, then start the five nodes on that genesis
E2E_GENESIS="$PWD/test/devnet/genesis/all-on.json" E2E_NODES=pillar,pillar2,pillar3,rpc,observer E2E_COVERAGE="$PWD/run.tsv" \
  go test -tags e2e ./test/e2e/ -count=1 -v -timeout 40m > e2e-all-on.log 2>&1   # the exit status is the result
test/devnet/down.sh logs-all-on               # copy the logs, count error lines, wipe
```

`up.sh` needs the image from P2 and starts from an empty chain every time. It selects the genesis through the compose overlay `test/devnet/compose.variant.yml`, which mounts the chosen file over the one in the image; no committed docker file changes. `genesis.py` writes the files to `test/devnet/genesis/`; check a new one with `go run ./cmd/devnet-keygen --verify-genesis <file>`.

The driver reads the spork list when it starts. It waits until three momentums after the last scheduled spork, runs only the groups whose contract is live, and for a contract that is not live it checks that every spork-gated method is refused. A case whose outcome depends on a spork says so in the case (`live["dynamic-plasma"]`). Record one results section per variant; a case that passes on one variant says nothing about another.

A variant cannot change what is compiled in. On every variant the bridge administrator is a fixed address whose key nobody on the devnet holds, an epoch is 24 hours, and stake, fusion, pillar and sentinel locks last hours to months. Cases that need those (bridge administration, rewards, expiry of long locks) run on the test build of P11.

### P11. Test build with fast constants

Some rules can only be reached after a day or more on a stock binary: an epoch is 24 hours, a stake lasts at least 30 days, a pillar is locked for 83 days. And the bridge administrator is a fixed address whose key nobody on the devnet holds. Those values are compiled in, so no genesis variant can change them.

The test build changes only those values. It is the same source with two extra files added at build time through a Go build overlay (`go build -overlay`); nothing in the node's source tree is edited. The files are `test/devnet/fast/constants.go.in` and `consensus.go.in`, and they are the complete list of differences:

| Value | Stock | Test build |
|---|---|---|
| Epoch | 8,640 momentums (24 hours) | 60 momentums (10 minutes) |
| Wait after an epoch before it is rewarded | 1 hour | 10 seconds |
| Minimum distance between contract updates | 300 momentums | 3 momentums |
| Fusion lock | 3,600 momentums | 12 momentums |
| Stake unit | 30 days | 1 minute |
| Pillar lock and revoke window | 83 days, 7 days | 2 minutes, 1 minute |
| Sentinel lock and revoke window | 27 days, 3 days | 2 minutes, 1 minute |
| Accelerator phase unit and voting period | 1 day, 14 days | 1 minute, 10 minutes |
| Bridge and Liquidity administrator | a fixed mainnet address | devnet account 9 |
| Bridge unhalt delay, administrator delay, soft delay | 6 hours, 2 epochs, 1 epoch | 6, 12 and 6 momentums |

The epoch cannot be shorter than 10 minutes. It has to be a whole number of election periods of 300 seconds, and with exactly one period per epoch a fresh node panics at its first momentum (the reason is written in `constants.go.in`).

```sh
FAST=1 test/devnet/build-image.sh             # image go-zenon-devnet-fast:latest; version ends in +fast-constants
test/devnet/up.sh fast                        # every spork from height 0, genesis time = now
test/devnet/fast/overlay.sh "$PWD" > build/fast-overlay.json
E2E_GENESIS="$PWD/test/devnet/genesis/fast.json" E2E_NODES=pillar,pillar2,pillar3,rpc,observer E2E_COVERAGE="$PWD/run.tsv" \
  go test -overlay build/fast-overlay.json -tags e2e ./test/e2e/ -count=1 -v -timeout 60m > e2e-fast.log 2>&1   # the exit status is the result
test/devnet/down.sh logs-fast
```

The driver has to be built with the same overlay, because it takes its expected values from the same constants. It compares its own constants with the node's version string when it starts and refuses to run if only one of the two is the test build. On the test build only the `TestFast` groups run; the normal groups are skipped, because their "before the window" cases assume the stock durations. Allow 30 minutes: the reward cases wait for the first epoch to end.

FPI-7 and FPI-8 pin the electorate of an accelerator tally (#274), on the pillar FPI-1 registers. With the dynamic plasma spork enforced, which the `fast` genesis does from height 0, only the votes of the pillars active at an update count, against the number of those pillars. Before the revoke (FPI-7), that pillar casts the only vote on a project of its own, and an update that executes above the frontier read after the vote must leave the project in voting: one vote of four active pillars is short of the 33 percent turnout. After the revoke (FPI-4), FPI-8 has an update execute above the frontier read after the revoke. The project must not be accepted; it stays in voting, or is closed if its window ended first. FPI-4 can wait up to twelve minutes before it revokes, so FPI-8 compares the pillar's revoke time with the end of the project's voting window (`creationTimestamp` plus `AcceleratorProjectVotingPeriod`). If the revoke came after the window ended, the final tally may have run before it, with one vote of four, and closed the project on any node; that run is inconclusive and the group is run again. Counted, the revoked pillar's stored vote would be one of three, enough to accept the project, which is what `dev` before #274 does. Both cases also require the stored breakdown to keep that one vote: the record of the poll is not rewritten by a revoke.

The `fast` genesis sets its genesis time to the moment it is generated. Epochs are counted from the genesis time, and the committed genesis is dated February 2025; on it the reward contracts would first have to work through every epoch since then, twenty per update.

What a pass on the test build means: the rule holds when its window is reached. Two values derived from the changed ones are not what a consistent short-epoch network would have, because they are computed before the overlay runs: the reward table per epoch keeps its stock amounts, so each momentum of the 60-momentum epoch carries 144 times the stock per-momentum reward, and `AcceleratorDuration` keeps its stock length. A reward collected on the test build shows that accrual and collection work and that nobody is paid more than the pool; it does not check the stock reward arithmetic. FS-2 and FPI-5 collect only after reading, from one frontier, that the contract has processed every finished epoch and that at least two minutes of the running epoch are left; the collect must then make one mint of exactly the credit, the token contract must pay exactly that to the account, the token contract's receipt of the mint must read the same on every endpoint and be confirmed in one and the same momentum; and if an epoch is processed around the collect after all, the public reads cannot tell which receive processed it, so the run is inconclusive and the case fails. FS-2 works out each epoch's stake reward to the unit as the contract does, from the two stakes' weights, themselves worked out from the amounts and durations signed (and required of the contract's entries, each signed stake listed exactly once and nothing else), and the times their receives acknowledge (start, and the last successful cancel), and the history must show exactly that. FS-2 waits until the contract has processed the epoch the stakes were made in. PL-1 requires one reply of `embedded.plasma.get` to show the fresh beneficiary with 50 QSR fused and exactly 50 × 2,100 plasma, current and maximum. FAZ-2 holds every Update call of the driver, executed or refused, to `CanPerformUpdate`: the last update that executed before its receipt is found by walking back from that receipt by previous-block hashes, and the receipt's outcome must be what that update allows. The driver checks, on every endpoint, the one compiled value that is visible from outside (the bridge administrator), and that all nodes report the same version with the test suffix; it cannot see the other values, which are pinned by the two overlay files. It does not show that the stock durations are what they should be, and the test binary is not the binary under test for anything else. A binary built with these files must never join a network with other binaries on it.

### P12. The whole baseline in one command, and testing a release candidate

`test/baseline.sh` runs P1, P2, P4, P5, P9 (normal and destructive), P10 and P11 one after another for the commit that is checked out, and leaves everything in one new directory. Use it for a release candidate and after every merge of `dev` into the test branch.

**1. Put the framework on the commit to test.** The framework is not on the release branches; it is merged onto the commit under test on a branch of its own, so the release branch is never touched. Pin every revision by its full hash and write the hashes down: the candidate, the framework, and the merge.

```sh
git fetch upstream
CANDIDATE=$(git rev-parse upstream/release/0.0.11-rc1)     # write both hashes down; from here on only the hashes are used
FRAMEWORK=e39b084e4ebf19d23a46aae79cbe8a94b943ee16       # a harness commit that has test/baseline.sh: the newest one a results page names; see below
git cat-file -e "$FRAMEWORK:test/baseline.sh" &&           # fails if that framework commit predates P12; nothing after it runs then
git switch -c testing/0.0.11-rc1 "$CANDIDATE" &&           # a new branch name for each candidate
git merge --no-ff "$FRAMEWORK" &&
git diff --exit-code "$CANDIDATE" HEAD -- . ':!test' ':!docs' && echo "node source is the candidate's" &&
git rev-parse HEAD                                         # the harness commit: the third hash to write down
```

The fork's `testing/framework` branch has not been updated since 2026-11-18 and has no `test/baseline.sh`; until it is, take as `FRAMEWORK` the harness commit of the newest results page (the `git cat-file -e` line stops the commands otherwise). Do not append `; echo "exit $?"` to these commands in automation: it prints the status but makes the line succeed. If a teardown fails after it stopped the nodes, the containers stay stopped with their volumes: inspect them, copy what is needed, then `docker compose --profile sync down -v`.

To repeat a published result, do not resolve branch names at all: set `CANDIDATE` and `FRAMEWORK` to the hashes its results page gives, or check out the harness commit it names, which already is the merge. For the results of 2026-11-22 the candidate is `203ccc64b17a2facd054303b4cecffc82bad3049`; the ordinary run that passed every stage (attempt 7) was made at harness commit `e39b084e4ebf19d23a46aae79cbe8a94b943ee16` and the `QUALIFY=1` run (attempt 5) at `d9a5f894d5316aed51d5f3f59159cb9a6f70c943`. The other attempts are in the table of runs on that page.

The `git diff --exit-code` line is the proof that the node being tested is the candidate: the merge may only change files under `test/` and `docs/`. The `git rev-parse HEAD` line records the revision the run is made at. The version string of the image names the merge commit, not the candidate's, so record both. The baseline refuses to run on a tree with uncommitted changes, because a local edit would enter the binary unseen; `ALLOW_DIRTY=1` overrides that and saves the diff with the run.

**2. Bring the gap tracker up to the new source.**

```sh
test/inventory/check.sh --sync      # prints ADDED and DROPPED, then rewrites the map
```

Every added line is an item with no case yet. Write them into the results, and commit the map.

**3. Run it.** On a machine that will not be suspended until it finishes, and whose docker VM has not been through a suspend since it started (P4 says why).

```sh
test/baseline.sh build/baseline/0.0.11-rc1-attempt1        # about three hours, unattended; its exit status is the result
# or, for a candidate, with -race, -shuffle, libznn and 30 s per fuzz target as well (a new directory again):
QUALIFY=1 test/baseline.sh build/baseline/0.0.11-rc1-attempt2
```

The directory must be new: every attempt gets its own, and an attempt that was interrupted is kept, not overwritten. The script also refuses to start while any `.go` file that git ignores lies inside the repository, wherever it is: `go build ./...` and the other stages would take in some of them, and the rule is kept simple rather than exact. Keep scratch files outside the repository. The script clears the environment variables that could change what is built or run, records the toolchain and the state of the node machine's clock, and then runs the stages. **Its exit code is the verdict: 0 only if every required stage ran and gave its required result.** A failed stage does not stop the independent stages after it, but the stages that depend on it are recorded as not run, and that is a failure too.

| File in the output directory | What it is |
|---|---|
| `status.tsv` | one line per stage: `ok` or `FAIL`, the stage, the detail |
| `environment.txt`, `dirty.txt`, `dirty.diff` | commits, toolchain, docker, clock; uncommitted changes if any were allowed |
| `build.out`, `unit.out`, `vet.txt`, `genesis*.out` | P1. With `QUALIFY=1` also `race.out`, `shuffle.out` (with the seeds), `libznn.out`, `fuzz.out` |
| `image.out`, `image-fast.out` | P2 and P11 builds, with the binary's SHA-256 and version |
| `manifest-<variant>.txt` | P3: image id, binary and genesis digests, genesis momentum and version of every node of that chain |
| `clock-samples.tsv`, `clock-run.out`, `stage-times.tsv` | the clock readings during the devnet stages, their verdict, and the time each stage ended. `clock-run` fails if the running median of five moved more than 30 ms, if a single reading is more than 30 ms off the median around it, or if the clock was not watched throughout (sampler not running at the end, first reading late, last reading old, readings more than 45 s apart); it names the stages a movement fell in |
| `go-env.txt`, `compose-config-<variant>.txt`, `compose-config.out`, `stages-*.txt`, `cases-*.diff` | the Go configuration in effect and the compose configuration as rendered for each variant (the `compose-config` stage fails if one does not render); the stage and case reconciliation |
| `fuzz-list.out`, `fuzz-targets.diff` | with `QUALIFY=1`: `fuzz-list.out` is the JSON event stream of `go test -json -list '^Fuzz' ./...`; from it, the fuzz targets the test binaries compile, by the package Go names for each and their name (any Go identifier that starts with `Fuzz`), which must be exactly `test/known-fuzz.txt`; each must then show its `execs` count above zero in `fuzz.out` |
| `logs-<variant>/rollbacks.txt` | every rollback of committed momentums in the collected logs of that chain's nodes |
| `smoke.json`, `smoke-joiner-*.json`, `cov-smoke.tsv` | P4 and P5 |
| `e2e-<variant>.out`, `cov-<variant>.tsv`, `skips-<variant>.txt` | the complete driver output, the coverage rows of the cases that passed, the groups that were skipped |
| `logs-<variant>/` | P6: every node's log and output, `final.txt` (each node's last frontier and the momentum all five share at the lowest of them), `error-lines.txt`, `error-histogram.txt` |

Two stages have something other than "exit 0" as their required result, written down so that a change is noticed: `vet` must print exactly the lines of `test/known-vet.txt`, and `joiner-105` must show the signature of issue #105. The `clock` stage measures the node machine's wall clock against its uptime before any devnet is started; if the wall clock moved by more than 30 ms, in three tries, no devnet stage is run and all of them count as failed. From then on a container takes one reading every 20 seconds until the last devnet stage is over; the readings are kept in `clock-samples.tsv`, and the `clock-run` stage fails if their running median moved by more than 30 ms. A reading is good to about 10 ms, and a step and its reversal inside one 20-second interval are not seen. The smoke stage runs with `--strict-cadence`. The two late-joiner stages judge only the joiner.

A driver stage passes only if the test exits 0, no case failed, no case skipped itself, the groups that were skipped are exactly the ones that variant must skip, and **the cases that passed are exactly those listed in `test/e2e/cases/<stage>.txt`**: a case that silently stopped running fails the stage. When a case is added, removed or renamed, that file changes in the same commit. In the same way the fuzz stage runs exactly the targets of `test/known-fuzz.txt`, and at the end the recorded stages must be exactly the required ones, each once (`stages.diff`).

If the evidence of a chain cannot be collected (`down-*` fails), that chain's containers and volumes are kept, some of them possibly already stopped by the teardown, no further devnet stage is run, and the last lines of the output say so: check each container's state with `docker compose --profile sync ps -a` before removing them with `docker compose --profile sync down -v`. The same holds when the script is interrupted: it stops its clock container, leaves the devnet as it is and says how to remove it.

One case depends on the calendar. A pillar is revocable for 7 days in every 90, counted from its registration time, and the genesis pillars of every variant are registered at time 0, so the cycle is counted from 1 January 1970: the window is open from 26 November to 3 December 2026, from 24 February to 3 March 2027, and so on every 90 days. On every date PI-9 holds the node's `isRevocable` and `revokeCooldown` for `dev3` against that calendar. The refused revoke itself is tried only while the pillar is locked; inside the window the case passes without it, the coverage mark `pillars.Revoke` E12 is not earned on the four normal variants, and the refusal is exercised on the test build only (FPI-4).

**4. Read it.** `status.tsv` first. Then compare with the previous baseline, variant by variant, and never across variants:

```bash
# cases <file> <list>: the file's case results, one per line, into <list>;
# fails on a file that is missing or unreadable, or that holds no case.
cases() {
  [ -r "$1" ] || { echo "cannot read $1" >&2; return 1; }
  grep -E '^[[:space:]]+--- ' "$1" > "$2.raw" || { echo "no case results in $1" >&2; return 1; }
  sed -E 's/ \([0-9.]+s\)//; s/^ +//' "$2.raw" > "$2.unsorted" && sort "$2.unsorted" > "$2" && [ -s "$2" ]
}
differ=0; T=$(mktemp -d)
for v in stock all-on mainnet-like all-off fast destructive; do
  echo "== $v"
  if cases old/e2e-$v.out "$T/old-$v" && cases new/e2e-$v.out "$T/new-$v"; then
    diff "$T/old-$v" "$T/new-$v" || differ=1
  else
    echo "$v: the comparison is not established"; differ=1
  fi
done
[ $differ -eq 0 ]   # non-zero if any variant differs or either side is missing
```

A case that appears, disappears or changes result is a difference to explain. A case whose name is unchanged can still have changed: if the driver changed between the two baselines, the two runs did not test the same thing, and the only clean comparison is to run the same driver commit against both binaries. Compare `error-histogram.txt` the same way, variant by variant.

**5. Record it.** P8, in `docs/test-results/<date>-<short commit>/`: `status.tsv`, `environment.txt`, the manifests, the smoke files, the per-case lines of each `e2e-*.out`, the coverage files, and for each variant `final.txt`, `error-lines.txt` and `error-histogram.txt`. Keep the complete output directory somewhere outside the repository as well; it is too large to commit. Merge the coverage with `test/inventory/record.py build/baseline/<attempt>/cov-*.tsv`; for a complete run, `--replace` rebuilds the map from it, so tokens that no case records any more are dropped. If there was more than one attempt, say so and say which one the tables are from.

A run of the baseline says that the cases it contains gave their required results on this commit. It says nothing about what it does not contain; `test/inventory/check.sh --list` names that.

## 4. Procedures still to be written

These need tooling that does not exist yet (TEST-PLAN.md section 8). Each gets a P-number here when its tool lands.

| Procedure | Needs |
|---|---|
| Bridge wrap, unwrap and redeem; TSS key change; guardians and the emergency path; Liquidity administration and liquidity staking; sentinel and liquidity rewards | driver cases on the test build (the build exists, P11). Wrap and unwrap need the driver to sign as the orchestrator with a test secp256k1 key |
| Before, at and after each new spork | driver cases written for the `staged` variant (the variant exists, P10) |
| Sync and network fault cases | fault peer; chaos script |
| RPC limits and subscriptions | load client for HTTP and WS |
| Mixed-version network | v0.0.10 image with devnet constants |
| Mainnet sync and state comparison | a host; RPC differ |

The smoke test is read-only. Everything that sends a transaction goes through the driver (P9).

## 5. Repeating our first run

The first run was on `dev` at `924e6b8`, on 2026-11-18. To repeat it: P2 with the script, P3 without the explorer, P4 at height 45, P5 started after height 45, P6, P7 with wipe. Then on a fresh devnet: P3, P9 normal run, P9 destructive run, P6, P7 with wipe. Then P10 for `all-on`, `mainnet-like` and `all-off`, and P11.
