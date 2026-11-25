#!/usr/bin/env python3
"""Read-only smoke test for the docker devnet (docker-compose.yml at the repo root).

What it checks, against the genesis file the devnet was started on:

- every one of the five nodes has the same momentum at every height up to the
  frontier minus two, and the range has no hole;
- every momentum has a well-formed hash that no other height has, and points to
  the hash of the one before;
- every momentum was produced by one of the genesis pillars' producer addresses,
  all of them produced, and no slot is empty, other than in the first --settle
  seconds after the first produced momentum or
  through the known issue in which a pillar logs dropping its slot as early
  (those are counted and reported; --strict-cadence fails them too);
- every spork of the genesis is listed with its id, its activation flag and its
  enforcement height, and momentums are version 2 exactly from the height after
  dynamic-plasma enforcement;
- every node reports sync done with at least two peers;
- one getter per RPC namespace answers with a well-formed JSON-RPC reply whose
  result has the expected shape and content;
- with --sync, the late joiner (host port --sync-port) is synced and on the same chain, and
  its frontier is what it serves at that height and what the RPC node has there;
  with --expect-105, that it shows exactly the signature of issue #105 instead.

Standard library only. Exit 0 when every check passes, 1 when one fails, 2 when the
network could not be examined at all (no answer, or it stopped producing).

  python3 test/devnet/smoke.py --height 45 --out smoke.json --cov smoke.tsv
  python3 test/devnet/smoke.py --genesis test/devnet/genesis/all-on.json
"""
import argparse, json, os, subprocess, sys, time, urllib.request

ROOT = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
NODES = ["pillar", "pillar2", "pillar3", "rpc", "observer"]
DEV = "z1qp3yph55qgresyytz83anynr2f4z39x2z3ej3e"  # devnet account, index 3
ZNN, QSR = "zts1znnxxxxxxxxxxxxx9z4ulx", "zts1qsrxxxxxxxxxxxxxmrhjll"
PAGE = 500  # ledger.getMomentumsByHeight returns at most 1024


class Unreachable(Exception):
    pass


def envelope(reply, what):
    """A JSON-RPC 2.0 success reply with a result that is not null."""
    if not isinstance(reply, dict) or reply.get("jsonrpc") != "2.0" or reply.get("id") != 1:
        raise ValueError("%s: not a JSON-RPC 2.0 reply to request 1: %s" % (what, json.dumps(reply)[:200]))
    if reply.get("error") is not None:
        raise ValueError("%s: error %s" % (what, json.dumps(reply["error"])[:200]))
    if reply.get("result") is None:
        raise ValueError("%s: no result" % what)
    return reply["result"]


# Host ports of the devnet's HTTP RPC, set from --port and --sync-port.
PORT, SYNC_PORT = 35997, 35999


def host(method, params=None, port=None):
    port = port or PORT
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params or []}).encode()
    req = urllib.request.Request("http://localhost:%d" % port, body, {"Content-Type": "application/json"})
    try:
        return json.load(urllib.request.urlopen(req, timeout=10))
    except Exception as e:  # refused, timed out, not JSON
        raise Unreachable("port %d %s: %s" % (port, method, e))


def inside(node, method, params=None):
    """Call a node that publishes no host port, through docker exec."""
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params or []})
    try:
        out = subprocess.run(
            ["docker", "exec", "znnd-devnet-%s" % node, "wget", "-qO-", "-T", "10", "--post-data", body,
             "--header", "Content-Type: application/json", "http://localhost:35997"],
            capture_output=True, text=True, timeout=20)
        return json.loads(out.stdout)
    except Exception as e:
        raise Unreachable("node %s %s: %s" % (node, method, e))


def momentums(call, first, last):
    """Every momentum from first to last, by pages; fails on a hole or a repeat."""
    got = []
    h = first
    while h <= last:
        n = min(PAGE, last - h + 1)
        page = envelope(call("ledger.getMomentumsByHeight", [h, n]), "momentums %d+%d" % (h, n)).get("list")
        if not isinstance(page, list) or len(page) != n:
            raise ValueError("asked for %d momentums from height %d, got %s" % (n, h, len(page) if isinstance(page, list) else page))
        got += page
        h += n
    if not all(isinstance(m, dict) for m in got):
        raise ValueError("a momentum in the reply is not an object")
    heights = [m.get("height") for m in got]
    if heights != list(range(first, last + 1)):
        raise ValueError("heights are not %d..%d without a hole: %s..." % (first, last, heights[:12]))
    for i, m in enumerate(got):
        if not is_hash(m.get("hash")) or not is_hash(m.get("previousHash"), zero_ok=m["height"] == 1):
            raise ValueError("momentum %d has no well-formed hash or previous hash: %s" % (m["height"], json.dumps(m)[:160]))
        if i and m["previousHash"] != got[i - 1]["hash"]:
            raise ValueError("momentum %d does not point to the hash of momentum %d" % (m["height"], m["height"] - 1))
        if type(m.get("timestamp")) is not int or type(m.get("version")) is not int or not is_address(m.get("producer")):
            raise ValueError("momentum %d has an ill-typed timestamp, version or producer" % m["height"])
    hashes = [m["hash"] for m in got]
    if len(set(hashes)) != len(hashes):
        twice = next(m["height"] for i, m in enumerate(got) if m["hash"] in hashes[:i])
        raise ValueError("momentum %d has the hash of a lower momentum; hashes must be unique" % twice)
    return got


def is_hash(v, zero_ok=False):
    return isinstance(v, str) and len(v) == 64 and all(c in "0123456789abcdef" for c in v) and (zero_ok or v != "0" * 64)


def is_address(v):
    return isinstance(v, str) and len(v) == 40 and v.startswith("z1")


def is_count(v):
    return type(v) is int and v >= 0


def is_amount(v):
    return isinstance(v, str) and v.isdigit()


def has(keys):
    return lambda r: isinstance(r, dict) and all(k in r for k in keys)


def paged(size, minimum=0, each=None):
    """A first page of the size asked for: count is the total, and the list holds
    exactly min(count, size) objects. A count with an empty list is not a page."""
    def ok(r):
        if not isinstance(r, dict) or not isinstance(r.get("list"), list) or not is_count(r.get("count")):
            return False
        if r["count"] < minimum or len(r["list"]) != min(r["count"], size):
            return False
        return all(isinstance(x, dict) and (each is None or each(x)) for x in r["list"])
    return ok


def getters(genesis):
    pillars = sorted(p["Name"] for p in genesis["PillarConfig"]["Pillars"])
    sporks = len((genesis.get("SporkConfig") or {}).get("Sporks") or [])
    return [
        ("embedded.pillar.getAll", [0, 10],
         lambda r: paged(10, len(pillars))(r) and sorted(p.get("name") for p in r["list"]) == pillars),
        ("embedded.pillar.getQsrRegistrationCost", [], lambda r: is_amount(r) and int(r) > 0),
        ("embedded.plasma.get", [DEV], lambda r: has(["currentPlasma", "maxPlasma", "qsrAmount"])(r) and is_amount(r["qsrAmount"]) and is_count(r["currentPlasma"]) and is_count(r["maxPlasma"])),
        ("embedded.plasma.getVariables", [],
         # This reply's field names are capitalised, unlike every other getter's.
         lambda r: has(["MaxBasePlasmaInMomentum", "FusedPlasmaTarget", "PowPlasmaTarget", "MaxPriceChangePercent", "PriceChangeDenominator"])(r)
         and all(type(v) is int and v > 0 for v in r.values())),
        ("embedded.token.getAll", [0, 10],
         lambda r: paged(10, 2)(r) and {ZNN, QSR} <= {t.get("tokenStandard") for t in r["list"]}),
        ("embedded.sentinel.getAllActive", [0, 10], paged(10)),
        ("embedded.stake.getEntriesByAddress", [DEV, 0, 10], lambda r: paged(10)(r) and is_amount(r.get("totalAmount"))),
        ("embedded.accelerator.getAll", [0, 10], paged(10)),
        ("embedded.swap.getLegacyPillars", [], lambda r: isinstance(r, list) and all(isinstance(x, dict) for x in r)),
        ("embedded.spork.getAll", [0, 20], lambda r: paged(20, sporks)(r) and r["count"] == sporks and len({s.get("id") for s in r["list"]}) == sporks),
        ("embedded.bridge.getBridgeInfo", [], lambda r: has(["administrator", "halted", "allowKeyGen"])(r) and is_address(r["administrator"]) and type(r["halted"]) is bool and type(r["allowKeyGen"]) is bool),
        ("embedded.liquidity.getLiquidityInfo", [], lambda r: has(["administrator", "isHalted", "tokenTuples"])(r) and is_address(r["administrator"]) and type(r["isHalted"]) is bool
         and isinstance(r["tokenTuples"], list) and is_amount(r.get("znnReward")) and is_amount(r.get("qsrReward"))),
        ("ledger.getAccountInfoByAddress", [DEV],
         lambda r: has(["address", "accountHeight", "balanceInfoMap"])(r) and r["address"] == DEV and ZNN in r["balanceInfoMap"]
         and is_amount(r["balanceInfoMap"][ZNN].get("balance")) and int(r["balanceInfoMap"][ZNN]["balance"]) > 0),
        ("ledger.getDetailedMomentumsByHeight", [1, 5],
         lambda r: paged(5, 5)(r) and [d.get("momentum", {}).get("height") for d in r["list"]] == [1, 2, 3, 4, 5]),
        ("ledger.getMomentumsByPage", [0, 5], lambda r: paged(5, 5)(r) and all(is_hash(m.get("hash")) for m in r["list"]) and len({m["hash"] for m in r["list"]}) == 5),
        ("stats.processInfo", [], lambda r: has(["version", "commit"])(r) and bool(r["version"]) and len(str(r["commit"])) == 40),
        ("stats.osInfo", [], lambda r: has(["os", "platform", "numCPU", "numGoroutine"])(r) and type(r["numCPU"]) is int and r["numCPU"] > 0),
    ]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--height", type=int, default=45, help="wait for this frontier height first")
    ap.add_argument("--timeout", type=int, default=900, help="seconds to wait for --height")
    ap.add_argument("--settle", type=int, default=100, help="seconds after the first produced momentum that count as start-up: an empty slot there is reported, not failed")
    ap.add_argument("--strict-cadence", action="store_true", help="fail on every empty slot after start-up, also one a pillar logged dropping as early")
    ap.add_argument("--joiner-only", action="store_true", help="with --sync or --expect-105: judge only the late joiner")
    ap.add_argument("--genesis", default=os.path.join(ROOT, "docker", "devnet", "genesis.json"),
                    help="the genesis file the devnet was started on")
    ap.add_argument("--sync", action="store_true", help="also require the late joiner (host port --sync-port) to be synced")
    ap.add_argument("--expect-105", action="store_true",
                    help="require the late joiner to show the signature of issue #105: height 1, no peers")
    ap.add_argument("--port", type=int, default=int(os.environ.get("DEVNET_HTTP_PORT", "35997")),
                    help="host port of the rpc node's HTTP RPC (DEVNET_HTTP_PORT, default 35997)")
    ap.add_argument("--sync-port", type=int, default=int(os.environ.get("DEVNET_SYNC_HTTP_PORT", "35999")),
                    help="host port of the late joiner's HTTP RPC (DEVNET_SYNC_HTTP_PORT, default 35999)")
    ap.add_argument("--out", help="write results as JSON to this file")
    ap.add_argument("--cov", help="write coverage rows for the getters that passed to this file")
    args = ap.parse_args()
    global PORT, SYNC_PORT
    PORT, SYNC_PORT = args.port, args.sync_port
    genesis = json.load(open(args.genesis))
    results = []

    def check(case, ok, detail):
        results.append({"case": case, "pass": bool(ok), "detail": detail})
        print("PASS" if ok else "FAIL", case, str(detail)[:300])

    def attempt(case, fn):
        """A check whose own failure to read the network is a failed check."""
        try:
            ok, detail = fn()
        except (ValueError, Unreachable, KeyError, TypeError, IndexError) as e:
            ok, detail = False, "%s: %s" % (type(e).__name__, e)
        check(case, ok, detail)

    # Wait for the height, with a limit, and notice a frontier that stopped.
    deadline, last, moved = time.time() + args.timeout, None, time.time()
    while True:
        try:
            h = envelope(host("ledger.getFrontierMomentum"), "frontier")["height"]
        except Unreachable as e:  # not up yet: keep waiting
            h = None
            err = e
        except (ValueError, KeyError, TypeError) as e:  # it answered, and the answer is wrong
            print("smoke: the node answers ledger.getFrontierMomentum with something that is not a frontier: %s" % e)
            sys.exit(2)
        if h is not None:
            if h != last:
                last, moved = h, time.time()
            if h >= args.height:
                break
            if time.time() - moved > 90:
                print("smoke: the frontier has been at height %d for 90 s; the network is not producing" % h)
                sys.exit(2)
        if time.time() > deadline:
            print("smoke: height %d not reached in %d s (last: %s)" % (args.height, args.timeout, h if h is not None else err))
            sys.exit(2)
        time.sleep(5)
    top = h - 2  # two below the frontier: every node should have it

    # One chain, at every height, on every node.
    chains = {}

    def one_chain():
        for n in NODES:
            chains[n] = momentums(lambda m, p, n=n: inside(n, m, p), 1, top)
        ref = [m["hash"] for m in chains["rpc"]]
        diff = {n: next(i + 1 for i, m in enumerate(c) if m["hash"] != ref[i]) for n, c in chains.items()
                if [m["hash"] for m in c] != ref}
        return not diff, "heights 1..%d on %d nodes; first differing height per node: %s" % (top, len(chains), diff or "none")
    if args.joiner_only:
        if not (args.sync or args.expect_105):
            sys.exit("--joiner-only needs --sync or --expect-105")
        attempt("reference-chain", lambda: (chains.setdefault("rpc", momentums(host, 1, top)) is not None, "heights 1..%d read from the RPC node" % top))
    else:
        attempt("one-chain", one_chain)
    ms = chains.get("rpc") or []

    expected = sorted(p["BlockProducingAddress"] for p in genesis["PillarConfig"]["Pillars"])
    network_checks = not args.joiner_only

    def producers():
        seen = {}
        for m in ms[1:]:  # height 1 is the genesis momentum
            seen[m["producer"]] = seen.get(m["producer"], 0) + 1
        return sorted(seen) == expected, "expected %s, produced: %s" % (expected, json.dumps(seen))
    if network_checks:
        attempt("genesis-pillars-produce", producers)

    missed = []

    def dropped_early(slot):
        """The pillar that logged dropping this slot as "before start time", if one did."""
        stamp = time.strftime("%Y-%m-%d %H:%M:%S", time.gmtime(slot))
        for n in ("pillar", "pillar2", "pillar3"):
            out = subprocess.run(["docker", "exec", "znnd-devnet-%s" % n, "grep", "-c",
                                  "StartTime:%s +0000 UTC.*current time is before start time" % stamp, "/root/.znn/log/zenon.log"],
                                 capture_output=True, text=True, timeout=20)
            if out.stdout.strip() not in ("", "0"):
                return n
        return None

    def cadence():
        # A gap of 20 s is one empty slot. Two things are known to leave a slot
        # empty and are reported, not failed: the first --settle seconds, while
        # the pillars are still finding each other; and the known node issue in
        # which a pillar is woken for its slot, finds its wall clock a moment
        # behind the slot's start, logs "current time is before start time" and
        # is not woken again. The second is accepted only with that log line
        # for exactly that slot. Any other empty slot fails the check.
        body = ms[1:]
        # Start-up is a span of time, not of heights: a network that produces
        # nothing for ten minutes after its second momentum is not starting up.
        settled = body[0]["timestamp"] + args.settle
        gaps = [(a["height"], a["timestamp"], b["timestamp"] - a["timestamp"]) for a, b in zip(body, body[1:])]
        unexplained = []
        for height, ts, gap in gaps:
            if gap == 10:
                continue
            if gap < 10 or gap % 10:
                unexplained.append("after %d: %d s" % (height, gap))
                continue
            for slot in range(ts + 10, ts + gap, 10):
                by = "start-up" if slot < settled else dropped_early(slot)
                missed.append({"after-height": height, "slot": time.strftime("%H:%M:%S", time.gmtime(slot)), "dropped-by": by})
                if by is None or (args.strict_cadence and by != "start-up"):
                    unexplained.append("after %d: slot %s empty%s" % (height, missed[-1]["slot"], "" if by is None else " (dropped early by %s)" % by))
        # Accepting the known issue has a limit: when more than one judged slot
        # in ten is empty, the machine's clock is unfit and the run says nothing.
        early = [m for m in missed if m["dropped-by"] not in (None, "start-up")]
        judged = max(1, (body[-1]["timestamp"] - settled) // 10)   # slots of time, produced or not
        if len(early) * 10 > judged:
            unexplained.append("%d of %d slots after start-up were dropped as early: the clock of the machine the nodes run on is being stepped; fix it and run again" % (len(early), judged))
        return len(gaps) == top - 2 and not unexplained, "%d intervals from height 2 to %d; %d empty slots: %s; not accepted: %s" % (
            len(gaps), top, len(missed), ["%s after %d (%s)" % (m["slot"], m["after-height"], m["dropped-by"]) for m in missed], unexplained or "none")
    if network_checks:
        attempt("cadence-10s", cadence)

    wanted = {s["id"]: s for s in (genesis.get("SporkConfig") or {}).get("Sporks") or []}
    listed = {}

    def sporks():
        page = envelope(host("embedded.spork.getAll", [0, 50]), "sporks")
        if not paged(50)(page):
            raise ValueError("the spork list is not a page: %s" % json.dumps(page)[:200])
        for s in page["list"]:
            if s["id"] in listed:
                raise ValueError("spork %s is listed twice" % s["id"])
            listed[s["id"]] = s
        wrong = [w["name"] for i, w in wanted.items()
                 if i not in listed or listed[i]["activated"] != w["activated"] or listed[i]["enforcementHeight"] != w["enforcementHeight"]]
        extra = [s["name"] for i, s in listed.items() if i not in wanted]
        return not wrong and not extra, "%d sporks in genesis; differing from it: %s; not in genesis: %s" % (len(wanted), wrong, extra)
    if network_checks:
        attempt("sporks-as-in-genesis", sporks)

    def versions():
        dp = next((s for s in wanted.values() if s["id"].endswith("0" * 62 + "02") and s["activated"]), None)
        bad = [m["height"] for m in ms[1:] if m["version"] != (2 if dp and m["height"] > dp["enforcementHeight"] else 1)]
        return not bad, "dynamic plasma enforced at %s; momentums with the wrong version: %s" % (dp["enforcementHeight"] if dp else "never", bad[:10])
    if network_checks:
        attempt("momentum-version", versions)

    # Methods whose reply passed a check, for --cov: the getters below, and
    # the two sync readers once a node's sync check passed.
    passed_getters = []
    for n in NODES if network_checks else []:
        def synced(n=n):
            si = envelope(inside(n, "stats.syncInfo"), "syncInfo")
            ni = envelope(inside(n, "stats.networkInfo"), "networkInfo")
            ok = si["state"] == 2 and ni["numPeers"] >= 2
            if ok and "stats.syncInfo" not in passed_getters:
                passed_getters.extend(["stats.syncInfo", "stats.networkInfo"])
            return ok, "%s peers=%s" % (si, ni["numPeers"])
        attempt("sync-" + n, synced)

    for method, params, valid in getters(genesis) if network_checks else []:
        def getter(method=method, params=params, valid=valid):
            r = envelope(host(method, params), method)
            ok = bool(valid(r))
            if ok:
                passed_getters.append(method)
            return ok, json.dumps(r)[:200]
        attempt("rpc:" + method, getter)

    if args.sync or args.expect_105:
        state = {}

        def read_joiner():
            state["si"] = envelope(host("stats.syncInfo", port=SYNC_PORT), "joiner syncInfo")
            state["peers"] = envelope(host("stats.networkInfo", port=SYNC_PORT), "joiner networkInfo")["numPeers"]
            state["frontier"] = envelope(host("ledger.getFrontierMomentum", port=SYNC_PORT), "joiner frontier")
            state["first"] = momentums(lambda m, p: host(m, p, port=SYNC_PORT), 1, 1)[0]
            if type(state["peers"]) is not int or type(state["frontier"].get("height")) is not int or not is_hash(state["frontier"].get("hash")):
                raise ValueError("ill-typed peers or frontier")
            if not all(type(state["si"].get(k)) is int for k in ("state", "currentHeight", "targetHeight")):
                raise ValueError("ill-typed syncInfo %s" % state["si"])
            # What the joiner calls its frontier must be what it serves at that
            # height, and the frontier reply's parent what it serves one below.
            fh = state["frontier"]["height"]
            if fh < 1:
                raise ValueError("the joiner reports frontier height %d" % fh)
            own = momentums(lambda m, p: host(m, p, port=SYNC_PORT), max(1, fh - 1), fh)
            state["own"] = own[-1]["hash"]
            if fh > 1 and state["frontier"].get("previousHash") != own[0]["hash"]:
                raise ValueError("the joiner reports frontier %s at height %d with parent %s, but serves %s at height %d" % (
                    state["frontier"]["hash"], fh, state["frontier"].get("previousHash"), own[0]["hash"], fh - 1))
            if state["own"] != state["frontier"]["hash"]:
                # One more look: the frontier may have moved between the two reads.
                again = envelope(host("ledger.getFrontierMomentum", port=SYNC_PORT), "joiner frontier")
                if again.get("height") == fh and again.get("hash") == state["frontier"]["hash"]:
                    raise ValueError("the joiner reports frontier %s at height %d and serves %s at that height" % (state["frontier"]["hash"], fh, state["own"]))
                raise ValueError("the joiner's frontier moved while it was read; read again")
            return True, "%s peers=%s height=%s" % (state["si"], state["peers"], state["frontier"]["height"])
        attempt("late-joiner-answers", read_joiner)
        if args.expect_105:
            # Issue #105: the joiner starts on the legacy transport after the others have
            # switched, finds nobody and stays on its genesis momentum. Only that counts.
            attempt("late-joiner-issue-105", lambda: (
                state["peers"] == 0 and state["frontier"]["height"] == 1 and state["si"]["state"] == 3
                and state["frontier"]["hash"] == ms[0]["hash"] == state["first"]["hash"],
                "want 0 peers, height 1 on the network's genesis momentum, state 3 (not enough peers); got peers=%s height=%s state=%s genesis %s" % (
                    state["peers"], state["frontier"]["height"], state["si"]["state"], "same" if state["frontier"]["hash"] == ms[0]["hash"] else "DIFFERENT")))
        else:
            def synced_joiner():
                fh = state["frontier"]["height"]
                # The reference node's momentum at the joiner's frontier height: the
                # joiner's frontier must be that one, not only its history below it.
                ref = momentums(host, fh, fh)[0]["hash"]
                return (state["si"]["state"] == 2 and state["peers"] >= 1 and fh >= top
                        and state["si"]["currentHeight"] >= top and state["si"]["targetHeight"] >= top
                        and state["frontier"]["hash"] == ref == state["own"],
                        "want state 2, a peer, frontier, current and target height at least %d, and the frontier the RPC node has at that height; got %s peers=%s frontier=%s, %s" % (
                            top, state["si"], state["peers"], fh, "same as the RPC node" if state["frontier"]["hash"] == ref else "NOT the RPC node's momentum at that height"))
            attempt("late-joiner-synced", synced_joiner)

            def same_chain():
                # One linked range from genesis through the joiner's own frontier,
                # so its tip is tied to the history below it, and the same range
                # from the RPC node. momentums() checks every parent link.
                fh = state["frontier"]["height"]
                mine = momentums(lambda m, p: host(m, p, port=SYNC_PORT), 1, fh)
                ref = momentums(host, 1, fh)
                return ([m["hash"] for m in mine] == [m["hash"] for m in ref] and mine[-1]["hash"] == state["frontier"]["hash"]
                        and [m["hash"] for m in ref[:len(ms)]] == [m["hash"] for m in ms][:fh],
                        "heights 1..%d, the joiner's frontier, as one linked chain compared with the RPC node" % fh)
            attempt("late-joiner-same-chain", same_chain)

    failed = [r for r in results if not r["pass"]]
    try:
        version = envelope(host("stats.processInfo"), "processInfo")
    except (Unreachable, ValueError) as e:
        version = {"version": "unknown (%s)" % e}
    print("\n%d/%d passed at height %d on %s" % (len(results) - len(failed), len(results), h, version["version"]))
    if args.out:
        json.dump({"version": version, "height": h, "compared-to-height": top, "genesis": os.path.relpath(args.genesis, ROOT),
                   "empty-slots": missed, "results": results}, open(args.out, "w"), indent=1)
    if args.cov:
        # The same rows the driver writes: kind, item, class:case. Only getters whose reply passed its shape check.
        types = {"ledger": "LedgerApi", "stats": "StatsApi", "embedded.token": "TokenAPI"}
        with open(args.cov, "w") as f:
            for method in sorted(passed_getters):
                ns, name = method.rsplit(".", 1)
                api = types.get(ns) or ns.split(".")[1].capitalize() + "Api"
                f.write("rpc-method\t%s.%s\tE1:SMOKE\n" % (api, name[0].upper() + name[1:]))
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
