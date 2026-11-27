#!/usr/bin/env python3
"""M-3: mainnet history scanner for the go-zenon test framework (level L6).

Two scans over mainnet history:

  --headers  Walk every momentum from 1 to a stated height in pages of 1000,
             verifying that every entry is exactly the expected height and
             that the hash links hold: the first entry's previousHash must
             equal the hash of the last entry of the previous page.
             Reports how many momentums were read and any break.

  --blocks   Scan a stated height range reading full account blocks (10
             momentums per call), reporting how many momentums and account
             blocks were read, all ChangeTssECDSAPubKey sends (bridge key
             changes, method id 15a0c641 — the first 4 bytes of the SHA3-256
             of the method signature, computed below) including on-curve and
             leading-zero-byte X/Y coordinate (< 2^248) checks, the live
             stored decompressedTssECDSAPubKey from embedded.bridge.getBridgeInfo
             (must be 65 bytes, 0x04 prefix, on-curve when non-empty), every
             account block whose Difficulty read as unsigned 64-bit is >= 2^63,
             and visibility counts for blocks above the Dynamic Plasma and
             legacy difficulty caps.

Refuses to run unless the endpoint reports chain identifier 1 and the
mainnet genesis momentum hash at height 1.

Standard library Python 3 only.  Does not publish transactions.

Outcome policy (docs/TEST-PLAN.md M-3: "none found, or each one listed"):
integrity problems (short read, wrong heights in a page, parse or decode
failures) FAIL the scan; #78 findings (an off-curve, unrecognized, or
leading-zero-coordinate bridge key, an invalid/short live bridge key, or a
difficulty >= 2^63) are listed one by one and counted in the summary, but
do not by themselves fail it.

Output: one JSON line per finding and per progress milestone, a final
summary, and the commit the tool was built from.

  python3 test/mainnet/scan.py --node http://127.0.0.1:35997 \
      --headers --to 14661812 --out scan.jsonl

  python3 test/mainnet/scan.py --node http://127.0.0.1:35997 \
      --blocks --from 1 --to 14661812 --out scan.jsonl
"""
import argparse
import base64
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.request

MAINNET_GENESIS_MOMENTUM = "9e204601d1b7b1427fe12bc82622e610d8a6ad43c40abf020eb66e538bb8eeb0"
MAINNET_CHAIN_ID = 1

# ChangeTssECDSAPubKey(string,string,string): first 4 bytes of SHA3-256,
# identical to vm/abi Method.Id() in go-zenon.
BRIDGE_KEY_SIG = "ChangeTssECDSAPubKey(string,string,string)"
BRIDGE_KEY_SELECTOR = hashlib.sha3_256(BRIDGE_KEY_SIG.encode()).digest()[:4]

HIGH_DIFF_THRESHOLD = 2**63
# Difficulty caps in go-zenon for visibility ahead of DynamicPlasmaSpork
# activation (not #78 findings on their own):
DP_MAX_DIFFICULTY = 315_000_000_000_000  # dp.MaxDifficultyForAccountBlock
LEGACY_MAX_DIFFICULTY = 141_750_000      # constants.MaxDifficultyForAccountBlock
DIFF_LOG_CAP = 100
PAGE = 1000
DETAILED = 10           # getDetailedMomentumsByHeight count per call
PROGRESS_EVERY = 100_000

# secp256k1 field prime, for the on-curve check of bridge key changes.
SECP256K1_P = 2**256 - 2**32 - 977
# Coordinates strictly below 2^248 have at least one leading zero byte in their
# 32-byte big-endian representation (big.Int.Bytes() produces < 32 bytes).
SHORT_COORD_THRESHOLD = 1 << 248

# The embedded bridge contract: only sends to this address whose data starts
# with the selector are real ChangeTssECDSAPubKey calls.
BRIDGE_CONTRACT = "z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d"

# User-send account blocks are blockType 2.
USER_SEND = 2


def log(line, fh):
    fh.write(json.dumps(line, default=str) + "\n")
    fh.flush()


def rpc(endpoint, method, params=None, timeout=120, retries=3):
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
    m = rpc(endpoint, "ledger.getMomentumsByHeight", [1, 1])
    h = m["list"][0]
    if h.get("chainIdentifier") != MAINNET_CHAIN_ID:
        raise SystemExit(f"REFUSING: {endpoint} reports chain identifier "
                         f"{h.get('chainIdentifier')}, expected {MAINNET_CHAIN_ID}")
    if h.get("hash") != MAINNET_GENESIS_MOMENTUM:
        raise SystemExit(f"REFUSING: {endpoint} reports momentum-1 hash "
                         f"{h.get('hash')}, expected {MAINNET_GENESIS_MOMENTUM}")


def scan_headers(endpoint, from_h, to_h, fh):
    """Walk momentum headers from_h..to_h in pages.  Every returned entry
    must be exactly the expected height (from_h + i), and each page's first
    previousHash must equal the hash of the last entry of the previous
    page.  The last page is capped at to_h; anything short of to_h is a
    FAIL."""
    read = 0
    prev_hash = None
    h = from_h
    last_h = from_h - 1
    link_breaks = []
    short = False
    while h <= to_h:
        want = min(PAGE, to_h - h + 1)
        page = rpc(endpoint, "ledger.getMomentumsByHeight", [h, want])["list"]
        if not page:
            short = True
            break
        for i, m in enumerate(page):
            if m.get("height") != h + i:
                # A duplicated or reordered height passes a count+link
                # check; require the exact sequence instead.
                link_breaks.append({"detail": "unexpected height in page",
                                    "atIndex": i, "expected": h + i,
                                    "got": m.get("height")})
            if prev_hash is not None and m.get("previousHash") != prev_hash:
                link_breaks.append({"atHeight": m.get("height"),
                                    "previousHash": m.get("previousHash"),
                                    "expected": prev_hash})
            prev_hash = m.get("hash")
            read += 1
        if not isinstance(page[-1].get("height"), int) or page[-1]["height"] < h:
            # No forward progress (garbage heights): the breaks above
            # already fail the scan; stop instead of looping.
            break
        last_h = page[-1]["height"]
        if read % PROGRESS_EVERY < PAGE:
            log({"case": "M-3-headers", "status": "INFO", "progress": read,
                 "atHeight": last_h}, fh)
        if len(page) < want or last_h >= to_h:
            if last_h < to_h and len(page) < want:
                short = True
            break
        h = last_h + 1
    status = "FAIL" if (link_breaks or short or last_h != to_h) else "PASS"
    log({"case": "M-3-headers", "status": status,
         "from": from_h, "momentumsRead": read, "toHeight": last_h,
         "statedTo": to_h, "shortRead": short,
         "linkBreaks": link_breaks[:20], "linkBreakCount": len(link_breaks)}, fh)
    return read, link_breaks, (status == "PASS")


def abi_first_string(raw):
    """Decode the first `string` argument of an ABI call payload (offsets
    are relative to the head, which starts right after the 4-byte
    selector)."""
    if len(raw) < 36:
        return None
    off = int.from_bytes(raw[4:36], "big")
    start = 4 + off  # head begins at byte 4
    if start + 32 > len(raw):
        return None
    ln = int.from_bytes(raw[start:start + 32], "big")
    if start + 32 + ln > len(raw):
        return None
    return raw[start + 32:start + 32 + ln]


def secp256k1_decompress_coords(dec):
    """Return (x, y) iff dec is a valid secp256k1 public key on the curve:
    a 65-byte uncompressed (0x04 || X || Y) or a 33-byte compressed
    (0x02/0x03 || X) point.  For compressed points, y is selected with the
    parity matching dec[0] & 1 (otherwise SECP256K1_P - y).  Returns None
    if dec is off-curve or not a recognized format."""
    if len(dec) == 65 and dec[0] == 4:
        x = int.from_bytes(dec[1:33], "big")
        y = int.from_bytes(dec[33:65], "big")
        if x >= SECP256K1_P or y >= SECP256K1_P:
            return None
        if (y * y - (pow(x, 3, SECP256K1_P) + 7)) % SECP256K1_P != 0:
            return None
        return (x, y)
    if len(dec) == 33 and dec[0] in (2, 3):
        x = int.from_bytes(dec[1:33], "big")
        if x >= SECP256K1_P:
            return None
        rhs = (pow(x, 3, SECP256K1_P) + 7) % SECP256K1_P
        # p % 4 == 3, so the square root is rhs**((p+1)/4) mod p.
        y = pow(rhs, (SECP256K1_P + 1) // 4, SECP256K1_P)
        if (y * y) % SECP256K1_P != rhs:
            return None  # no square root: not on the curve at all
        if (y & 1) != (dec[0] & 1):
            y = SECP256K1_P - y
        if y >= SECP256K1_P or (y & 1) != (dec[0] & 1):
            return None
        return (x, y)
    return None


def secp256k1_on_curve(dec):
    """True iff dec is a secp256k1 public key on the curve: a 65-byte
    uncompressed (0x04 || X || Y) or a 33-byte compressed (0x02/0x03 || X)
    point.  False for a 33-byte compressed-length key (including invalid
    prefix bytes such as 33 zero bytes) or a 65-byte 0x04-prefixed key that
    is not on the curve.  None if it is neither format."""
    if secp256k1_decompress_coords(dec) is not None:
        return True
    if len(dec) == 33 or (len(dec) == 65 and dec[0] == 4):
        return False
    return None  # not a key we recognize


def check_live_bridge_key(endpoint, fh):
    """Query embedded.bridge.getBridgeInfo and verify decompressedTssECDSAPubKey.

    If empty (uninitialized bridge), logs INFO with no finding and no failure.
    If non-empty, verifies it base64-decodes to exactly 65 bytes with prefix
    0x04 and lies on secp256k1.  An invalid or short stored key is logged and
    counted as a finding without failing the scan; a malformed RPC payload
    counts as a parse failure.  Returns (findings, parse_failures, key_check)."""
    info = rpc(endpoint, "embedded.bridge.getBridgeInfo")
    if not isinstance(info, dict):
        log({"case": "M-3-liveBridgeKey", "status": "FAIL",
             "finding": False, "keyCheck": "invalid-response"}, fh)
        return 0, 1, "invalid-response"

    decomp_b64 = info.get("decompressedTssECDSAPubKey")
    if decomp_b64 is None or decomp_b64 == "":
        log({"case": "M-3-liveBridgeKey", "status": "INFO",
             "finding": False, "keyCheck": "empty", "byteLength": 0}, fh)
        return 0, 0, "empty"

    if not isinstance(decomp_b64, str):
        log({"case": "M-3-liveBridgeKey", "status": "FAIL",
             "finding": False, "keyCheck": "invalid-response"}, fh)
        return 0, 1, "invalid-response"

    pub_sha = hashlib.sha256(decomp_b64.encode()).hexdigest()[:16]
    try:
        raw = base64.b64decode(decomp_b64, validate=True)
    except Exception:
        log({"case": "M-3-liveBridgeKey", "status": "INFO",
             "finding": True, "keyCheck": "invalid-base64",
             "byteLength": None, "pubKeySha256": pub_sha}, fh)
        return 1, 0, "invalid-base64"

    if len(raw) != 65:
        log({"case": "M-3-liveBridgeKey", "status": "INFO",
             "finding": True, "keyCheck": "invalid-length",
             "byteLength": len(raw), "expectedLength": 65,
             "pubKeySha256": pub_sha}, fh)
        return 1, 0, "invalid-length"

    if raw[0] != 4:
        log({"case": "M-3-liveBridgeKey", "status": "INFO",
             "finding": True, "keyCheck": "invalid-prefix",
             "byteLength": len(raw), "prefix": f"0x{raw[0]:02x}",
             "pubKeySha256": pub_sha}, fh)
        return 1, 0, "invalid-prefix"

    coords = secp256k1_decompress_coords(raw)
    if coords is None:
        log({"case": "M-3-liveBridgeKey", "status": "INFO",
             "finding": True, "keyCheck": "OFF-CURVE",
             "byteLength": len(raw), "pubKeySha256": pub_sha}, fh)
        return 1, 0, "OFF-CURVE"

    x_short = coords[0] < SHORT_COORD_THRESHOLD
    y_short = coords[1] < SHORT_COORD_THRESHOLD
    log({"case": "M-3-liveBridgeKey", "status": "PASS",
         "finding": False, "keyCheck": "valid",
         "byteLength": len(raw), "pubKeySha256": pub_sha,
         "shortCoord": x_short or y_short,
         "leadingZeroCoord": {"x": x_short, "y": y_short}}, fh)
    return 0, 0, "valid"


def scan_blocks(endpoint, from_h, to_h, fh):
    """Scan full account blocks over a height range. Momentum 1 is skipped:
    getDetailedMomentumsByHeight does not answer at height 1 on full-history
    nodes (getMomentumByHash does); the effective range is logged so the
    exclusion is visible. The last page is capped at to_h; anything short
    is a FAIL, and every returned momentum must be exactly the expected
    height.

    Integrity problems (short read, wrong heights, parse or decode
    failures) FAIL the scan.  #78 findings (an off-curve, unrecognized, or
    leading-zero-coordinate bridge key, an invalid/short live stored bridge
    key, or a difficulty >= 2^63) are listed and counted but do not by
    themselves fail it (docs/TEST-PLAN.md M-3: "none found, or each one
    listed with the height where dev and v0.0.10 would disagree").  Blocks
    above DP_MAX_DIFFICULTY and LEGACY_MAX_DIFFICULTY are also counted for
    visibility ahead of DynamicPlasmaSpork activation."""
    momentums = 0
    blocks_read = 0
    bridge_keys = []
    high_diff = []
    above_dp_max = 0
    above_dp_entries = []
    above_legacy_max = 0
    above_legacy_sample = []
    max_diff = 0
    parse_failures = 0
    bad_pages = []
    h = from_h
    if h == 1:
        log({"case": "M-3-blocks", "status": "INFO",
             "detail": "momentum-1 excluded: getDetailedMomentumsByHeight does not "
                       "answer at height 1 on full-history nodes; read it via "
                       "getMomentumByHash if ever needed"}, fh)
        h = 2
    from_eff = h
    last_h = h - 1
    short = False
    while h <= to_h:
        want = min(DETAILED, to_h - h + 1)
        res = rpc(endpoint, "ledger.getDetailedMomentumsByHeight", [h, want])
        page = res.get("list") or []
        # dev caps the encoded answer at 10 MB: when the next momentum
        # would pass the budget the page is short and "more" is true, and
        # the caller continues after the last returned momentum.  A short
        # page with more:true is budget-capped, not a short read.
        more = bool(res.get("more"))
        if not page:
            if more:
                bad_pages.append({"detail": "more:true with an empty page",
                                  "from": h})
            else:
                short = True
            break
        for i, e in enumerate(page):
            mh = (e.get("momentum") or {}).get("height")
            if mh != h + i:
                bad_pages.append({"detail": "unexpected height in page",
                                  "atIndex": i, "expected": h + i, "got": mh})
        for e in page:
            m = e["momentum"]
            momentums += 1
            for b in e.get("blocks") or []:
                blocks_read += 1
                try:
                    bdiff = int(b.get("difficulty", "0") or 0)
                except (ValueError, TypeError):
                    bdiff = 0
                    parse_failures += 1
                if bdiff > max_diff:
                    max_diff = bdiff
                if bdiff >= HIGH_DIFF_THRESHOLD:
                    high_diff.append({"kind": "block", "height": m.get("height"),
                                      "hash": b.get("hash"),
                                      "difficulty": b.get("difficulty")})
                if bdiff > DP_MAX_DIFFICULTY:
                    above_dp_max += 1
                    if len(above_dp_entries) < DIFF_LOG_CAP:
                        above_dp_entries.append({"kind": "block",
                                                 "height": m.get("height"),
                                                 "hash": b.get("hash"),
                                                 "difficulty": b.get("difficulty")})
                if bdiff > LEGACY_MAX_DIFFICULTY:
                    above_legacy_max += 1
                    if len(above_legacy_sample) < 20:
                        above_legacy_sample.append({"height": m.get("height"),
                                                    "hash": b.get("hash"),
                                                    "difficulty": b.get("difficulty")})
                data = b.get("data") or ""
                raw = b""
                try:
                    raw = base64.b64decode(data)
                except Exception:
                    parse_failures += 1
                if (raw[:4] == BRIDGE_KEY_SELECTOR
                        and b.get("toAddress") == BRIDGE_CONTRACT
                        and b.get("blockType") == USER_SEND):
                    key_b64 = abi_first_string(raw)
                    key_status = "not-decodable"
                    short_coord = False
                    leading_zero = None
                    if key_b64:
                        try:
                            key_dec = base64.b64decode(key_b64, validate=True)
                            coords = secp256k1_decompress_coords(key_dec)
                            if coords is not None:
                                key_status = "on-curve"
                                x_short = coords[0] < SHORT_COORD_THRESHOLD
                                y_short = coords[1] < SHORT_COORD_THRESHOLD
                                short_coord = x_short or y_short
                                leading_zero = {"x": x_short, "y": y_short}
                            else:
                                on_curve = secp256k1_on_curve(key_dec)
                                key_status = ("OFF-CURVE" if on_curve is False else
                                              "unexpected-key-format")
                        except Exception:
                            key_status = "not-decodable"
                    if key_status == "not-decodable":
                        # A key that cannot be decoded cannot be checked:
                        # that is an integrity failure, not a finding.
                        parse_failures += 1
                    bridge_keys.append({"height": m.get("height"),
                                        "momentumHash": m.get("hash"),
                                        "address": b.get("address"),
                                        "blockHash": b.get("hash"),
                                        "selector": BRIDGE_KEY_SELECTOR.hex(),
                                        "pubKeySha256": key_b64 and
                                            hashlib.sha256(key_b64).hexdigest()[:16],
                                        "keyCheck": key_status,
                                        "shortCoord": short_coord,
                                        "leadingZeroCoord": leading_zero})
        if not isinstance(page[-1]["momentum"].get("height"), int) \
                or page[-1]["momentum"]["height"] < h:
            # No forward progress (garbage heights): the bad page above
            # already fails the scan; stop instead of looping.
            break
        last_h = page[-1]["momentum"]["height"]
        if momentums % PROGRESS_EVERY < DETAILED:
            log({"case": "M-3-blocks", "status": "INFO", "momentums": momentums,
                 "blocks": blocks_read, "atHeight": last_h}, fh)
        if last_h >= to_h:
            break
        if more:
            h = last_h + 1
            continue
        if len(page) < want:
            short = True
            break
        h = last_h + 1
    for k in bridge_keys:
        log({"case": "M-3-bridgeKey", **k}, fh)
    for d in high_diff:
        log({"case": "M-3-highDifficulty", **d}, fh)
    for d in above_dp_entries:
        log({"case": "M-3-aboveDpMaxDifficulty", **d}, fh)
    live_findings, live_parse_failures, live_key_check = check_live_bridge_key(
        endpoint, fh)
    parse_failures += live_parse_failures
    off_curve = sum(1 for k in bridge_keys if k["keyCheck"] == "OFF-CURVE")
    odd_format = sum(1 for k in bridge_keys
                     if k["keyCheck"] == "unexpected-key-format")
    short_coord_keys = sum(1 for k in bridge_keys if k["shortCoord"])
    findings = (off_curve + odd_format + short_coord_keys
                + live_findings + len(high_diff))
    status = "FAIL" if (short or last_h != to_h or parse_failures
                        or bad_pages) else "PASS"
    log({"case": "M-3-blocks", "status": status, "from": from_eff,
         "momentumsRead": momentums, "accountBlocksRead": blocks_read,
         "toHeight": last_h, "statedTo": to_h, "shortRead": short,
         "bridgeKeySends": len(bridge_keys),
         "distinctBridgeKeys": len({k["pubKeySha256"] for k in bridge_keys}),
         "offCurveKeys": off_curve,
         "unexpectedKeyFormats": odd_format,
         "shortCoordKeys": short_coord_keys,
         "liveBridgeKeyCheck": live_key_check,
         "liveBridgeKeyFindings": live_findings,
         "highDifficultyItems": len(high_diff),
         "aboveDpMaxDifficulty": above_dp_max,
         "aboveLegacyMaxDifficulty": above_legacy_max,
         "aboveLegacyMaxDifficultySample": above_legacy_sample,
         "maxDifficulty": max_diff,
         "findings": findings,
         "badPages": bad_pages[:20], "badPageCount": len(bad_pages),
         "parseFailures": parse_failures}, fh)
    return momentums, blocks_read, findings, (status == "PASS")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--node", default="http://127.0.0.1:35997")
    ap.add_argument("--headers", action="store_true", help="walk + link-check headers 1..--to")
    ap.add_argument("--blocks", action="store_true", help="scan full blocks --from..--to")
    ap.add_argument("--from", dest="from_h", type=int, default=1)
    ap.add_argument("--to", dest="to_h", type=int, default=0,
                    help="stated end height (0 = current frontier)")
    ap.add_argument("--out", default="")
    args = ap.parse_args()

    if not args.headers and not args.blocks:
        sys.exit("Specify --headers and/or --blocks")
    if args.from_h < 1:
        sys.exit(f"--from must be >= 1, got {args.from_h}")

    gate(args.node)
    frontier = rpc(args.node, "ledger.getFrontierMomentum")["height"]
    if args.to_h and args.to_h > frontier:
        # Clamping to the frontier would silently pass below the stated
        # height; refuse instead.
        sys.exit(f"--to {args.to_h} is above the frontier {frontier}: the scan "
                 f"cannot reach the stated height")
    to_h = args.to_h or frontier
    if args.from_h > to_h:
        sys.exit(f"inverted range: --from {args.from_h} > --to {to_h} "
                 f"(the scan would read 0 momentums and pass)")

    tmpdir = None
    if args.out:
        path = args.out
    else:
        # A unique directory per run: two slices without --out must not
        # clobber each other's results.
        tmpdir = tempfile.mkdtemp(prefix="scan-")
        path = os.path.join(tmpdir, "scan-results.jsonl")
        print(f"writing to {path} (use --out to choose the location)",
              file=sys.stderr)
    with open(path, "w") as fh:
        log({"case": "header", "tool": "test/mainnet/scan.py",
             "toolCommit": tool_commit(),
             "startedUtc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
             "node": args.node, "frontier": frontier, "to": to_h,
             "bridgeKeySelector": BRIDGE_KEY_SELECTOR.hex(),
             "bridgeKeySig": BRIDGE_KEY_SIG}, fh)
        log({"case": "gate", "status": "PASS",
             "detail": "chain id 1 and mainnet genesis momentum verified"}, fh)
        fail = 0
        findings = 0
        if args.headers:
            _, breaks, ok = scan_headers(args.node, args.from_h, to_h, fh)
            fail += len(breaks)
            if not ok:
                fail += 1
        if args.blocks:
            _, _, f, ok = scan_blocks(args.node, args.from_h, to_h, fh)
            findings += f
            if not ok:
                fail += 1
        log({"case": "summary", "fail": fail, "findings": findings}, fh)
    if not args.out:
        with open(path) as fh:
            sys.stdout.write(fh.read())
        shutil.rmtree(tmpdir, ignore_errors=True)
    else:
        print(f"Wrote {path}", file=sys.stderr)
    sys.exit(1 if fail else 0)


if __name__ == "__main__":
    main()
