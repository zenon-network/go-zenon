#!/bin/sh
set -e

ROLE="${ZNND_ROLE:-rpc}"
SRC="/devnet/${ROLE}"
DATA_DIR="/root/.znn"

if [ ! -d "$SRC" ]; then
    echo "unknown ZNND_ROLE='$ROLE' (expected a directory under /devnet)" >&2
    exit 1
fi

mkdir -p "$DATA_DIR"

if [ ! -f "$DATA_DIR/config.json" ]; then
    echo "seeding $DATA_DIR for role=$ROLE"
    cp "$SRC/config.json" "$DATA_DIR/config.json"
    cp /devnet/genesis.json "$DATA_DIR/genesis.json"

    # The committed configs name their seeders in 172.30.0.0/24. A devnet on
    # another subnet (ZNND_DEVNET_SUBNET, see docker-compose.yml) has the same
    # nodes at the same last octet.
    SUBNET="${ZNND_DEVNET_SUBNET:-172.30.0}"
    if [ "$SUBNET" != "172.30.0" ]; then
        sed -i "s/@172\.30\.0\./@${SUBNET}./g" "$DATA_DIR/config.json"
    fi

    if [ -d "$SRC/wallet" ]; then
        mkdir -p "$DATA_DIR/wallet"
        cp -r "$SRC/wallet/." "$DATA_DIR/wallet/"
        chmod 700 "$DATA_DIR/wallet"
        chmod 600 "$DATA_DIR/wallet/"*
    fi

    if [ -f "$SRC/network-private-key" ]; then
        cp "$SRC/network-private-key" "$DATA_DIR/network-private-key"
        chmod 600 "$DATA_DIR/network-private-key"
    fi
fi

exec znnd "$@"
