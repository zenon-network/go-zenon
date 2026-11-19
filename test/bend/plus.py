#!/usr/bin/env python3
"""Mark every parameter of every def in a Bend file as reusable (+).

Bend values are affine by default: a parameter may be used once. The models
here are small and copy freely, so every parameter is written with +. Run
this on a model after adding a def, instead of adding the marks by hand.

    plus.py <file.bend>...
"""
import re
import sys

for path in sys.argv[1:]:
    out = []
    for line in open(path):
        if line.startswith("def ") and "(" in line and ") ->" in line:
            pre, rest = line.split("(", 1)
            params, post = rest.rsplit(") ->", 1)
            params = re.sub(r"(^|, )(?![+\-~])(\w+): ", r"\1+\2: ", params)
            line = pre + "(" + params + ") ->" + post
        out.append(line)
    open(path, "w").write("".join(out))
