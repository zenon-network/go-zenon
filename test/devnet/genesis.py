#!/usr/bin/env python3
"""Write the genesis variants used by the test procedures.

    test/devnet/genesis.py            # writes every variant except fast to test/devnet/genesis/
    test/devnet/genesis.py fast       # writes the named variant(s) only
    test/devnet/genesis.py --list

Each variant is docker/devnet/genesis.json with a different SporkConfig and
nothing else changed, so accounts, pillars, balances and keys are the same on
all of them. The spork ids are the ones compiled into znnd
(common/types/spork.go). A height of None leaves the spork out of the genesis.
"""
import json
import os
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
STOCK = os.path.join(HERE, "..", "..", "docker", "devnet", "genesis.json")
OUT = os.path.join(HERE, "genesis")

SPORKS = [  # name, id, description
    ("az", "6d2b1e6cb4025f2f45533f0fe22e9b7ce2014d91cc960471045fa64eee5a6ba3", "az"),
    ("htlc", "ceb7e3808ef17ea910adda2f3ab547be4cdfb54de8400ce3683258d06be1354b", "htlc"),
    ("bridge-liq", "ddd43466769461c5b5d109c639da0f50a7eeb96ad6e7274b1928a35c431d7b1b", "bridge-liq"),
    ("ptlc", "e59f1e97f1096a18c2bc8421345b2633b11b839adb153e96dc65034c95f83f8d", "ptlc"),
    ("libp2p", "0" * 63 + "1", "Activates the libp2p networking stack"),
    ("dynamic-plasma", "0" * 63 + "2", "Activates Dynamic Plasma"),
    ("embedded-refund", "0" * 63 + "4", "Refund of a failed contract-to-contract send"),
    ("epoch-reward-terms", "0" * 63 + "7", "Per-epoch pillar reward terms"),
]

# Enforcement height per spork; a missing name means the spork is not in the genesis.
VARIANTS = {
    # No spork at all: the chain as it was before the first spork.
    "all-off": {},
    # What mainnet enforces today: Accelerator, HTLC, Bridge and Liquidity. Legacy transport, no dynamic plasma.
    "mainnet-like": {"az": 0, "htlc": 0, "bridge-liq": 0},
    # Every spork of this binary. libp2p and dynamic plasma keep the stock heights, because their
    # switch-over while the network runs is itself under test (smoke test, procedure P4).
    "all-on": {"az": 0, "htlc": 0, "bridge-liq": 0, "ptlc": 0, "embedded-refund": 0, "epoch-reward-terms": 0,
               "libp2p": 20, "dynamic-plasma": 30},
    # The new sporks arrive one after another on a running network, for before/at/after cases.
    "staged": {"az": 0, "htlc": 0, "bridge-liq": 0, "libp2p": 20, "dynamic-plasma": 30,
               "ptlc": 60, "embedded-refund": 70, "epoch-reward-terms": 80},
    # For the test build with fast constants (FAST=1 build-image.sh, procedure P11): every spork
    # from the start, and a genesis time of now. Epochs are counted from the genesis time; with
    # the committed time, which is long past, the reward contracts would first have to work
    # through every epoch since then.
    "fast": {"az": 0, "htlc": 0, "bridge-liq": 0, "ptlc": 0, "embedded-refund": 0, "epoch-reward-terms": 0,
             "libp2p": 0, "dynamic-plasma": 0},
}
FRESH_TIME = {"fast"}


def build(heights, fresh_time=False):
    with open(STOCK) as f:
        genesis = json.load(f)
    if fresh_time:
        genesis["GenesisTimestampSec"] = int(time.time()) // 10 * 10 - 30
    genesis["SporkConfig"] = {"Sporks": [
        {"id": sid, "name": name, "description": desc, "activated": True, "enforcementHeight": heights[name]}
        for name, sid, desc in SPORKS if name in heights
    ]}
    return genesis


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    if "--list" in sys.argv:
        for name, heights in VARIANTS.items():
            print("%-13s %s" % (name, ", ".join("%s@%d" % kv for kv in heights.items()) or "no sporks"))
        print("%-13s the committed docker/devnet/genesis.json" % "stock")
        return
    unknown = [a for a in args if a not in VARIANTS]
    if unknown:
        sys.exit("unknown variant(s): %s; see --list" % ", ".join(unknown))
    os.makedirs(OUT, exist_ok=True)
    # With no name, every variant whose content does not depend on the clock.
    for name in args or [n for n in VARIANTS if n not in FRESH_TIME]:
        path = os.path.join(OUT, name + ".json")
        with open(path, "w") as f:
            json.dump(build(VARIANTS[name], name in FRESH_TIME), f, indent=2)
            f.write("\n")
        print("wrote", os.path.relpath(path))


if __name__ == "__main__":
    main()
