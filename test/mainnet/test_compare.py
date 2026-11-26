#!/usr/bin/env python3
"""Unit tests for the classification logic of test/mainnet/compare.py.

Canned observations only — no node, no network.  Run from the repository
root either directly:

    python3 test/mainnet/test_compare.py -v

or through unittest discovery (`test` is also a stdlib package name, so
`python3 -m unittest test.mainnet.test_compare` does not resolve here):

    python3 -m unittest discover -s test/mainnet -t test/mainnet -v
"""
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import compare  # noqa: E402

METHOD_MISSING = ("embedded.plasma.getVariables: {'code': -32601, 'message': "
                  "'the method embedded.plasma.getVariables does not "
                  "exist/is not available'}")


def pillar(name, weight, rank, produced):
    """One PillarInfo answer (producerAddress present, so normalize treats
    it as pillar info)."""
    return {"name": name, "rank": rank, "type": 2,
            "ownerAddress": "z1q" + name, "producerAddress": "z1p" + name,
            "withdrawAddress": "z1w" + name,
            "isRevocable": False, "revokeCooldown": 0, "revokeTimestamp": 0,
            "giveMomentumRewardPercentage": 0,
            "giveDelegateRewardPercentage": 0,
            "currentStats": {"producedMomentums": produced,
                             "expectedMomentums": 100},
            "weight": weight}


class TransportFailures(unittest.TestCase):
    def test_both_sides_transport_failure_is_error_not_pass(self):
        # Identical transport failures are not agreement: neither getter
        # returned an answer.
        status, detail = compare.classify_getter(
            "embedded.pillar.getAll",
            {"transportError": "timed out"}, {"transportError": "timed out"})
        self.assertEqual(status, "ERROR")
        self.assertEqual(detail["detail"], "transport-failure-both-sides")

    def test_one_sided_transport_failure_is_error_not_fail(self):
        status, detail = compare.classify_getter(
            "embedded.pillar.getAll",
            {"transportError": "timed out"}, {"result": {"list": []}})
        self.assertEqual(status, "ERROR")
        self.assertEqual(detail["detail"], "transport-failure-on-one-side")


class MethodMissing(unittest.TestCase):
    def test_missing_on_ref_success_on_test_is_info(self):
        # A getter added on dev (absent in v0.0.10): INFO, not FAIL.
        status, detail = compare.classify_getter(
            "embedded.plasma.getVariables",
            {"rpcError": METHOD_MISSING},
            {"result": {"basePlasma": 21000}})
        self.assertEqual(status, "INFO")
        self.assertEqual(detail["detail"], "method-missing-on-ref")

    def test_missing_on_test_success_on_ref_is_fail(self):
        # A getter the reference serves but the binary under test dropped
        # is a removed RPC surface: FAIL (regression), not the
        # one-sided-error branch and not a note.
        status, detail = compare.classify_getter(
            "embedded.token.getAll",
            {"result": {"count": 0, "list": []}},
            {"rpcError": METHOD_MISSING})
        self.assertEqual(status, "FAIL")
        self.assertEqual(detail["detail"], "method-missing-on-test")

    def test_missing_on_both_is_info_consistent(self):
        status, detail = compare.classify_getter(
            "embedded.future.getX",
            {"rpcError": METHOD_MISSING}, {"rpcError": METHOD_MISSING})
        self.assertEqual(status, "INFO")
        self.assertEqual(detail["detail"], "method-missing-on-both")


class IsMethodMissing(unittest.TestCase):
    def test_code_minus_32601_is_method_missing(self):
        self.assertTrue(compare.is_method_missing(METHOD_MISSING))

    def test_message_substring_without_code_is_not_method_missing(self):
        # "not available" / "does not exist" in an unrelated message must
        # not misclassify a real error as an absent method.
        self.assertFalse(compare.is_method_missing(
            "ledger.getAccountInfoByAddress: {'code': -32000, 'message': "
            "'account not available'}"))
        self.assertFalse(compare.is_method_missing(
            "embedded.htlc.getById: {'code': -32000, 'message': "
            "'the requested resource does not exist'}"))
        self.assertFalse(compare.is_method_missing(None))


class RpcErrors(unittest.TestCase):
    def test_identical_rpc_errors_are_pass(self):
        err = "embedded.htlc.getById: {'code': -32000, 'message': 'not found'}"
        status, _ = compare.classify_getter("embedded.htlc.getById",
                                            {"rpcError": err}, {"rpcError": err})
        self.assertEqual(status, "PASS")

    def test_different_rpc_errors_are_fail(self):
        status, detail = compare.classify_getter(
            "embedded.htlc.getById",
            {"rpcError": "a: {'message': 'not found'}"},
            {"rpcError": "a: {'message': 'internal error'}"})
        self.assertEqual(status, "FAIL")
        self.assertEqual(detail["detail"], "error-surface-differs")

    def test_error_on_one_side_is_fail(self):
        status, detail = compare.classify_getter(
            "embedded.bridge.getBridgeInfo",
            {"rpcError": "boom"}, {"result": {}})
        self.assertEqual(status, "FAIL")
        self.assertEqual(detail["detail"], "error-on-one-side")


class ExpectedApiAdditions(unittest.TestCase):
    def test_allowlisted_extra_key_is_info(self):
        # requiredFusedPlasma on dev (Dynamic Plasma) must not fail a
        # dev-vs-v0.0.10 run.
        ref = {"result": {"availablePlasma": 100, "basePlasma": 21000,
                          "requiredDifficulty": 0}}
        test = {"result": {"availablePlasma": 100, "basePlasma": 21000,
                           "requiredDifficulty": 0,
                           "requiredFusedPlasma": 21000}}
        status, detail = compare.classify_getter(
            "embedded.plasma.getRequiredPoWForAccountBlock", ref, test)
        self.assertEqual(status, "INFO")
        self.assertEqual(detail["detail"], "expected-api-addition")
        self.assertEqual(detail["addedKeys"], ["requiredFusedPlasma"])
        self.assertIn("requiredFusedPlasma", detail["reasons"])

    def test_unlisted_extra_key_still_fails(self):
        status, detail = compare.classify_getter(
            "embedded.bridge.getBridgeInfo",
            {"result": {"unwrapCooldown": 10}},
            {"result": {"unwrapCooldown": 10, "surprise": 1}})
        self.assertEqual(status, "FAIL")
        self.assertEqual(detail["firstDiff"]["path"], "surprise")
        self.assertTrue(detail["firstDiff"]["refMissing"])

    def test_allowlisted_key_with_other_diff_still_fails(self):
        status, _ = compare.classify_getter(
            "embedded.plasma.getRequiredPoWForAccountBlock",
            {"result": {"availablePlasma": 100, "requiredDifficulty": 0}},
            {"result": {"availablePlasma": 200, "requiredDifficulty": 0,
                        "requiredFusedPlasma": 21000}})
        self.assertEqual(status, "FAIL")


class PillarGetAllNormalization(unittest.TestCase):
    def test_order_and_cache_fields_normalize_to_pass(self):
        # embedded.pillar.getAll: warm node sorts by descending weight,
        # cold node (weight 0) by ascending name; weight/rank/currentStats
        # come from the node-local consensus cache.  Same pillars, both
        # orderings, different cache values: PASS.
        warm = {"count": 2, "list": [pillar("bbb", "999999", 0, 55),
                                     pillar("aaa", "111111", 1, 10)]}
        cold = {"count": 2, "list": [pillar("aaa", "0", 0, 0),
                                     pillar("bbb", "0", 1, 0)]}
        status, detail = compare.classify_getter("embedded.pillar.getAll",
                                                 {"result": warm},
                                                 {"result": cold})
        self.assertEqual(status, "PASS", detail)

    def test_real_pillar_diff_still_fails(self):
        # Same normalization, but a non-cache field differs: FAIL.
        a = {"count": 1, "list": [pillar("aaa", "0", 0, 0)]}
        b = {"count": 1, "list": [dict(pillar("aaa", "0", 0, 0),
                                       giveMomentumRewardPercentage=10)]}
        status, detail = compare.classify_getter("embedded.pillar.getAll",
                                                 {"result": a}, {"result": b})
        self.assertEqual(status, "FAIL")
        self.assertEqual(detail["firstDiff"]["path"],
                         "list[0].giveMomentumRewardPercentage")

    def test_get_by_owner_order_normalizes_to_pass(self):
        # embedded.pillar.getByOwner answers with the same PillarInfo list
        # (a plain list, not a {"list": ...} page) in the same node-local
        # weight order as getAll; the same sort key applies.
        warm = [pillar("bbb", "999999", 0, 55), pillar("aaa", "111111", 1, 10)]
        cold = [pillar("aaa", "0", 0, 0), pillar("bbb", "0", 1, 0)]
        status, detail = compare.classify_getter("embedded.pillar.getByOwner",
                                                 {"result": warm},
                                                 {"result": cold})
        self.assertEqual(status, "PASS", detail)


class ResultComparison(unittest.TestCase):
    def test_equal_results_pass(self):
        status, _ = compare.classify_getter(
            "embedded.bridge.getBridgeInfo",
            {"result": {"unwrapCooldown": 10}},
            {"result": {"unwrapCooldown": 10}})
        self.assertEqual(status, "PASS")

    def test_fail_carries_first_diff(self):
        status, detail = compare.classify_getter(
            "embedded.bridge.getBridgeInfo",
            {"result": {"unwrapCooldown": 10}},
            {"result": {"unwrapCooldown": 11}})
        self.assertEqual(status, "FAIL")
        self.assertEqual(detail["detail"], "result-differs")
        self.assertEqual(detail["firstDiff"],
                         {"path": "unwrapCooldown", "ref": "10", "test": "11"})

    def test_paged_top_level_fields_are_compared(self):
        # page_all preserves fields next to "list" (count, qsrAmount, ...);
        # a disagreement on them is a FAIL even when the lists match.
        status, detail = compare.classify_getter(
            "embedded.stake.getEntriesByAddress",
            {"result": {"count": 1, "totalAmount": "100", "list": [{"x": 1}]}},
            {"result": {"count": 1, "totalAmount": "200", "list": [{"x": 1}]}})
        self.assertEqual(status, "FAIL")
        self.assertEqual(detail["firstDiff"]["path"], "totalAmount")


if __name__ == "__main__":
    unittest.main()
