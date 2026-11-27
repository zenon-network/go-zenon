#!/usr/bin/env python3
"""Unit tests for test/mainnet/scan.py.

Canned observations only — no node, no network.  Run from the repository
root either directly:

    python3 test/mainnet/test_scan.py -v

or through unittest discovery:

    python3 -m unittest discover -s test/mainnet -v
"""
import base64
import io
import json
import os
import sys
import unittest
from unittest import mock

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import scan  # noqa: E402

# secp256k1 generator G (both X and Y >= 2^248; Y is even).
SECP256K1_GX = 0x79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798
SECP256K1_GY = 0x483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8

# Repo bridge fixtures (vm/embedded/implementation/bridge_test.go &
# vm/embedded/tests/z_bridge_test.go).
VALID_COMPRESSED_B64 = "AhOiqdjx002Cj8o1jxTM5LqywbgNFZwUPJuR9ffdQwFP"
SHORT_X_COMPRESSED_B64 = "AgB3WPsbAbaMAZdHlFw58Q8936u9QISPZ358k3ze48Zm"

# Deterministic secp256k1 points with X >= 2^248 and one Y parity < 2^248:
# - SHORT_Y_ROOT_*: primary square root y = rhs**((p+1)/4) mod p is < 2^248
#   (even, prefix 0x02), while p - y is >= 2^248 (odd, prefix 0x03).
SHORT_Y_ROOT_X = 0x01000000000000000000000000000000000000000000000000000000000000A4
SHORT_Y_ROOT_Y = 0x002B873E29F2E595CDCB4F6716C2498A340710BB1E3A875B76D08CF9D1974326

# - SHORT_Y_NEG_*: primary square root y is >= 2^248 (odd, prefix 0x03), while
#   the negated root p - y is < 2^248 (even, prefix 0x02).
SHORT_Y_NEG_X = 0x01000000000000000000000000000000000000000000000000000000000000AA
SHORT_Y_NEG_Y = 0x001FC55B7307C0EF96B96254CA351CE50608D0F95E5954D578F44A19224200E4

# x < SECP256K1_P where x^3 + 7 is a quadratic non-residue mod p.
NON_RESIDUE_X = 0x0100000000000000000000000000000000000000000000000000000000000004


def pack_change_tss_key(pub_key_bytes):
    """Build a ChangeTssECDSAPubKey(string,string,string) ABI payload with
    pub_key_bytes as the first string argument (matches scan.abi_first_string)."""
    head = (
        (96).to_bytes(32, "big")
        + (96 + 32 + ((len(pub_key_bytes) + 31) // 32) * 32).to_bytes(32, "big")
        + (96 + 32 + ((len(pub_key_bytes) + 31) // 32) * 32 + 32).to_bytes(32, "big")
    )
    pad = (-len(pub_key_bytes)) % 32
    tail1 = len(pub_key_bytes).to_bytes(32, "big") + pub_key_bytes + b"\x00" * pad
    empty = (0).to_bytes(32, "big")
    return scan.BRIDGE_KEY_SELECTOR + head + tail1 + empty + empty


def make_bridge_key_block(pub_key_b64, block_hash="bhash", difficulty="0"):
    if isinstance(pub_key_b64, str):
        pub_key_b64 = pub_key_b64.encode()
    payload = pack_change_tss_key(pub_key_b64)
    return {
        "hash": block_hash,
        "address": "z1qsender",
        "toAddress": scan.BRIDGE_CONTRACT,
        "blockType": scan.USER_SEND,
        "difficulty": difficulty,
        "data": base64.b64encode(payload).decode(),
    }


def make_plain_block(block_hash, difficulty="0"):
    return {
        "hash": block_hash,
        "address": "z1qsender",
        "toAddress": "z1qrecipient",
        "blockType": scan.USER_SEND,
        "difficulty": difficulty,
        "data": "",
    }


def run_scan_blocks(pages_by_height, bridge_info, from_h=2, to_h=2):
    """Execute scan.scan_blocks with mocked RPC responses and parse the JSONL."""
    def fake_rpc(_endpoint, method, params=None):
        if method == "ledger.getDetailedMomentumsByHeight":
            h, want = params
            items = []
            for cur in range(h, h + want):
                if cur in pages_by_height:
                    items.append(pages_by_height[cur])
            return {"list": items, "more": False}
        if method == "embedded.bridge.getBridgeInfo":
            return bridge_info
        raise AssertionError(f"unexpected RPC call: {method} {params}")

    buf = io.StringIO()
    with mock.patch.object(scan, "rpc", side_effect=fake_rpc):
        res = scan.scan_blocks("http://mock", from_h, to_h, buf)
    records = [json.loads(line) for line in buf.getvalue().splitlines()]
    return res, records


def uncompressed_b64(x, y):
    raw = b"\x04" + x.to_bytes(32, "big") + y.to_bytes(32, "big")
    return base64.b64encode(raw).decode()


class Secp256k1DecompressAndCurve(unittest.TestCase):
    def test_compressed_normal_coordinates(self):
        comp_even = b"\x02" + SECP256K1_GX.to_bytes(32, "big")
        comp_odd = b"\x03" + SECP256K1_GX.to_bytes(32, "big")

        self.assertTrue(scan.secp256k1_on_curve(comp_even))
        self.assertEqual(
            scan.secp256k1_decompress_coords(comp_even),
            (SECP256K1_GX, SECP256K1_GY),
        )
        self.assertTrue(scan.secp256k1_on_curve(comp_odd))
        self.assertEqual(
            scan.secp256k1_decompress_coords(comp_odd),
            (SECP256K1_GX, scan.SECP256K1_P - SECP256K1_GY),
        )

        # Repo fixture key should decompress with both coordinates >= 2^248.
        fixture_dec = base64.b64decode(VALID_COMPRESSED_B64)
        self.assertTrue(scan.secp256k1_on_curve(fixture_dec))
        coords = scan.secp256k1_decompress_coords(fixture_dec)
        self.assertIsNotNone(coords)
        self.assertGreaterEqual(coords[0], scan.SHORT_COORD_THRESHOLD)
        self.assertGreaterEqual(coords[1], scan.SHORT_COORD_THRESHOLD)
        self.assertEqual(coords[1] & 1, fixture_dec[0] & 1)

    def test_compressed_leading_zero_x(self):
        dec = base64.b64decode(SHORT_X_COMPRESSED_B64)
        self.assertTrue(scan.secp256k1_on_curve(dec))
        coords = scan.secp256k1_decompress_coords(dec)
        self.assertIsNotNone(coords)
        x, y = coords
        self.assertLess(x, scan.SHORT_COORD_THRESHOLD)
        self.assertGreaterEqual(y, scan.SHORT_COORD_THRESHOLD)
        self.assertEqual(y & 1, dec[0] & 1)
        self.assertEqual((y * y - (pow(x, 3, scan.SECP256K1_P) + 7)) % scan.SECP256K1_P, 0)

    def test_compressed_leading_zero_y_parity_selection(self):
        # Verify parity selection picks y vs SECP256K1_P - y in BOTH cases:
        # 1) primary sqrt is < 2^248; 2) negated sqrt (p - y) is < 2^248.
        for x, y_small in (
            (SHORT_Y_ROOT_X, SHORT_Y_ROOT_Y),
            (SHORT_Y_NEG_X, SHORT_Y_NEG_Y),
        ):
            y_large = scan.SECP256K1_P - y_small
            self.assertGreaterEqual(x, scan.SHORT_COORD_THRESHOLD)
            self.assertLess(y_small, scan.SHORT_COORD_THRESHOLD)
            self.assertGreaterEqual(y_large, scan.SHORT_COORD_THRESHOLD)

            small_prefix = bytes([2 + (y_small & 1)])
            large_prefix = bytes([2 + (y_large & 1)])
            self.assertNotEqual(small_prefix, large_prefix)

            key_small = small_prefix + x.to_bytes(32, "big")
            key_large = large_prefix + x.to_bytes(32, "big")

            self.assertEqual(scan.secp256k1_decompress_coords(key_small), (x, y_small))
            self.assertEqual(scan.secp256k1_decompress_coords(key_large), (x, y_large))
            self.assertTrue(scan.secp256k1_on_curve(key_small))
            self.assertTrue(scan.secp256k1_on_curve(key_large))

    def test_compressed_odd_leading_zero_y(self):
        # Also verify an odd short Y (prefix 0x03 is short, 0x02 is normal).
        # Find the smallest x >= 2^248 with an odd y < 2^248 on secp256k1.
        p = scan.SECP256K1_P
        x = scan.SHORT_COORD_THRESHOLD
        found = None
        while found is None:
            rhs = (pow(x, 3, p) + 7) % p
            y = pow(rhs, (p + 1) // 4, p)
            if (y * y) % p == rhs:
                for cand in (y, p - y):
                    if cand < scan.SHORT_COORD_THRESHOLD and (cand & 1) == 1:
                        found = (x, cand)
                        break
            x += 1
        fx, fy_odd = found
        key_odd = b"\x03" + fx.to_bytes(32, "big")
        key_even = b"\x02" + fx.to_bytes(32, "big")
        self.assertEqual(scan.secp256k1_decompress_coords(key_odd), (fx, fy_odd))
        self.assertEqual(scan.secp256k1_decompress_coords(key_even), (fx, p - fy_odd))
        self.assertLess(fy_odd, scan.SHORT_COORD_THRESHOLD)
        self.assertGreaterEqual(p - fy_odd, scan.SHORT_COORD_THRESHOLD)

    def test_uncompressed_keys(self):
        uncomp_g = b"\x04" + SECP256K1_GX.to_bytes(32, "big") + SECP256K1_GY.to_bytes(32, "big")
        self.assertTrue(scan.secp256k1_on_curve(uncomp_g))
        self.assertEqual(scan.secp256k1_decompress_coords(uncomp_g), (SECP256K1_GX, SECP256K1_GY))

        uncomp_short_y = (
            b"\x04"
            + SHORT_Y_NEG_X.to_bytes(32, "big")
            + SHORT_Y_NEG_Y.to_bytes(32, "big")
        )
        self.assertTrue(scan.secp256k1_on_curve(uncomp_short_y))
        coords = scan.secp256k1_decompress_coords(uncomp_short_y)
        self.assertEqual(coords, (SHORT_Y_NEG_X, SHORT_Y_NEG_Y))
        self.assertLess(coords[1], scan.SHORT_COORD_THRESHOLD)

    def test_off_curve_keys_including_33_zero_bytes(self):
        # 33 zero bytes: length matches compressed key, fails DecompressPubkey.
        zero33 = b"\x00" * 33
        self.assertIsNone(scan.secp256k1_decompress_coords(zero33))
        self.assertIs(scan.secp256k1_on_curve(zero33), False)

        # 33 bytes with invalid prefix byte (0x04 or 0x01).
        bad_prefix33 = b"\x04" + SECP256K1_GX.to_bytes(32, "big")
        self.assertIsNone(scan.secp256k1_decompress_coords(bad_prefix33))
        self.assertIs(scan.secp256k1_on_curve(bad_prefix33), False)

        # 33 bytes with x >= SECP256K1_P.
        max_x = b"\x02" + b"\xff" * 32
        self.assertIsNone(scan.secp256k1_decompress_coords(max_x))
        self.assertIs(scan.secp256k1_on_curve(max_x), False)

        eq_p = b"\x02" + scan.SECP256K1_P.to_bytes(32, "big")
        self.assertIsNone(scan.secp256k1_decompress_coords(eq_p))
        self.assertIs(scan.secp256k1_on_curve(eq_p), False)

        # 33 bytes with x < p but x^3 + 7 not a quadratic residue.
        non_res = b"\x02" + NON_RESIDUE_X.to_bytes(32, "big")
        self.assertIsNone(scan.secp256k1_decompress_coords(non_res))
        self.assertIs(scan.secp256k1_on_curve(non_res), False)

        # 65-byte uncompressed off-curve point.
        off_uncomp = (
            b"\x04"
            + SECP256K1_GX.to_bytes(32, "big")
            + (SECP256K1_GY + 1).to_bytes(32, "big")
        )
        self.assertIsNone(scan.secp256k1_decompress_coords(off_uncomp))
        self.assertIs(scan.secp256k1_on_curve(off_uncomp), False)

        # 65-byte uncompressed with out-of-field coordinate.
        oob_uncomp = b"\x04" + b"\xff" * 32 + SECP256K1_GY.to_bytes(32, "big")
        self.assertIsNone(scan.secp256k1_decompress_coords(oob_uncomp))
        self.assertIs(scan.secp256k1_on_curve(oob_uncomp), False)

    def test_bad_formats(self):
        for bad in (
            b"",
            b"\x02" * 32,
            b"\x02" * 34,
            b"\x04" + b"\x01" * 63,  # 64-byte truncated uncompressed key
            b"\x05" + SECP256K1_GX.to_bytes(32, "big") + SECP256K1_GY.to_bytes(32, "big"),
            b"not-a-key",
        ):
            self.assertIsNone(scan.secp256k1_decompress_coords(bad), bad)
            self.assertIsNone(scan.secp256k1_on_curve(bad), bad)


class ScanBlocksBridgeKeys(unittest.TestCase):
    def _valid_bridge_info(self):
        return {"decompressedTssECDSAPubKey": uncompressed_b64(SECP256K1_GX, SECP256K1_GY)}

    def test_short_x_and_short_y_and_normal_keys(self):
        short_y_b64 = base64.b64encode(
            b"\x02" + SHORT_Y_NEG_X.to_bytes(32, "big")
        ).decode()
        normal_y_same_x_b64 = base64.b64encode(
            b"\x03" + SHORT_Y_NEG_X.to_bytes(32, "big")
        ).decode()

        pages = {
            2: {
                "momentum": {"height": 2, "hash": "mh2"},
                "blocks": [
                    make_bridge_key_block(VALID_COMPRESSED_B64, "b-normal"),
                    make_bridge_key_block(SHORT_X_COMPRESSED_B64, "b-short-x"),
                    make_bridge_key_block(short_y_b64, "b-short-y"),
                    make_bridge_key_block(normal_y_same_x_b64, "b-normal-y"),
                ],
            }
        }
        (momentums, blocks_read, findings, ok), records = run_scan_blocks(
            pages, self._valid_bridge_info(), from_h=2, to_h=2
        )
        self.assertTrue(ok)
        self.assertEqual(momentums, 1)
        self.assertEqual(blocks_read, 4)
        self.assertEqual(findings, 2)

        by_hash = {
            r["blockHash"]: r for r in records if r["case"] == "M-3-bridgeKey"
        }
        self.assertEqual(by_hash["b-normal"]["keyCheck"], "on-curve")
        self.assertFalse(by_hash["b-normal"]["shortCoord"])
        self.assertEqual(by_hash["b-normal"]["leadingZeroCoord"], {"x": False, "y": False})

        self.assertEqual(by_hash["b-short-x"]["keyCheck"], "on-curve")
        self.assertTrue(by_hash["b-short-x"]["shortCoord"])
        self.assertEqual(by_hash["b-short-x"]["leadingZeroCoord"], {"x": True, "y": False})

        self.assertEqual(by_hash["b-short-y"]["keyCheck"], "on-curve")
        self.assertTrue(by_hash["b-short-y"]["shortCoord"])
        self.assertEqual(by_hash["b-short-y"]["leadingZeroCoord"], {"x": False, "y": True})

        self.assertEqual(by_hash["b-normal-y"]["keyCheck"], "on-curve")
        self.assertFalse(by_hash["b-normal-y"]["shortCoord"])
        self.assertEqual(by_hash["b-normal-y"]["leadingZeroCoord"], {"x": False, "y": False})

        summary = [r for r in records if r["case"] == "M-3-blocks"][-1]
        self.assertEqual(summary["status"], "PASS")
        self.assertEqual(summary["bridgeKeySends"], 4)
        self.assertEqual(summary["offCurveKeys"], 0)
        self.assertEqual(summary["unexpectedKeyFormats"], 0)
        self.assertEqual(summary["shortCoordKeys"], 2)
        self.assertEqual(summary["findings"], 2)

    def test_off_curve_33_zeros_and_unexpected_format(self):
        zero33_b64 = base64.b64encode(b"\x00" * 33).decode()
        bad32_b64 = base64.b64encode(b"\x02" * 32).decode()
        pages = {
            2: {
                "momentum": {"height": 2, "hash": "mh2"},
                "blocks": [
                    make_bridge_key_block(zero33_b64, "b-zero33"),
                    make_bridge_key_block(bad32_b64, "b-bad32"),
                ],
            }
        }
        (_, _, findings, ok), records = run_scan_blocks(
            pages, self._valid_bridge_info(), from_h=2, to_h=2
        )
        self.assertTrue(ok)
        self.assertEqual(findings, 2)
        by_hash = {r["blockHash"]: r for r in records if r["case"] == "M-3-bridgeKey"}
        self.assertEqual(by_hash["b-zero33"]["keyCheck"], "OFF-CURVE")
        self.assertFalse(by_hash["b-zero33"]["shortCoord"])
        self.assertIsNone(by_hash["b-zero33"]["leadingZeroCoord"])
        self.assertEqual(by_hash["b-bad32"]["keyCheck"], "unexpected-key-format")
        summary = [r for r in records if r["case"] == "M-3-blocks"][-1]
        self.assertEqual(summary["offCurveKeys"], 1)
        self.assertEqual(summary["unexpectedKeyFormats"], 1)
        self.assertEqual(summary["shortCoordKeys"], 0)
        self.assertEqual(summary["findings"], 2)

    def test_non_base64_bridge_key_is_parse_failure(self):
        pages = {
            2: {
                "momentum": {"height": 2, "hash": "mh2"},
                "blocks": [make_bridge_key_block("!!!not-base64!!!", "b-badb64")],
            }
        }
        (_, _, findings, ok), records = run_scan_blocks(
            pages, self._valid_bridge_info(), from_h=2, to_h=2
        )
        self.assertFalse(ok)
        self.assertEqual(findings, 0)
        summary = [r for r in records if r["case"] == "M-3-blocks"][-1]
        self.assertEqual(summary["status"], "FAIL")
        self.assertEqual(summary["parseFailures"], 1)


class ScanBlocksLiveBridgeKey(unittest.TestCase):
    def _empty_page(self):
        return {2: {"momentum": {"height": 2, "hash": "mh2"}, "blocks": []}}

    def test_valid_65_byte_live_key(self):
        info = {"decompressedTssECDSAPubKey": uncompressed_b64(SECP256K1_GX, SECP256K1_GY)}
        (_, _, findings, ok), records = run_scan_blocks(self._empty_page(), info)
        self.assertTrue(ok)
        self.assertEqual(findings, 0)
        live = [r for r in records if r["case"] == "M-3-liveBridgeKey"]
        self.assertEqual(len(live), 1)
        self.assertEqual(live[0]["status"], "PASS")
        self.assertEqual(live[0]["keyCheck"], "valid")
        self.assertEqual(live[0]["byteLength"], 65)
        self.assertFalse(live[0]["finding"])
        self.assertFalse(live[0]["shortCoord"])
        summary = [r for r in records if r["case"] == "M-3-blocks"][-1]
        self.assertEqual(summary["liveBridgeKeyCheck"], "valid")
        self.assertEqual(summary["liveBridgeKeyFindings"], 0)

    def test_valid_65_byte_padded_short_coord_live_key(self):
        # A properly left-padded 65-byte stored key whose X has a leading zero
        # byte is valid (length 65, 0x04 prefix, on-curve) -> 0 findings.
        x, y = scan.secp256k1_decompress_coords(base64.b64decode(SHORT_X_COMPRESSED_B64))
        info = {"decompressedTssECDSAPubKey": uncompressed_b64(x, y)}
        (_, _, findings, ok), records = run_scan_blocks(self._empty_page(), info)
        self.assertTrue(ok)
        self.assertEqual(findings, 0)
        live = [r for r in records if r["case"] == "M-3-liveBridgeKey"][0]
        self.assertEqual(live["status"], "PASS")
        self.assertEqual(live["keyCheck"], "valid")
        self.assertEqual(live["byteLength"], 65)
        self.assertFalse(live["finding"])
        self.assertTrue(live["shortCoord"])
        self.assertEqual(live["leadingZeroCoord"], {"x": True, "y": False})

    def test_short_64_byte_live_key_is_finding(self):
        # Simulate unpadded storage of a leading-zero-X key: 0x04 || X[31] || Y[32].
        x, y = scan.secp256k1_decompress_coords(base64.b64decode(SHORT_X_COMPRESSED_B64))
        x_minimal = x.to_bytes((x.bit_length() + 7) // 8, "big")
        self.assertEqual(len(x_minimal), 31)
        raw64 = b"\x04" + x_minimal + y.to_bytes(32, "big")
        self.assertEqual(len(raw64), 64)
        info = {"decompressedTssECDSAPubKey": base64.b64encode(raw64).decode()}

        (_, _, findings, ok), records = run_scan_blocks(self._empty_page(), info)
        self.assertTrue(ok)
        self.assertEqual(findings, 1)
        live = [r for r in records if r["case"] == "M-3-liveBridgeKey"][0]
        self.assertEqual(live["keyCheck"], "invalid-length")
        self.assertEqual(live["byteLength"], 64)
        self.assertEqual(live["expectedLength"], 65)
        self.assertTrue(live["finding"])
        summary = [r for r in records if r["case"] == "M-3-blocks"][-1]
        self.assertEqual(summary["status"], "PASS")
        self.assertEqual(summary["liveBridgeKeyCheck"], "invalid-length")
        self.assertEqual(summary["liveBridgeKeyFindings"], 1)
        self.assertEqual(summary["findings"], 1)

    def test_empty_live_key_on_uninitialized_bridge_passes(self):
        info = {"decompressedTssECDSAPubKey": ""}
        (_, _, findings, ok), records = run_scan_blocks(self._empty_page(), info)
        self.assertTrue(ok)
        self.assertEqual(findings, 0)
        live = [r for r in records if r["case"] == "M-3-liveBridgeKey"][0]
        self.assertEqual(live["status"], "INFO")
        self.assertEqual(live["keyCheck"], "empty")
        self.assertEqual(live["byteLength"], 0)
        self.assertFalse(live["finding"])

    def test_invalid_prefix_off_curve_and_bad_base64_live_key(self):
        bad_prefix = base64.b64encode(
            b"\x02" + SECP256K1_GX.to_bytes(32, "big") + SECP256K1_GY.to_bytes(32, "big")
        ).decode()
        off_curve = uncompressed_b64(SECP256K1_GX, SECP256K1_GY + 1)

        for val, expected_check in (
            (bad_prefix, "invalid-prefix"),
            (off_curve, "OFF-CURVE"),
            ("!!!not-base64!!!", "invalid-base64"),
        ):
            (_, _, findings, ok), records = run_scan_blocks(
                self._empty_page(), {"decompressedTssECDSAPubKey": val}
            )
            self.assertTrue(ok, expected_check)
            self.assertEqual(findings, 1, expected_check)
            live = [r for r in records if r["case"] == "M-3-liveBridgeKey"][0]
            self.assertEqual(live["keyCheck"], expected_check)
            self.assertTrue(live["finding"])

    def test_malformed_bridge_info_response_fails_scan(self):
        (_, _, findings, ok), records = run_scan_blocks(self._empty_page(), None)
        self.assertFalse(ok)
        self.assertEqual(findings, 0)
        summary = [r for r in records if r["case"] == "M-3-blocks"][-1]
        self.assertEqual(summary["status"], "FAIL")
        self.assertEqual(summary["parseFailures"], 1)


class ScanBlocksDifficultyThresholds(unittest.TestCase):
    def test_difficulty_counters_and_boundaries(self):
        blocks = [
            make_plain_block("d-zero", "0"),
            make_plain_block("d-legacy-eq", str(scan.LEGACY_MAX_DIFFICULTY)),
            make_plain_block("d-legacy-plus1", str(scan.LEGACY_MAX_DIFFICULTY + 1)),
            make_plain_block("d-dp-eq", str(scan.DP_MAX_DIFFICULTY)),
            make_plain_block("d-dp-plus1", str(scan.DP_MAX_DIFFICULTY + 1)),
            make_plain_block("d-high-minus1", str(scan.HIGH_DIFF_THRESHOLD - 1)),
            make_plain_block("d-high-eq", str(scan.HIGH_DIFF_THRESHOLD)),
        ]
        pages = {2: {"momentum": {"height": 2, "hash": "mh2"}, "blocks": blocks}}
        info = {"decompressedTssECDSAPubKey": uncompressed_b64(SECP256K1_GX, SECP256K1_GY)}

        (momentums, blocks_read, findings, ok), records = run_scan_blocks(pages, info)
        self.assertTrue(ok)
        self.assertEqual(momentums, 1)
        self.assertEqual(blocks_read, len(blocks))
        # Only >= 2^63 counts as a #78 finding; DP/legacy cap crossings are visibility-only.
        self.assertEqual(findings, 1)

        high_entries = [r for r in records if r["case"] == "M-3-highDifficulty"]
        self.assertEqual([r["hash"] for r in high_entries], ["d-high-eq"])

        dp_entries = [r for r in records if r["case"] == "M-3-aboveDpMaxDifficulty"]
        self.assertEqual(
            [r["hash"] for r in dp_entries],
            ["d-dp-plus1", "d-high-minus1", "d-high-eq"],
        )

        summary = [r for r in records if r["case"] == "M-3-blocks"][-1]
        self.assertEqual(summary["status"], "PASS")
        self.assertEqual(summary["highDifficultyItems"], 1)
        self.assertEqual(summary["aboveDpMaxDifficulty"], 3)
        self.assertEqual(summary["aboveLegacyMaxDifficulty"], 5)
        self.assertEqual(len(summary["aboveLegacyMaxDifficultySample"]), 5)
        self.assertEqual(summary["maxDifficulty"], scan.HIGH_DIFF_THRESHOLD)
        self.assertEqual(summary["findings"], 1)


if __name__ == "__main__":
    unittest.main()
