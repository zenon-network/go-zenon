// Command genesishash prints the hash of the genesis momentum that a genesis
// file produces, computed here from the file and not asked of a running node.
// up.sh compares it with what every node reports at height 1.
//
//	go run ./test/devnet/genesishash <genesis.json>
package main

import (
	"fmt"
	"os"

	"github.com/zenon-network/go-zenon/chain/genesis"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: genesishash <genesis.json>")
		os.Exit(2)
	}
	g, err := genesis.ReadGenesisConfigFromFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "genesishash:", err)
		os.Exit(1)
	}
	m := g.GetGenesisMomentum()
	if m == nil || m.Height != 1 {
		fmt.Fprintln(os.Stderr, "genesishash: no genesis momentum at height 1")
		os.Exit(1)
	}
	fmt.Println(m.Hash.String())
}
