#!/usr/bin/env python3
"""M-2: mainnet RPC differ for the go-zenon test framework (test level L6).

Compares a binary under test against a reference node on mainnet:

  M-2a  momentum header + account-block comparison at the spork heights and
        ranges listed in the L6 specification (0x3639/go-zenon#17), plus the
        frontier.
  M-2b  frontier momentum hash comparison.
  M-2c  frontier state: N account infos (batches, guarded by frontier hashes
        read before and after each batch) and every embedded.* read method in
        test/inventory/coverage.tsv, paged to the end.
  M-2d  controls: self-self comparison (must match) and a planted height
        difference h vs h+1 (must be detected).

Refuses to run unless every endpoint reports chain identifier 1 and the
mainnet genesis momentum hash at height 1.

Standard library Python 3 only.  Signs nothing: no wallet import, no call to
ledger.publishRawTransaction, no writes of any kind.

Output: one JSON line per comparison (JSONL), a final summary by case id,
and the commit the tool was built from.

  python3 test/mainnet/compare.py \
      --ref http://127.0.0.1:35997 \
      --test http://127.0.0.1:36997 \
      --sample-height 14700000 \
      --out results.jsonl

--sample-height is S of the sampling rule of 0x3639/go-zenon#17: the first
height divisible by 10,000 after the test node reported sync state 2
(fully synced).  A node that reached state 2 at height 14,691,234 uses
S = 14,700,000; the momentum at S must already exist on both nodes.
"""
import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.request

# Mainnet momentum-1 hash and the chain identifier every endpoint must report.
MAINNET_GENESIS_MOMENTUM = "9e204601d1b7b1427fe12bc82622e610d8a6ad43c40abf020eb66e538bb8eeb0"
MAINNET_CHAIN_ID = 1

# The zero address, used only when a run samples no addresses at all
# (--addr-count is validated > 0, so this is defensive).
ZERO_ADDRESS = "z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"

# M-2a heights: every spork activation height and the one before it, the
# 4,188,198-4,188,207 window (HTLC + Multiverse activations), and the
# 14,567,886-14,567,888 renewal window (0x3639's specification, #17).
M2A_HEIGHTS = (
    [1_241_272, 1_241_273]
    + list(range(4_188_198, 4_188_208))
    + [14_567_886, 14_567_887, 14_567_888]
)

# Methods left out of the M-2c getter list, with the reason (spec: list them).
LEFT_OUT = [
    ("stats.syncInfo", "node-local"),
    ("stats.networkInfo", "node-local"),
    ("stats.osInfo", "node-local"),
    ("stats.processInfo", "node-local"),
    ("ledger.getUnconfirmedBlocksByAddress", "unconfirmed pool is node-local"),
    ("ledger.publishRawTransaction", "write method"),
    ("ledger.subscribe", "subscription"),
]

# Known, intended API additions of dev over the v0.0.10 reference, per
# method: top-level result keys the test answer may carry that the
# reference answer lacks.  A result diff consisting of exactly these extra
# keys is INFO (logged with the reason), not FAIL, so exit status 0 stays
# a real gate for dev-vs-v0.0.10 runs.  Any other difference still FAILs.
EXPECTED_API_ADDITIONS = {
    # dev's Dynamic Plasma work adds the fused-only payment target to the
    # PoW quote (GetRequiredResult.RequiredFusedPlasma,
    # rpc/api/embedded/plasma.go); v0.0.10 never returns it.
    "embedded.plasma.getRequiredPoWForAccountBlock": {
        "requiredFusedPlasma": "added on dev with Dynamic Plasma (total "
                               "fused-only payment target); absent in v0.0.10",
    },
}

# List getters whose item order is node-local, not chain state:
# embedded.pillar.getAll sorts by descending Weight from the consensus
# cache, which is refreshed asynchronously (at most every 5 min), so a cold
# node reports all-zero weights and the order degrades to ascending name
# while a warm node reports weight order (rpc/api/embedded/pillar.go,
# PillarInfoByWeight).  embedded.pillar.getByOwner answers with the same
# PillarInfo list and the same node-local ordering.  These lists are
# sorted by a stable key before comparison; the cache-derived fields
# themselves (weight/rank/currentStats) are stripped by normalize().
SORT_BEFORE_COMPARE = {
    "embedded.pillar.getAll": lambda p: (p.get("name") or "",
                                         p.get("ownerAddress") or ""),
    "embedded.pillar.getByOwner": lambda p: (p.get("name") or "",
                                             p.get("ownerAddress") or ""),
}

PAGE = 1000          # RpcMaxPageSize / RpcMaxCountSize
MAX_PAGES = 500      # guard against unbounded paging; truncation is reported

# Known, intended API additions on dev INSIDE list items (the table above
# covers top-level result keys).  The item key is stripped from every
# element of the answer's "list" (or of the answer itself when it is a
# list) before comparison; a pair whose only difference is allowlisted
# item keys is INFO, not FAIL.
EXPECTED_ITEM_ADDITIONS = {
    "embedded.plasma.getEntriesByAddress": {
        "isRevocable": "added on dev with Dynamic Plasma: reports whether "
                       "the frontier height passed the entry's expiration "
                       "height (rpc/api/embedded/plasma.go); absent in "
                       "v0.0.10",
    },
}

# Known, intended error-surface changes on dev: the reference fails the
# call, the binary under test answers.  Key: method; value: (substring of
# the reference error, reason).  Only this direction is allowlisted — the
# binary under test erroring where the reference answers stays a FAIL.
EXPECTED_ERROR_SURFACE = {
    "embedded.bridge.getAllUnwrapTokenRequests": (
        "unknown network",
        "dev filters stored unwrap requests whose network or token pair "
        "has been removed (unwrapRequestList); v0.0.10 fails the whole "
        "call with ErrUnknownNetwork"),
    "embedded.bridge.getAllUnwrapTokenRequestsByToAddress": (
        "unknown network",
        "same filter change as getAllUnwrapTokenRequests"),
}

# How often a frontier-state comparison (account-info batch or getter) is
# retried when the frontier moved while it was being read.
FRONTIER_RETRIES = 3

# How many candidate addresses the populated-address probes try before
# falling back to the sampled address.
PROBE_MAX = 25

# Argument sources for the M-2c getter list (every embedded.* read method in
# test/inventory/coverage.tsv).  Markers:
#   ("addr",)          the first sampled mainnet address (see sample_addresses)
#   ("pillarOwner",)   populated addresses resolved from list getters on the
#   ("sentinelOwner",) reference node: pillar owner, sentinel owner, a staker,
#   ("staker",)        a liquidity staker, a fuser, a delegator, a token owner
#   ("liquidityStaker",) — so address-keyed getters are exercised against
#   ("fuser",)         non-empty state.  Each falls back to the sampled
#   ("delegator",)     address with a note in the logged address book when
#   ("tokenOwner",)    the chain has no such entity (see Ctx)
#   ("page",)          trailing paging marker: the getter is paged to the end
#   other tuples       lazily resolved from the reference node (see Ctx)
#   literals           used verbatim
GETTERS = [
    # accelerator (5 read methods in coverage.tsv)
    ("embedded.accelerator.getAll", [("page",)]),
    ("embedded.accelerator.getProjectById", [("project",)]),
    ("embedded.accelerator.getPhaseById", [("phase",)]),
    ("embedded.accelerator.getVoteBreakdown", [("project",)]),
    ("embedded.accelerator.getPillarVotes", [("name",), [("project",)]]),
    # bridge (15)
    ("embedded.bridge.getBridgeInfo", []),
    ("embedded.bridge.getSecurityInfo", []),
    ("embedded.bridge.getOrchestratorInfo", []),
    ("embedded.bridge.getTimeChallengesInfo", []),
    ("embedded.bridge.getNetworkInfo", [("networkClass",), ("chainId",)]),
    ("embedded.bridge.getAllNetworks", [("page",)]),
    ("embedded.bridge.getWrapTokenRequestById", [("wrapId",)]),
    ("embedded.bridge.getAllWrapTokenRequests", [("page",)]),
    ("embedded.bridge.getAllWrapTokenRequestsByToAddress", [("wrapToAddress",), ("page",)]),
    ("embedded.bridge.getAllWrapTokenRequestsByToAddressNetworkClassAndChainId",
        [("wrapToAddress",), ("networkClass",), ("chainId",), ("page",)]),
    ("embedded.bridge.getAllUnsignedWrapTokenRequests", [("page",)]),
    ("embedded.bridge.getUnwrapTokenRequestByHashAndLog", [("unwrapHash",), ("unwrapLogIndex",)]),
    ("embedded.bridge.getAllUnwrapTokenRequests", [("page",)]),
    ("embedded.bridge.getAllUnwrapTokenRequestsByToAddress", [("unwrapToAddress",), ("page",)]),
    ("embedded.bridge.getFeeTokenPair", [("feeZts",)]),
    # htlc (2; coverage.tsv has no list method, so getById uses the zero hash
    # and compares the not-found answer on both nodes)
    ("embedded.htlc.getById", [("zeroHash",)]),
    ("embedded.htlc.getProxyUnlockStatus", [("addr",)]),
    # liquidity (6)
    ("embedded.liquidity.getLiquidityInfo", []),
    ("embedded.liquidity.getSecurityInfo", []),
    ("embedded.liquidity.getLiquidityStakeEntriesByAddress", [("liquidityStaker",), ("page",)]),
    ("embedded.liquidity.getUncollectedReward", [("liquidityStaker",)]),
    ("embedded.liquidity.getFrontierRewardByPage", [("liquidityStaker",), ("page",)]),
    ("embedded.liquidity.getTimeChallengesInfo", []),
    # pillar (11)
    ("embedded.pillar.getDepositedQsr", [("pillarOwner",)]),
    ("embedded.pillar.getUncollectedReward", [("pillarOwner",)]),
    ("embedded.pillar.getFrontierRewardByPage", [("pillarOwner",), ("page",)]),
    ("embedded.pillar.getQsrRegistrationCost", []),
    ("embedded.pillar.getAll", [("page",)]),
    ("embedded.pillar.getByOwner", [("pillarOwner",)]),
    ("embedded.pillar.getByName", [("name",)]),
    ("embedded.pillar.checkNameAvailability", [("name",)]),
    ("embedded.pillar.getDelegatedPillar", [("delegator",)]),
    ("embedded.pillar.getPillarEpochHistory", [("name",), ("page",)]),
    ("embedded.pillar.getPillarsHistoryByEpoch", [1, ("page",)]),
    # plasma (4)
    ("embedded.plasma.get", [("fuser",)]),
    ("embedded.plasma.getEntriesByAddress", [("fuser",), ("page",)]),
    ("embedded.plasma.getRequiredPoWForAccountBlock",
        [{"address": ("fuser",), "blockType": 2, "toAddress": ("addr",), "data": ""}]),
    ("embedded.plasma.getVariables", []),
    # ptlc (2)
    ("embedded.ptlc.getById", [("zeroHash",)]),
    ("embedded.ptlc.getReclaimLockById", [("zeroHash",)]),
    # sentinel (5)
    ("embedded.sentinel.getAllActive", [("page",)]),
    ("embedded.sentinel.getByOwner", [("sentinelOwner",)]),
    ("embedded.sentinel.getDepositedQsr", [("sentinelOwner",)]),
    ("embedded.sentinel.getUncollectedReward", [("sentinelOwner",)]),
    ("embedded.sentinel.getFrontierRewardByPage", [("sentinelOwner",), ("page",)]),
    # spork (1)
    ("embedded.spork.getAll", [("page",)]),
    # stake (3)
    ("embedded.stake.getUncollectedReward", [("staker",)]),
    ("embedded.stake.getFrontierRewardByPage", [("staker",), ("page",)]),
    ("embedded.stake.getEntriesByAddress", [("staker",), ("page",)]),
    # swap (3)
    ("embedded.swap.getAssetsByKeyIdHash", [("swapKeyIdHash",)]),
    ("embedded.swap.getAssets", []),
    ("embedded.swap.getLegacyPillars", []),
    # token (3)
    ("embedded.token.getAll", [("page",)]),
    ("embedded.token.getByOwner", [("tokenOwner",), ("page",)]),
    ("embedded.token.getByZts", [("znnZts",)]),
]


def log(line, fh):
    fh.write(json.dumps(line, default=str) + "\n")
    fh.flush()


def rpc(endpoint, method, params=None, timeout=60, retries=3):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method,
                       "params": params or []}).encode()
    last = None
    for attempt in range(retries):
        try:
            req = urllib.request.Request(endpoint, data=body,
                                         headers={"Content-Type": "application/json"})
            resp = urllib.request.urlopen(req, timeout=timeout)
            data = json.loads(resp.read())
            if "error" in data:
                raise RuntimeError(f"{method}: {data['error']}")
            return data["result"]
        except RuntimeError:
            raise
        except Exception as e:
            last = e
            time.sleep(2 * (attempt + 1))
    raise last


def rpc_catching(endpoint, method, params=None, timeout=60):
    """Like rpc but returns (result, error-string); transport failures still
    raise after retries.  RPC-level errors are comparable answers."""
    try:
        return rpc(endpoint, method, params, timeout), None
    except RuntimeError as e:
        return None, str(e)


def tool_commit():
    """The commit the tool runs from, with -dirty appended unless the tree is
    clean, so a validation run is attributable to the code that produced it."""
    try:
        root = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
        out = subprocess.run(["git", "-C", root, "rev-parse", "HEAD"],
                             capture_output=True, text=True, timeout=10)
        if out.returncode == 0:
            commit = out.stdout.strip()
            dirty = subprocess.run(["git", "-C", root, "status", "--porcelain"],
                                   capture_output=True, text=True, timeout=10)
            if dirty.returncode == 0 and dirty.stdout.strip():
                return commit + "-dirty"
            return commit
    except Exception:
        pass
    return "unknown"


def gate(endpoint):
    """Spec: refuse to run unless every endpoint reports chain identifier 1
    and the mainnet genesis momentum hash at height 1."""
    m = rpc(endpoint, "ledger.getMomentumsByHeight", [1, 1])
    h = m["list"][0]
    if h.get("chainIdentifier") != MAINNET_CHAIN_ID:
        raise SystemExit(f"REFUSING: {endpoint} reports chain identifier "
                         f"{h.get('chainIdentifier')}, expected {MAINNET_CHAIN_ID}")
    if h.get("hash") != MAINNET_GENESIS_MOMENTUM:
        raise SystemExit(f"REFUSING: {endpoint} reports momentum-1 hash "
                         f"{h.get('hash')}, expected {MAINNET_GENESIS_MOMENTUM}")


def detailed(endpoint, h):
    return rpc(endpoint, "ledger.getDetailedMomentumsByHeight", [h, 1])


# PillarInfo fields served from a node-local consensus cache (refreshed
# asynchronously, at most every 5 min): weight, rank and currentStats
# legitimately differ between two nodes on the same frontier (see #17).
# Guarded by the producerAddress discriminator so a same-named field in
# another structure is never silently stripped.
PILLAR_NODE_LOCAL = ("weight", "rank", "currentStats")


def normalize(value):
    """Strip node-local contextual fields that legitimately differ between
    two nodes at different heights (but would also differ at the same
    height): confirmation counters and token supplies embedded in account
    blocks, and the pillar consensus-cache fields (weight/rank/currentStats).
    Applied recursively so pairedAccountBlock is covered too."""
    if isinstance(value, dict):
        is_pillar_info = "producerAddress" in value
        out = {}
        for k, v in value.items():
            if k == "numConfirmations":
                continue
            if is_pillar_info and k in PILLAR_NODE_LOCAL:
                continue
            if k == "token" and isinstance(v, dict):
                v = {tk: tv for tk, tv in v.items()
                     if tk not in ("totalSupply", "maxSupply")}
            out[k] = normalize(v)
        return out
    if isinstance(value, list):
        return [normalize(v) for v in value]
    return value


def first_diff(x, y, path=""):
    """A small record of the first difference between two answers, with a
    dotted path ("list[3].weight") to the differing leaf.  Keys present on
    only one side are reported as such."""
    if isinstance(x, dict) and isinstance(y, dict):
        for k in sorted(set(x) | set(y)):
            p = f"{path}.{k}" if path else k
            if k not in x:
                return {"path": p, "refMissing": True, "test": str(y[k])[:120]}
            if k not in y:
                return {"path": p, "testMissing": True, "ref": str(x[k])[:120]}
            if x[k] != y[k]:
                if isinstance(x[k], (dict, list)) and isinstance(y[k], (dict, list)):
                    return first_diff(x[k], y[k], p)
                return {"path": p, "ref": str(x[k])[:120], "test": str(y[k])[:120]}
    if isinstance(x, list) and isinstance(y, list):
        for i, (a, b) in enumerate(zip(x, y)):
            if a != b:
                p = f"{path}[{i}]"
                if isinstance(a, (dict, list)) and isinstance(b, (dict, list)):
                    return first_diff(a, b, p)
                return {"path": p, "ref": str(a)[:120], "test": str(b)[:120]}
        if len(x) != len(y):
            return {"path": path, "detail": "length", "ref": len(x), "test": len(y)}
    return {"path": path, "ref": str(x)[:120], "test": str(y)[:120]}


def compare_momentum_answer(ref_a, test_a):
    """Compare one getDetailedMomentumsByHeight answer.  Returns (status, detail)."""
    rl = ref_a.get("list") or []
    tl = test_a.get("list") or []
    if not rl or not tl:
        if not rl and not tl:
            return "INFO", {"detail": "no-momentum-at-height", "ref": 0, "test": 0}
        # Blocks/momentums on exactly one side: the full-history reference
        # (#17) serves every height, so one-sided availability is a FAIL.
        return "FAIL", {"detail": "momentum-availability-mismatch",
                        "ref": len(rl), "test": len(tl)}
    r, t = rl[0], tl[0]
    rm, tm = r["momentum"], t["momentum"]
    diffs = []
    for f in ("hash", "changesHash", "height", "version", "chainIdentifier"):
        if rm.get(f) != tm.get(f):
            diffs.append({"field": f, "ref": rm.get(f), "test": tm.get(f)})
    rb = r.get("blocks") or []
    tb = t.get("blocks") or []
    out = {"refBlocks": len(rb), "testBlocks": len(tb)}
    if rb and tb:
        nr, nt = normalize(rb), normalize(tb)
        if nr != nt:
            first = None
            for i, (x, y) in enumerate(zip(nr, nt)):
                if x != y:
                    # Union of keys: a key added on only one side must show
                    # up as the first diff, not leave firstDiff null.
                    for k in sorted(set(x) | set(y)):
                        if x.get(k) != y.get(k):
                            first = {"block": i, "field": k,
                                     "ref": str(x.get(k))[:120],
                                     "test": str(y.get(k))[:120]}
                            break
                if first:
                    break
            diffs.append({"field": "blocks", "ref": len(rb), "test": len(tb),
                          "firstDiff": first})
    elif rb or tb:
        # Blocks on exactly one side: the #17 reference node is full
        # history, so this is a chain difference, not an availability note.
        out["detail"] = "block-availability-mismatch"
        diffs.append({"field": "blocks", "ref": len(rb), "test": len(tb)})
    return ("FAIL" if diffs else "PASS"), {**out, **({"diffs": diffs} if diffs else {})}


def is_method_missing(err):
    """True iff the error string reports JSON-RPC method-not-found.  Only
    the -32601 code qualifies: message substrings such as "not available"
    or "does not exist" also appear in unrelated errors (account-state and
    not-found messages) and would misclassify a real error as an absent
    method."""
    return err is not None and "-32601" in err


def allowlisted_diff_only(method, nr, nt):
    """True iff the only difference between the two normalized answers is
    test-side extra keys listed in EXPECTED_API_ADDITIONS[method]."""
    allowed = EXPECTED_API_ADDITIONS.get(method)
    if not allowed or not isinstance(nr, dict) or not isinstance(nt, dict):
        return False
    extra = set(nt) - set(nr)
    if not extra or not extra <= set(allowed):
        return False
    return {k: v for k, v in nt.items() if k not in extra} == nr


def sort_list_answer(answer, key):
    """A copy of the answer with its list (the answer itself, or its "list"
    field) sorted by `key`; anything else is returned unchanged."""
    if isinstance(answer, dict) and isinstance(answer.get("list"), list):
        return {**answer, "list": sorted(answer["list"], key=key)}
    if isinstance(answer, list):
        return sorted(answer, key=key)
    return answer


def strip_item_keys(nt, allowed):
    """A copy of the normalized answer `nt` with every key in `allowed`
    removed from each element of its "list" (or of the answer itself when
    it is a list).  Returns (stripped, set of keys actually removed)."""
    removed = set()

    def strip_item(it):
        if isinstance(it, dict):
            extra = set(it) & set(allowed)
            if extra:
                removed.update(extra)
                return {k: v for k, v in it.items() if k not in extra}
        return it

    if isinstance(nt, dict) and isinstance(nt.get("list"), list):
        return {**nt, "list": [strip_item(i) for i in nt["list"]]}, removed
    if isinstance(nt, list):
        return [strip_item(i) for i in nt], removed
    return nt, removed


def classify_getter(method, ref_obs, test_obs):
    """Classify one getter pair.  Each observation is one of
    {"result": answer}      the call returned a result
    {"rpcError": str}       a JSON-RPC error (a comparable answer)
    {"transportError": str} the request failed below JSON-RPC after retries
    Returns (status, detail).  Pure: no I/O, no clock.

    Rules (see README):
      - any transport failure is ERROR, never PASS/FAIL: identical
        transport failures on both sides mean neither getter answered;
      - method-missing is classified before the one-sided-error rule:
        missing on the reference only is INFO (a getter added on dev
        after the reference release); missing on the test side only is
        FAIL (the reference serves it and the binary under test dropped
        it — a removed RPC surface is a regression, not a note); missing
        on both is INFO (consistent: neither build serves it);
      - identical RPC errors on both sides are PASS, different ones FAIL;
      - one-sided RPC error is FAIL, except a reference-side error listed
        in EXPECTED_ERROR_SURFACE with a matching message: an intended
        behavior change on dev, INFO with the reason;
      - results are compared after normalization (and list reordering for
        SORT_BEFORE_COMPARE methods); an expected API addition (top-level
        key in EXPECTED_API_ADDITIONS or list-item key in
        EXPECTED_ITEM_ADDITIONS) is INFO, anything else FAIL with the
        first difference recorded.
    """
    rtrans = ref_obs.get("transportError")
    ttrans = test_obs.get("transportError")
    if rtrans is not None or ttrans is not None:
        d = {"refError": (rtrans or "")[:200], "testError": (ttrans or "")[:200]}
        if rtrans is not None and ttrans is not None:
            # Identical transport failures are NOT agreement: neither
            # getter returned an answer to compare.
            d["detail"] = "transport-failure-both-sides"
        else:
            d["detail"] = "transport-failure-on-one-side"
        return "ERROR", d
    rerr = ref_obs.get("rpcError")
    terr = test_obs.get("rpcError")
    if rerr is not None or terr is not None:
        d = {"refError": (rerr or "")[:200], "testError": (terr or "")[:200]}
        rmiss, tmiss = is_method_missing(rerr), is_method_missing(terr)
        if rmiss and tmiss:
            # Neither build serves the method: consistent, documented as
            # INFO rather than PASS so the missing surface stays visible.
            d["detail"] = "method-missing-on-both"
            return "INFO", d
        if rmiss:
            # The method exists only on the binary under test (a getter
            # added after the reference release): expected, not a chain
            # difference.
            d["detail"] = "method-missing-on-ref"
            return "INFO", d
        if tmiss:
            # The reference serves it but the binary under test does not:
            # a removed RPC surface is a regression and must gate the run,
            # not degrade to a note.
            d["detail"] = "method-missing-on-test"
            return "FAIL", d
        if rerr is not None and terr is not None:
            # Identical errors on both sides are agreement (not-found id,
            # ...).  Different errors are a real difference in the error
            # surface.
            if rerr == terr:
                return "PASS", d
            d["detail"] = "error-surface-differs"
            return "FAIL", d
        expected = EXPECTED_ERROR_SURFACE.get(method)
        if (expected is not None and rerr is not None and terr is None
                and expected[0] in rerr):
            # The reference fails the call where dev answers by design
            # (e.g. v0.0.10 errors a stored unwrap request whose network
            # was removed; dev filters it): intended, INFO with reason.
            d["detail"] = "expected-error-surface-change"
            d["reason"] = expected[1]
            return "INFO", d
        d["detail"] = "error-on-one-side"
        return "FAIL", d
    rr, tt = ref_obs["result"], test_obs["result"]
    key = SORT_BEFORE_COMPARE.get(method)
    if key is not None:
        rr = sort_list_answer(rr, key)
        tt = sort_list_answer(tt, key)
    nr, nt = normalize(rr), normalize(tt)
    if nr == nt:
        return "PASS", {}
    if allowlisted_diff_only(method, nr, nt):
        extra = sorted(set(nt) - set(nr))
        return "INFO", {"detail": "expected-api-addition", "addedKeys": extra,
                        "reasons": {k: EXPECTED_API_ADDITIONS[method][k]
                                    for k in extra}}
    item_allowed = EXPECTED_ITEM_ADDITIONS.get(method)
    if item_allowed is not None:
        stripped, removed = strip_item_keys(nt, item_allowed)
        if removed and stripped == nr:
            return "INFO", {"detail": "expected-api-addition",
                            "addedKeys": sorted(removed),
                            "reasons": {k: item_allowed[k]
                                        for k in sorted(removed)}}
    return "FAIL", {"detail": "result-differs", "firstDiff": first_diff(nr, nt)}


class ArgUnavailable(Exception):
    """The reference chain has no entity to build this argument from (e.g.
    no accelerator project with a phase).  Dependent getters are SKIP with
    the reason, not ERROR: there is nothing to compare, and that is a
    property of the chain, not a failure of either node."""


class Ctx:
    """Lazily-resolved argument sources, read once from the reference node.

    Address-keyed getters are exercised with addresses known to hold the
    relevant state (a pillar owner has pillar state, a staker has stake
    entries, ...), resolved from list getters on the reference node, so the
    comparison does not degenerate to comparing trivially-empty answers.
    When the chain has no such entity the sampled address is used and the
    fallback is recorded in `sources` (logged as the address book)."""

    def __init__(self, ref, addr):
        self.ref = ref
        self.addr = addr
        self.cache = {}
        self.sources = {}   # address slot -> where the address came from

    def get(self, key):
        if key not in self.cache:
            self.cache[key] = self._resolve(key)
        return self.cache[key]

    def address_book(self):
        """The resolved populated-address slots and their provenance."""
        return {k: {"address": self.cache[k], "source": self.sources[k]}
                for k in sorted(self.sources)}

    def _pillars(self):
        if "pillars" not in self.cache:
            res = rpc(self.ref, "embedded.pillar.getAll", [0, 100])
            self.cache["pillars"] = (res or {}).get("list") or []
        return self.cache["pillars"]

    def _sentinels(self):
        if "sentinels" not in self.cache:
            res, err = rpc_catching(self.ref, "embedded.sentinel.getAllActive",
                                    [0, 100])
            self.cache["sentinels"] = [] if err else ((res or {}).get("list") or [])
        return self.cache["sentinels"]

    def _candidates(self):
        """Addresses known to hold state, most-likely-populated first."""
        if "candidates" not in self.cache:
            cands = [p["ownerAddress"] for p in self._pillars()
                     if p.get("ownerAddress")]
            cands += [s["owner"] for s in self._sentinels() if s.get("owner")]
            self.cache["candidates"] = cands
        return self.cache["candidates"]

    def _fallback(self, key, why):
        self.sources[key] = f"fallback: sampled address ({why})"
        return self.addr

    def _probe_list(self, key, method):
        """The first candidate whose `method` answer carries a non-empty
        list, else the sampled address with a note."""
        for cand in self._candidates()[:PROBE_MAX]:
            res, err = rpc_catching(self.ref, method, [cand, 0, 1])
            if not err and ((res or {}).get("list") or []):
                self.sources[key] = f"{method} non-empty"
                return cand
        return self._fallback(key, f"no candidate with entries in {method}")

    def _resolve(self, key):
        if key == "addr":
            return self.addr
        if key == "zeroHash":
            return "00" * 32
        if key == "pillarOwner":
            for p in self._pillars():
                if p.get("ownerAddress"):
                    self.sources[key] = "embedded.pillar.getAll ownerAddress"
                    return p["ownerAddress"]
            raise ArgUnavailable("no pillar on ref")
        if key == "sentinelOwner":
            for s in self._sentinels():
                if s.get("owner"):
                    self.sources[key] = "embedded.sentinel.getAllActive owner"
                    return s["owner"]
            return self._fallback(key, "no active sentinel on ref")
        if key == "staker":
            return self._probe_list(key, "embedded.stake.getEntriesByAddress")
        if key == "liquidityStaker":
            return self._probe_list(key,
                                    "embedded.liquidity.getLiquidityStakeEntriesByAddress")
        if key == "fuser":
            return self._probe_list(key, "embedded.plasma.getEntriesByAddress")
        if key == "delegator":
            for cand in self._candidates()[:PROBE_MAX]:
                res, err = rpc_catching(self.ref, "embedded.pillar.getDelegatedPillar",
                                        [cand])
                if not err and res:
                    self.sources[key] = "embedded.pillar.getDelegatedPillar non-null"
                    return cand
            return self._fallback(key, "no delegated candidate found")
        if key == "tokenOwner":
            for pi in range(5):
                res = rpc(self.ref, "embedded.token.getAll", [pi, 100])
                tokens = (res or {}).get("list") or []
                for t in tokens:
                    if t.get("owner"):
                        self.sources[key] = "embedded.token.getAll owner"
                        return t["owner"]
                if len(tokens) < 100:
                    break
            return self._fallback(key, "no token owner on ref")
        if key == "name":
            pillars = self._pillars()
            if not pillars:
                raise ArgUnavailable("no pillar on ref")
            return pillars[0]["name"]
        if key == "project":
            l = rpc(self.ref, "embedded.accelerator.getAll", [0, 1])["list"]
            if not l:
                raise ArgUnavailable("no accelerator project on ref")
            return l[0]["id"]
        if key == "phase":
            # getAll returns projects sorted by descending
            # lastUpdateTimestamp, so the first project may be a fresh
            # submission with phases: [] — scan for the first project that
            # actually has a phase.
            for pi in range(5):
                res = rpc(self.ref, "embedded.accelerator.getAll", [pi, 100])
                projects = (res or {}).get("list") or []
                for p in projects:
                    d = rpc(self.ref, "embedded.accelerator.getProjectById",
                            [p["id"]])
                    for e in (d or {}).get("phases") or []:
                        if (e.get("phase") or {}).get("id"):
                            return e["phase"]["id"]
                if len(projects) < 100:
                    break
            raise ArgUnavailable("no accelerator project with a phase on ref")
        if key in ("networkClass", "chainId"):
            l = rpc(self.ref, "embedded.bridge.getAllNetworks", [0, 1])["list"]
            if not l:
                raise ArgUnavailable("no bridge network on ref")
            return l[0][key]
        if key in ("wrapId", "wrapToAddress"):
            l = rpc(self.ref, "embedded.bridge.getAllWrapTokenRequests", [0, 1])["list"]
            if not l:
                raise ArgUnavailable("no wrap request on ref")
            return l[0]["id"] if key == "wrapId" else l[0]["toAddress"]
        if key in ("unwrapHash", "unwrapLogIndex", "unwrapToAddress"):
            l = rpc(self.ref, "embedded.bridge.getAllUnwrapTokenRequests", [0, 1])["list"]
            if not l:
                raise ArgUnavailable("no unwrap request on ref")
            e = l[0]
            return {"unwrapHash": e.get("transactionHash"),
                    "unwrapLogIndex": e.get("logIndex"),
                    "unwrapToAddress": e.get("toAddress")}[key]
        if key == "feeZts":
            # ZNN is always a bridge fee token; resolve its zts from the
            # chain instead of embedding a checksum-bearing constant.
            return self.get("znnZts")
        if key == "znnZts":
            for pi in range(20):
                res = rpc(self.ref, "embedded.token.getAll", [pi, 100])
                for t in (res or {}).get("list") or []:
                    if t.get("symbol") == "ZNN":
                        return t["tokenStandard"]
                if len((res or {}).get("list") or []) < 100:
                    break
            raise ArgUnavailable("ZNN token not found on ref")
        if key == "swapKeyIdHash":
            assets = rpc(self.ref, "embedded.swap.getAssets") or {}
            for h in assets:
                return h
            raise ArgUnavailable("no swap asset on ref")
        raise RuntimeError(f"unknown arg source {key}")


def build_args(tmpl, ctx):
    out = []
    for a in tmpl:
        if isinstance(a, tuple):
            out.append(ctx.get(a[0]))
        elif isinstance(a, list):
            out.append(build_args(a, ctx))
        elif isinstance(a, dict):
            out.append({k: (ctx.get(v[0]) if isinstance(v, tuple) else v)
                        for k, v in a.items()})
        else:
            out.append(a)
    return out


def page_all(endpoint, method, fixed_args):
    """Page a list getter to the end.  Returns (answer, pages, truncated)
    where answer is the page-0 answer with "list" replaced by the merged
    items of all pages, so top-level fields next to "list" (count,
    qsrAmount, totalAmount, totalWeightedAmount, ...) survive and are
    compared too."""
    merged = []
    top = {}
    for pi in range(MAX_PAGES):
        res = rpc(endpoint, method, fixed_args + [pi, PAGE], timeout=180) or {}
        items = res.get("list") or []
        if pi == 0:
            top = {k: v for k, v in res.items() if k != "list"}
        merged.extend(items)
        count = res.get("count")
        if len(items) < PAGE or (count is not None and len(merged) >= count):
            return {**top, "list": merged}, pi + 1, False
    return {**top, "list": merged}, MAX_PAGES, True


def observe(endpoint, method, fixed, paged):
    """Run one getter on one endpoint.  Returns (observation, meta) for
    classify_getter; meta carries page counts/truncation for the log."""
    meta = {}
    try:
        if paged:
            ans, pages, trunc = page_all(endpoint, method, fixed)
            meta["Pages"] = pages
            if trunc:
                meta["Truncated"] = True
            return {"result": ans}, meta
        return {"result": rpc(endpoint, method, fixed, timeout=180)}, meta
    except RuntimeError as e:
        return {"rpcError": str(e)}, meta
    except Exception as e:
        return {"transportError": str(e)}, meta


def sample_addresses(endpoint, ref, S, want, fh):
    """The sampling rule of 0x3639/go-zenon#17, so the sample is the same
    from any node and can be checked later: the seed is the momentum hash
    at height S, the first height divisible by 10,000 after the test node
    reports sync state 2.  For i = 0, 1, 2 and so on, sha3-256 of the seed
    followed by i as 8 big-endian bytes; the first 8 bytes of the digest,
    read as a big-endian integer n, name the momentum at 1 + (n mod S).
    Collect the address of each account block in those momentums until
    `want` distinct ones, stopping inside the momentum that reaches `want`.
    Record for each: the address, the height it was found at, and the
    account's height and frontier block hash at sampling time."""
    if S <= 0 or S % 10000 != 0:
        raise SystemExit(f"sample rule: S={S} is not a height divisible by 10,000")
    seed_hex = rpc(endpoint, "ledger.getMomentumsByHeight", [S, 1])["list"][0]["hash"]
    ref_seed = rpc(ref, "ledger.getMomentumsByHeight", [S, 1])["list"][0]["hash"]
    if ref_seed != seed_hex:
        raise SystemExit(f"sample rule: seed hash mismatch at S={S} "
                         f"(ref {ref_seed} != test {seed_hex}); nodes not on one chain")
    seed = bytes.fromhex(seed_hex.removeprefix("0x"))
    log({"case": "M-2c", "status": "INFO", "detail": "sample-rule",
         "S": S, "seedHash": seed_hex, "want": want}, fh)
    seen, order = set(), []
    i = 0
    while len(order) < want:
        d = hashlib.sha3_256(seed + i.to_bytes(8, "big")).digest()
        h = 1 + int.from_bytes(d[:8], "big") % S
        i += 1
        try:
            page = detailed(endpoint, h).get("list") or []
        except Exception as e:
            # Never skip an index silently: the sample would no longer be
            # the rule's output and two runs could draw different samples.
            raise SystemExit(
                f"sample rule: momentum at height {h} (i={i - 1}) could not be "
                f"read after retries: {e}. Refusing to continue with a "
                f"skipped index.")
        for e in page:
            for b in e.get("blocks") or []:
                a = b.get("address")
                if a and a not in seen:
                    seen.add(a)
                    order.append({"address": a, "foundHeight": h})
                    if len(order) == want:
                        break
            if len(order) == want:
                break
    for row in order:
        page = rpc(endpoint, "ledger.getAccountBlocksByPage",
                   [row["address"], 0, 1], timeout=30).get("list") or []
        if page:
            row["accountHeight"] = page[0].get("height")
            row["frontierBlockHash"] = page[0].get("hash")
        else:
            row["accountHeight"] = 0
            row["frontierBlockHash"] = None
    return order


def frontier_hash(endpoint):
    return rpc(endpoint, "ledger.getFrontierMomentum", timeout=30).get("hash")


def run(ref, test, args, fh):
    log({"case": "header", "tool": "test/mainnet/compare.py",
         "toolCommit": tool_commit(),
         "startedUtc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
         "ref": ref, "test": test,
         "params": {"addrCount": args.addr_count, "sampleHeight": args.sample_height,
                    "batchSize": args.batch_size},
         "leftOut": LEFT_OUT,
         "expectedApiAdditions": {m: sorted(k) for m, k in
                                  EXPECTED_API_ADDITIONS.items()}}, fh)

    gate(ref)
    gate(test)
    log({"case": "gate", "status": "PASS",
         "detail": "chain id 1 and mainnet genesis momentum verified on both endpoints"}, fh)

    fr = rpc(ref, "ledger.getFrontierMomentum", timeout=30)
    ft = rpc(test, "ledger.getFrontierMomentum", timeout=30)
    t_frontier = ft["height"]

    # ---- M-2a: spork-window momentum comparisons
    for h in M2A_HEIGHTS:
        r = {"case": "M-2a", "height": h}
        if h > t_frontier:
            r["status"] = "INFO"; r["detail"] = "test-not-synced"; r["testHeight"] = t_frontier
        else:
            try:
                status, detail = compare_momentum_answer(detailed(ref, h), detailed(test, h))
                r["status"] = status; r.update(detail)
            except Exception as e:
                r["status"] = "ERROR"; r["error"] = str(e)[:200]
        log(r, fh)

    # ---- M-2b: frontier comparison
    r = {"case": "M-2b",
         "ref": {"height": fr["height"], "hash": fr["hash"]},
         "test": {"height": ft["height"], "hash": ft["hash"]}}
    if fr["hash"] == ft["hash"]:
        r["status"] = "PASS"
    elif fr["height"] == ft["height"]:
        r["status"] = "FAIL"; r["detail"] = "split-chain"
    else:
        r["status"] = "INFO"; r["detail"] = "height-mismatch-syncing"
    log(r, fh)

    # The frontier itself, guarded by the M-2b result: a full
    # getDetailedMomentumsByHeight answer at the frontier height is part of
    # the spec; it is only meaningful when both nodes are on the same
    # frontier hash.
    if fr["hash"] == ft["hash"]:
        r = {"case": "M-2a", "height": fr["height"], "frontier": True}
        try:
            status, detail = compare_momentum_answer(
                detailed(ref, fr["height"]), detailed(test, ft["height"]))
            r["status"] = status; r.update(detail)
        except Exception as e:
            r["status"] = "ERROR"; r["error"] = str(e)[:200]
        log(r, fh)
    else:
        log({"case": "M-2a", "height": fr["height"], "frontier": True,
             "status": "INFO", "detail": "skipped: frontier hashes differ (see M-2b)"}, fh)

    # ---- M-2c: frontier state
    # The sample is drawn by the #17 rule from the test node (the seed is
    # cross-checked against the reference node, which is full history).
    # The manifest (address, found height, account height, frontier block
    # hash at sampling) goes to its own file, hashed into the log, so the
    # sample can be checked later from any node.
    rows = sample_addresses(test, ref, args.sample_height, args.addr_count, fh)
    with open(args.sample_out, "w") as sf:
        for row in rows:
            sf.write(json.dumps(row) + "\n")
    with open(args.sample_out, "rb") as sf:
        manifest_hash = hashlib.sha256(sf.read()).hexdigest()
    addrs = [r["address"] for r in rows]
    log({"case": "M-2c", "status": "INFO", "detail": "sample-manifest",
         "count": len(addrs), "file": args.sample_out, "sha256": manifest_hash}, fh)

    # Account infos.  A batch counts only if the frontier hashes of both
    # nodes are identical before and after it; if the frontier moved the
    # batch is retried a bounded number of times before it degrades to
    # INFO.  ledger.getAccountInfoByAddress reads the frontier account
    # store, which includes node-local unconfirmed pool blocks, so two
    # nodes with identical frontier hashes can still disagree
    # intermittently: a differing address is re-fetched on both sides once
    # before it is declared a FAIL.
    compared_batches = 0
    for i in range(0, len(addrs), args.batch_size):
        batch = addrs[i:i + args.batch_size]
        r = {"case": "M-2c-accountInfos", "batch": i // args.batch_size, "n": len(batch)}
        for attempt in range(1, FRONTIER_RETRIES + 1):
            h_before = (frontier_hash(ref), frontier_hash(test))
            infos_ref, infos_test = [], []
            try:
                for a in batch:
                    infos_ref.append(rpc(ref, "ledger.getAccountInfoByAddress", [a], timeout=30))
                    infos_test.append(rpc(test, "ledger.getAccountInfoByAddress", [a], timeout=30))
            except Exception as e:
                r["status"] = "ERROR"; r["error"] = str(e)[:200]
                compared_batches += 1
                break
            h_after = (frontier_hash(ref), frontier_hash(test))
            if h_before[0] == h_before[1] == h_after[0] == h_after[1]:
                diffs = [a for a, x, y in zip(batch, infos_ref, infos_test) if x != y]
                if diffs:
                    try:
                        hp_before = (frontier_hash(ref), frontier_hash(test))
                        still = []
                        for a in diffs:
                            x2 = rpc(ref, "ledger.getAccountInfoByAddress", [a], timeout=30)
                            y2 = rpc(test, "ledger.getAccountInfoByAddress", [a], timeout=30)
                            if x2 != y2:
                                still.append(a)
                        hp_after = (frontier_hash(ref), frontier_hash(test))
                    except Exception as e:
                        r["status"] = "ERROR"; r["error"] = ("pool-retry: " + str(e))[:200]
                        compared_batches += 1
                        break
                    if not (hp_before[0] == hp_before[1] == hp_after[0] == hp_after[1]):
                        # The frontier advanced during the pool re-read:
                        # the re-read then compares different chain states
                        # and would report pool noise as a chain diff.
                        # Retry the batch; degrade to INFO (uncounted) at
                        # the retry bound.
                        if attempt < FRONTIER_RETRIES:
                            continue
                        r["status"] = "INFO"
                        r["detail"] = "frontier-moved-during-batch"
                        r["attempts"] = attempt
                        break
                    if still:
                        r["status"] = "FAIL"
                        r["firstDiffAddress"] = still[0]
                        r["diffAddresses"] = len(still)
                    else:
                        # The difference was node-local unconfirmed pool
                        # state and resolved on the single re-read.
                        r["status"] = "PASS"
                        r["poolRetryResolved"] = len(diffs)
                else:
                    r["status"] = "PASS"
                compared_batches += 1
                if attempt > 1:
                    r["attempts"] = attempt
                break
            if attempt == FRONTIER_RETRIES:
                r["status"] = "INFO"
                r["detail"] = "frontier-moved-during-batch"
                r["attempts"] = attempt
        log(r, fh)

    # Embedded getters: run the identical walk on both nodes and compare.
    # classify_getter decides the verdict (see its docstring and the
    # README); a result comparison counts only when the frontier hashes of
    # both nodes are identical before and after the walk, retried a
    # bounded number of times before it degrades to INFO.
    addr = addrs[0] if addrs else ZERO_ADDRESS
    ctx = Ctx(ref, addr)
    compared_getters = 0
    for method, tmpl in GETTERS:
        r = {"case": "M-2c-getters", "method": method}
        try:
            paged = bool(tmpl and isinstance(tmpl[-1], tuple) and tmpl[-1][0] == "page")
            fixed = build_args(tmpl[:-1] if paged else tmpl, ctx)
        except ArgUnavailable as e:
            # The reference chain has no entity to build the argument from:
            # nothing to compare, by property of the chain, not a failure.
            r["status"] = "SKIP"; r["detail"] = ("arg-unavailable: " + str(e))[:200]
            log(r, fh)
            continue
        except Exception as e:
            r["status"] = "ERROR"; r["error"] = ("arg-resolution: " + str(e))[:200]
            log(r, fh)
            continue
        for attempt in range(1, FRONTIER_RETRIES + 1):
            h_before = (frontier_hash(ref), frontier_hash(test))
            ref_obs, ref_meta = observe(ref, method, fixed, paged)
            test_obs, test_meta = observe(test, method, fixed, paged)
            h_after = (frontier_hash(ref), frontier_hash(test))
            status, detail = classify_getter(method, ref_obs, test_obs)
            stable = h_before[0] == h_before[1] == h_after[0] == h_after[1]
            if "result" in ref_obs and "result" in test_obs:
                if not stable:
                    # A result comparison across a moved frontier proves
                    # nothing; retry, and degrade to INFO after the bound.
                    # Not counted as compared: an all-frontier-moved run
                    # must not pass as exit 0 (see the no-getter-compared
                    # ERROR).
                    if attempt < FRONTIER_RETRIES:
                        continue
                    status, detail = "INFO", {"detail": "frontier-moved-during-getter"}
                else:
                    compared_getters += 1
            # Error and missing-method outcomes are verdicts too, but they
            # do not count toward the no-getter-compared guard: a run
            # whose result-bearing getters all degraded to frontier-moved
            # must not pass on error agreements alone.
            r["status"] = status
            r.update(detail)
            for k, v in ref_meta.items():
                r["ref" + k] = v
            for k, v in test_meta.items():
                r["test" + k] = v
            if attempt > 1:
                r["attempts"] = attempt
            break
        log(r, fh)

    log({"case": "M-2c-getters", "status": "INFO", "detail": "address-book",
         "addresses": ctx.address_book()}, fh)

    # A run in which the frontier moved through every retry of every batch
    # or getter compared nothing; it must not pass as exit 0.
    if addrs and compared_batches == 0:
        log({"case": "M-2c-accountInfos", "status": "ERROR",
             "detail": "no-batch-compared: the frontier moved through every "
                       "retry of every batch"}, fh)
    if compared_getters == 0:
        log({"case": "M-2c-getters", "status": "ERROR",
             "detail": "no-getter-compared: no result-bearing getter "
                       "comparison completed (frontier moved through every "
                       "retry, or only error/missing-method outcomes)"}, fh)

    # ---- M-2d: controls
    try:
        s, d = compare_momentum_answer(detailed(ref, 100), detailed(ref, 100))
        log({"case": "M-2d", "control": "self-self", "height": 100, "status": s, **d}, fh)
    except Exception as e:
        log({"case": "M-2d", "control": "self-self", "status": "ERROR",
             "error": str(e)[:200]}, fh)
    try:
        s, d = compare_momentum_answer(detailed(ref, 100), detailed(ref, 101))
        # The two heights genuinely differ, so the differ reporting FAIL is
        # the control passing.
        log({"case": "M-2d", "control": "planted-height-diff", "heights": [100, 101],
             "status": "PASS" if s == "FAIL" else "FAIL", **d}, fh)
    except Exception as e:
        log({"case": "M-2d", "control": "planted-height-diff", "status": "ERROR",
             "error": str(e)[:200]}, fh)


def summarize(path):
    counts = {}
    with open(path) as fh:
        for line in fh:
            try:
                r = json.loads(line)
            except json.JSONDecodeError:
                continue
            if r.get("case") in ("header", "gate", "summary"):
                continue
            k = (r.get("case"), r.get("status", "ERROR"))
            counts[k] = counts.get(k, 0) + 1
    by_case = {}
    for (case, status), n in sorted(counts.items()):
        by_case.setdefault(case, {})[status] = n
    with open(path, "a") as fh:
        log({"case": "summary", "byCase": by_case,
             "fail": sum(n for (c, s), n in counts.items() if s == "FAIL"),
             "error": sum(n for (c, s), n in counts.items() if s == "ERROR")}, fh)
    return by_case


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--ref", default="http://127.0.0.1:35997")
    ap.add_argument("--test", default="http://127.0.0.1:36997")
    ap.add_argument("--addr-count", type=int, default=1000)
    ap.add_argument("--sample-height", type=int, required=True,
                    help="first height divisible by 10,000 after the test node "
                         "reported sync state 2 (the #17 sampling rule's S)")
    ap.add_argument("--batch-size", type=int, default=50)
    ap.add_argument("--out", default="")
    ap.add_argument("--sample-out", default="",
                    help="where the sample manifest is written (default: <out>.sample.jsonl)")
    args = ap.parse_args()

    if args.batch_size <= 0:
        ap.error(f"--batch-size must be > 0, got {args.batch_size}")
    if args.addr_count <= 0:
        ap.error(f"--addr-count must be > 0, got {args.addr_count}")

    tmpdir = None
    if args.out:
        path = args.out
    else:
        # A unique directory per run: two slices without --out must not
        # clobber each other's results or sample manifests.
        tmpdir = tempfile.mkdtemp(prefix="compare-")
        path = os.path.join(tmpdir, "compare-results.jsonl")
        print(f"writing to {path} (use --out to choose the location)",
              file=sys.stderr)
    if not args.sample_out:
        args.sample_out = path + ".sample.jsonl"
    with open(path, "w") as fh:
        run(args.ref, args.test, args, fh)
    by_case = summarize(path)
    if not args.out:
        with open(path) as fh:
            sys.stdout.write(fh.read())
        shutil.rmtree(tmpdir, ignore_errors=True)
    else:
        print(f"Wrote {path}", file=sys.stderr)
    fail = sum(s.get("FAIL", 0) for s in by_case.values())
    err = sum(s.get("ERROR", 0) for s in by_case.values())
    print(f"summary: {json.dumps(by_case)}", file=sys.stderr)
    sys.exit(0 if fail == 0 and err == 0 else 1)


if __name__ == "__main__":
    main()
