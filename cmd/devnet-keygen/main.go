package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tyler-smith/go-bip39"

	"github.com/zenon-network/go-zenon/chain/genesis"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/p2p/discover"
	"github.com/zenon-network/go-zenon/wallet"

	"github.com/ethereum/go-ethereum/crypto"
)

const (
	devMnemonic = "abstract affair idle position alien fluid board ordinary exist afraid chapter wood wood guide sun walnut crew perfect place firm poverty model side million"
	devPassword = "devnet"

	devnetDir   = "docker/devnet"
	genesisFile = "docker/devnet/genesis.json"

	// Total derivations exposed in the dev table. 0/4/6 are producer keys
	// (live in pillar containers); 1/5/7 are pillar owners (held by the
	// user); 2 is spork; 3 is a general dev account.
	derivationCount uint32 = 8
)

// pillarSpec describes one of the three devnet pillars. The producer key for
// each pillar is committed under <Dir>/wallet/<producer-address>; the owner
// is referenced from genesis.json only and is never put in a container.
type pillarSpec struct {
	Role          string // also the docker-compose service name and ZNND_ROLE env var
	Dir           string // <repo>/docker/devnet/<Role>
	IP            string // static IPv4 on the znnd-devnet bridge
	ProducerIndex uint32
	OwnerIndex    uint32
	IsBootstrap   bool // bootstrap pillars start with no seeders + MinPeers=0
}

var pillars = []pillarSpec{
	{Role: "pillar", Dir: filepath.Join(devnetDir, "pillar"), IP: "172.30.0.10", ProducerIndex: 0, OwnerIndex: 1, IsBootstrap: true},
	{Role: "pillar2", Dir: filepath.Join(devnetDir, "pillar2"), IP: "172.30.0.12", ProducerIndex: 4, OwnerIndex: 5},
	{Role: "pillar3", Dir: filepath.Join(devnetDir, "pillar3"), IP: "172.30.0.13", ProducerIndex: 6, OwnerIndex: 7},
}

type relaySpec struct {
	Role     string
	Dir      string
	IP       string
	MinPeers int
}

var relays = []relaySpec{
	{Role: "rpc", Dir: filepath.Join(devnetDir, "rpc"), IP: "172.30.0.11", MinPeers: 2},
	{Role: "observer", Dir: filepath.Join(devnetDir, "observer"), IP: "172.30.0.14", MinPeers: 2},
}

func main() {
	force := flag.Bool("force", false, "overwrite existing keystores and network-private-keys")
	verifyGenesis := flag.String("verify-genesis", "", "path to genesis.json to validate (skips key generation)")
	flag.Parse()

	if *verifyGenesis != "" {
		if err := runVerifyGenesis(*verifyGenesis); err != nil {
			fmt.Fprintf(os.Stderr, "verify-genesis failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := run(*force); err != nil {
		fmt.Fprintf(os.Stderr, "devnet-keygen failed: %v\n", err)
		os.Exit(1)
	}
}

func loadChainIdentifier(path string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	cfg := new(genesis.GenesisConfig)
	if err := json.NewDecoder(f).Decode(cfg); err != nil {
		return 0, fmt.Errorf("decode genesis: %w", err)
	}
	return cfg.ChainIdentifier, nil
}

func run(force bool) error {
	chainId, err := loadChainIdentifier(genesisFile)
	if err != nil {
		return fmt.Errorf("load chain identifier: %w", err)
	}
	fmt.Printf("ChainIdentifier: %d (from %s)\n", chainId, genesisFile)

	entropy, err := bip39.EntropyFromMnemonic(devMnemonic)
	if err != nil {
		return fmt.Errorf("decode mnemonic: %w", err)
	}

	ks, err := keystoreFromEntropy(entropy)
	if err != nil {
		return fmt.Errorf("derive keystore: %w", err)
	}

	addrs := make([]types.Address, derivationCount)
	for i := uint32(0); i < derivationCount; i++ {
		_, kp, err := ks.DeriveForIndexPath(i)
		if err != nil {
			return fmt.Errorf("derive index %d: %w", i, err)
		}
		addrs[i] = kp.Address
	}

	// Pass 1: write keystores + network-private-keys per pillar.
	for _, p := range pillars {
		producer := addrs[p.ProducerIndex]
		walletDir := filepath.Join(p.Dir, "wallet")
		if err := os.MkdirAll(walletDir, 0o755); err != nil {
			return err
		}
		keyfilePath := filepath.Join(walletDir, producer.String())
		if force || !fileExists(keyfilePath) {
			if err := writeKeystore(ks, producer, keyfilePath); err != nil {
				return err
			}
			fmt.Printf("wrote keystore: %s\n", keyfilePath)
		} else {
			fmt.Printf("keystore already exists, leaving in place: %s (use --force to overwrite)\n", keyfilePath)
		}

		netKeyPath := filepath.Join(p.Dir, "network-private-key")
		if force || !fileExists(netKeyPath) {
			k, err := crypto.GenerateKey()
			if err != nil {
				return fmt.Errorf("generate p2p key for %s: %w", p.Role, err)
			}
			if err := crypto.SaveECDSA(netKeyPath, k); err != nil {
				return fmt.Errorf("save p2p key for %s: %w", p.Role, err)
			}
			fmt.Printf("wrote network-private-key: %s\n", netKeyPath)
		} else {
			fmt.Printf("network-private-key already exists: %s (use --force to regenerate)\n", netKeyPath)
		}
	}
	for _, r := range relays {
		netKeyPath := filepath.Join(r.Dir, "network-private-key")
		if err := os.MkdirAll(filepath.Dir(netKeyPath), 0o755); err != nil {
			return err
		}
		if force || !fileExists(netKeyPath) {
			k, err := crypto.GenerateKey()
			if err != nil {
				return fmt.Errorf("generate p2p key for %s: %w", r.Role, err)
			}
			if err := crypto.SaveECDSA(netKeyPath, k); err != nil {
				return fmt.Errorf("save p2p key for %s: %w", r.Role, err)
			}
			fmt.Printf("wrote network-private-key: %s\n", netKeyPath)
		} else {
			fmt.Printf("network-private-key already exists: %s (use --force to regenerate)\n", netKeyPath)
		}
	}

	// Pass 2: load each p2p key, build static enodes for every devnet node.
	enodes := make(map[string]string, len(pillars)+len(relays))
	var bootstrapEnode string
	for _, p := range pillars {
		k, err := crypto.LoadECDSA(filepath.Join(p.Dir, "network-private-key"))
		if err != nil {
			return fmt.Errorf("load p2p key for %s: %w", p.Role, err)
		}
		// Seeder parsing in p2p/discover requires a numeric IP, not a hostname,
		// so we point at the static IPs reserved in docker-compose.yml.
		enode := fmt.Sprintf("enode://%s@%s:35995", discover.PubkeyID(&k.PublicKey).String(), p.IP)
		enodes[p.Role] = enode
		if p.IsBootstrap {
			bootstrapEnode = enode
		}
	}
	for _, r := range relays {
		k, err := crypto.LoadECDSA(filepath.Join(r.Dir, "network-private-key"))
		if err != nil {
			return fmt.Errorf("load p2p key for %s: %w", r.Role, err)
		}
		enodes[r.Role] = fmt.Sprintf("enode://%s@%s:35995", discover.PubkeyID(&k.PublicKey).String(), r.IP)
	}
	if bootstrapEnode == "" {
		return fmt.Errorf("no bootstrap pillar configured")
	}

	// Pass 3: write per-node configs. Relays seed from all pillars plus each other
	// so transaction ingress is not dependent on a single bootstrap connection.
	for _, p := range pillars {
		producer := addrs[p.ProducerIndex]
		seeders := []string{}
		minPeers := 0
		if !p.IsBootstrap {
			seeders = pillarSeedersExcept(enodes, p.Role)
			minPeers = 1
		}
		cfgPath := filepath.Join(p.Dir, "config.json")
		if err := writePillarConfig(cfgPath, p.Role, producer, p.ProducerIndex, seeders, minPeers); err != nil {
			return err
		}
	}
	for _, r := range relays {
		if err := writeRelayConfig(filepath.Join(r.Dir, "config.json"), r.Role, relaySeedersExcept(enodes, r.Role), r.MinPeers); err != nil {
			return err
		}
	}

	fmt.Println()
	fmt.Println("Dev addresses (BIP-44 m/44'/73404'/i'):")
	roleByIdx := map[uint32]string{
		0: "pillar 1 producer",
		1: "pillar 1 owner / dev wallet",
		2: "spork address",
		3: "general dev account",
		4: "pillar 2 producer",
		5: "pillar 2 owner",
		6: "pillar 3 producer",
		7: "pillar 3 owner",
	}
	for i := uint32(0); i < derivationCount; i++ {
		fmt.Printf("  index %d (%-28s): %s\n", i, roleByIdx[i], addrs[i])
	}
	fmt.Println()
	for _, p := range pillars {
		fmt.Printf("%s enode: %s\n", p.Role, enodes[p.Role])
	}
	for _, r := range relays {
		fmt.Printf("%s enode: %s\n", r.Role, enodes[r.Role])
	}
	return nil
}

func writeKeystore(ks *wallet.KeyStore, producer types.Address, dst string) error {
	kf, err := ks.Encrypt(devPassword)
	if err != nil {
		return fmt.Errorf("encrypt keystore: %w", err)
	}
	// KeyFile.Path has no JSON tag, so it ends up in the serialised JSON.
	// wallet.Manager keys its in-memory map on the value read back from disk,
	// so we have to write the *container* path here (where znnd will read it
	// from) rather than the host path.
	kf.Path = "/root/.znn/wallet/" + producer.String()
	b, err := json.MarshalIndent(kf, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal keystore: %w", err)
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		return fmt.Errorf("write keystore: %w", err)
	}
	return nil
}

// keystoreFromEntropy mirrors wallet.keyStoreFromEntropy (which is unexported).
// It builds a KeyStore directly from raw entropy and seeds the BaseAddress
// from the index-0 derivation, matching wallet/keystore.go:24-44.
func keystoreFromEntropy(entropy []byte) (*wallet.KeyStore, error) {
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return nil, err
	}
	ks := &wallet.KeyStore{
		Entropy:  entropy,
		Seed:     bip39.NewSeed(mnemonic, ""),
		Mnemonic: mnemonic,
	}
	_, kp, err := ks.DeriveForIndexPath(0)
	if err != nil {
		return nil, err
	}
	ks.BaseAddress = kp.Address
	return ks, nil
}

func writePillarConfig(path, name string, producer types.Address, producerIdx uint32, seeders []string, minPeers int) error {
	cfg := map[string]any{
		"DataPath":    "/root/.znn",
		"WalletPath":  "/root/.znn/wallet",
		"GenesisFile": "/root/.znn/genesis.json",
		"Name":        "devnet-" + name,
		"LogLevel":    "info",
		"Producer": map[string]any{
			"Address":     producer.String(),
			"Index":       producerIdx,
			"KeyFilePath": producer.String(),
			"Password":    devPassword,
		},
		"RPC": map[string]any{
			"EnableHTTP":       true,
			"EnableWS":         true,
			"HTTPHost":         "0.0.0.0",
			"HTTPPort":         35997,
			"WSHost":           "0.0.0.0",
			"WSPort":           35998,
			"Endpoints":        []string{},
			"HTTPVirtualHosts": []string{"*"},
			"HTTPCors":         []string{"*"},
			"WSOrigins":        []string{"*"},
		},
		"Net": map[string]any{
			"ListenHost":        "0.0.0.0",
			"ListenPort":        35995,
			"MinPeers":          minPeers,
			"MinConnectedPeers": minPeers,
			"Seeders":           seeders,
		},
	}
	return writeJSON(path, cfg)
}

func writeRelayConfig(path, name string, seeders []string, minPeers int) error {
	cfg := map[string]any{
		"DataPath":    "/root/.znn",
		"WalletPath":  "/root/.znn/wallet",
		"GenesisFile": "/root/.znn/genesis.json",
		"Name":        "devnet-" + name,
		"LogLevel":    "info",
		"RPC": map[string]any{
			"EnableHTTP":       true,
			"EnableWS":         true,
			"HTTPHost":         "0.0.0.0",
			"HTTPPort":         35997,
			"WSHost":           "0.0.0.0",
			"WSPort":           35998,
			"Endpoints":        []string{},
			"HTTPVirtualHosts": []string{"*"},
			"HTTPCors":         []string{"*"},
			"WSOrigins":        []string{"*"},
		},
		"Net": map[string]any{
			"ListenHost":        "0.0.0.0",
			"ListenPort":        35995,
			"MinPeers":          minPeers,
			"MinConnectedPeers": minPeers,
			"Seeders":           seeders,
		},
	}
	return writeJSON(path, cfg)
}

func pillarSeedersExcept(enodes map[string]string, self string) []string {
	seeders := make([]string, 0, len(pillars)-1)
	for _, p := range pillars {
		if p.Role != self {
			seeders = append(seeders, enodes[p.Role])
		}
	}
	return seeders
}

func relaySeedersExcept(enodes map[string]string, self string) []string {
	seeders := make([]string, 0, len(pillars)+len(relays)-1)
	for _, p := range pillars {
		seeders = append(seeders, enodes[p.Role])
	}
	for _, r := range relays {
		if r.Role != self {
			seeders = append(seeders, enodes[r.Role])
		}
	}
	return seeders
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "    ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote config: %s\n", path)
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func runVerifyGenesis(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	cfg := new(genesis.GenesisConfig)
	if err := json.NewDecoder(f).Decode(cfg); err != nil {
		return fmt.Errorf("decode genesis: %w", err)
	}
	if err := genesis.CheckGenesis(cfg); err != nil {
		return fmt.Errorf("CheckGenesis: %w", err)
	}

	if cfg.PillarConfig == nil || len(cfg.PillarConfig.Pillars) != len(pillars) {
		return fmt.Errorf("expected exactly %d pillars, got %d", len(pillars), len(cfg.PillarConfig.Pillars))
	}
	if cfg.TokenConfig == nil || len(cfg.TokenConfig.Tokens) == 0 {
		return fmt.Errorf("no tokens defined")
	}
	for _, t := range cfg.TokenConfig.Tokens {
		if t.TotalSupply == nil || t.TotalSupply.Sign() == 0 {
			return fmt.Errorf("token %s has zero TotalSupply (likely JSON tag mismatch)", t.TokenName)
		}
	}
	fmt.Printf("genesis OK: ChainIdentifier=%d, pillars=%d, tokens=%d\n",
		cfg.ChainIdentifier, len(cfg.PillarConfig.Pillars), len(cfg.TokenConfig.Tokens))
	return nil
}
