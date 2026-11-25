#!/bin/sh
# Compare the source-derived inventory with coverage.tsv. See check.py.
exec python3 "$(dirname "$0")/check.py" "$@"
