#!/usr/bin/env bash
set -euo pipefail

# Computes the znnd/libznn release version from the current git checkout.
#
# Inputs (env):
#   REF_TYPE - "tag" or "branch" (mirrors github.ref_type)
#   REF_NAME - tag name, or branch name (mirrors github.ref_name)
#
# Outputs: printed to stdout as key=value lines, and additionally appended
# to $GITHUB_OUTPUT when that variable is set.

if [ "${REF_TYPE}" = "tag" ]; then
  VERSION="${REF_NAME}"
  PRERELEASE="false"
else
  BASE_TAG="$(git describe --tags --abbrev=0 --match 'v[0-9]*' --exclude '*-dev' --exclude '*-dev-*' --exclude '*-testnet' --exclude '*-testnet-*')"
  BASE_VERSION="${BASE_TAG%-alphanet}"
  COUNT="$(git rev-list "${BASE_TAG}..HEAD" --count)"
  HASH="$(git rev-parse --short HEAD)"
  case "${REF_NAME}" in
    dev)
      VERSION="${BASE_VERSION}-dev-${COUNT}-g${HASH}"
      PRERELEASE="true"
      ;;
    testnet)
      VERSION="${BASE_VERSION}-testnet-${COUNT}-g${HASH}"
      PRERELEASE="true"
      ;;
    *)
      VERSION="${BASE_VERSION}-${COUNT}-g${HASH}"
      PRERELEASE="false"
      ;;
  esac
fi

OUTPUT="version=$VERSION
release_name=$VERSION
release_tag=$VERSION
prerelease=$PRERELEASE"

echo "$OUTPUT"
if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "$OUTPUT" >> "$GITHUB_OUTPUT"
fi
