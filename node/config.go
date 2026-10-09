package node

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain/genesis"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/metadata"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon"
)

// Secret is a string that does not reveal itself: the fmt verbs (with the
// two exceptions noted on Format) and JSON encoding of a value or field
// render it as a fixed marker, so a configuration carrying one can be
// printed or logged whole. encoding/json writes a Secret used as a map key
// as is, since it uses keys of string kind directly.
//
// Decoding is encoding/json's own for a string kind, so a config.json
// reads exactly as it did for a plain string field: a JSON null leaves the
// value unchanged and a non-string value is a type error recorded against
// the field while the enclosing object keeps decoding. The value is
// obtained with a string conversion where it is used; assigning from a
// string variable takes a conversion as well, Secret(v), while untyped
// constants assign directly.
//
// The JSON encoding is diagnostic output, not a persistence format:
// decoding it yields the marker, not the value.
type Secret string

const redactedMarker = "[redacted]"

// String implements fmt.Stringer.
func (s Secret) String() string {
	if s == "" {
		return ""
	}
	return redactedMarker
}

// GoString implements fmt.GoStringer.
func (s Secret) GoString() string {
	return fmt.Sprintf("node.Secret(%q)", s.String())
}

// writeState writes s to a fmt.State. The state writes into fmt's own
// buffer and never reports an error.
func writeState(f fmt.State, s string) {
	_, _ = io.WriteString(f, s)
}

// Format implements fmt.Formatter so that every verb, including the
// numeric, float and rune verbs for which fmt would otherwise print a
// diagnostic containing the raw value, renders the marker. Two verbs
// cannot be covered because fmt rejects them before consulting the
// operand's methods and prints its diagnostic with the raw value: %p
// applied to a non-pointer value and %w applied to a value that is not an
// error, when the operand is a Secret or a ProducerConfig holding one (a
// Config holds the section behind a pointer, which that path prints as an
// address). go vet's printf check reports both misuses.
func (s Secret) Format(f fmt.State, verb rune) {
	switch {
	case verb == 'q':
		writeState(f, fmt.Sprintf("%q", s.String()))
	case verb == 'v' && f.Flag('#'):
		writeState(f, s.GoString())
	default:
		writeState(f, s.String())
	}
}

// MarshalJSON writes the marker in place of the value.
func (s Secret) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

type ProducerConfig struct {
	Address     string
	Index       uint32
	KeyFilePath string
	Password    Secret
}

// Format implements fmt.Formatter. A pointer to this struct nested in
// another value and printed with a verb that is invalid for pointers would
// otherwise be re-printed by fmt's diagnostic path, which does not consult
// the fields' own formatting; rendering the struct here keeps Password
// redacted under every verb except the two noted on Secret.Format.
func (c ProducerConfig) Format(f fmt.State, verb rune) {
	type view ProducerConfig // same fields, no methods: default struct rendering
	switch {
	case verb == 'v' && f.Flag('#'):
		writeState(f, "node.ProducerConfig"+strings.TrimPrefix(fmt.Sprintf("%#v", view(c)), "node.view"))
	case verb == 'v' && f.Flag('+'):
		writeState(f, fmt.Sprintf("%+v", view(c)))
	default:
		writeState(f, fmt.Sprintf("%v", view(c)))
	}
}

type RPCConfig struct {
	EnableHTTP bool
	EnableWS   bool

	HTTPHost string
	HTTPPort int
	WSHost   string
	WSPort   int

	Endpoints []string

	HTTPVirtualHosts []string
	HTTPCors         []string
	WSOrigins        []string

	// MaxSubscriptionsPerConn bounds the subscriptions one RPC connection may
	// hold; a further subscribe on that connection fails with "too many
	// subscriptions on this connection". MaxSubscriptions bounds live
	// subscriptions across all connections, IPC and in-process included;
	// once reached, every new subscribe fails with "subscribe server
	// subscription limit reached" until a slot is released. Zero selects
	// the built-in default for either.
	MaxSubscriptionsPerConn int
	MaxSubscriptions        int
	// MaxWSConnectionsPerIP bounds concurrent WebSocket connections from a
	// single remote IP address; connections in excess are refused with HTTP
	// 429. Zero means no per-IP limit. This composes with the per-connection
	// subscription limit: with MaxSubscriptionsPerConn subscriptions per
	// connection and MaxWSConnectionsPerIP connections, one address can hold
	// at most their product in subscriptions.
	MaxWSConnectionsPerIP int
}
type NetConfig struct {
	ListenHost string
	ListenPort int

	// P2PBackend selects which transport backend to start: "auto"
	// (default, spork-gated), "libp2p" (skip oracle, start libp2p
	// directly), or "legacy" (start legacy, never swap). The
	// "libp2p" value lets a fresh node bootstrap from a post-activation
	// network where no legacy peers remain (issue #105).
	P2PBackend string

	MinPeers          int
	MinConnectedPeers int
	MaxPeers          int
	MaxPendingPeers   int

	// Seeders is the legacy bootstrap list (enode:// format), used by
	// the devp2p/RLPX backend pre-activation. Existing operator configs
	// populate this field.
	Seeders []string

	// BootstrapPeers is the libp2p bootstrap list (multiaddr format),
	// used by the libp2p backend post-activation. New field; operators
	// add it to config.json when the spork-gated rollout begins. Omitted
	// configs fall back to p2p.DefaultBootstrapPeers.
	BootstrapPeers []string

	// NATPortMap enables UPnP / NAT-PMP port mapping for the libp2p
	// backend. Default false (matches the pre-libp2p network: legacy
	// had NAT mapping unconfigured in the standard wiring path, so it
	// was effectively off). Home operators behind a NATting router can
	// opt-in by setting this to true in config.json.
	NATPortMap bool

	// PeerstoreDir is an optional override for the libp2p peer-database
	// directory. A pointer so config.json can distinguish "omitted" from
	// "explicitly set to empty": when the field is omitted (nil),
	// node/config.go places it at <DataPath>/network/libp2p-peerstore/;
	// when explicitly set to "", peer persistence is disabled (every
	// restart is a cold start), per p2p.Config.PeerstoreDir. Operators
	// normally don't need to set this at all.
	PeerstoreDir *string
}

type Config struct {
	DataPath    string // default ~/.zenon
	WalletPath  string // default DataPath/wallet
	GenesisFile string // GenesisFile is the absolute path to the genesis file

	Name string

	LogLevel string // "debug", "dbug" | "info" | "warn" | "error", "error" | "crit"

	Producer *ProducerConfig
	RPC      RPCConfig
	Net      NetConfig
}

func (c *Config) MakePathsAbsolute() error {
	if c.DataPath == "" {
		c.DataPath = DefaultDataDir()
	} else {
		absDataDir, err := filepath.Abs(c.DataPath)
		if err != nil {
			return err
		}
		c.DataPath = absDataDir
	}

	if c.WalletPath == "" {
		c.WalletPath = filepath.Join(c.DataPath, DefaultWalletDir)
	} else {
		c.WalletPath = ReplaceHomeVariable(c.WalletPath)
		absWalletDir, err := filepath.Abs(c.WalletPath)
		if err != nil {
			return err
		}
		c.WalletPath = absWalletDir
	}

	if c.GenesisFile != "" {
		c.GenesisFile = ReplaceHomeVariable(c.GenesisFile)
		absGenesisFile, err := filepath.Abs(c.GenesisFile)
		if err != nil {
			return err
		}
		c.GenesisFile = absGenesisFile
	}

	return nil
}

func (c *Config) makeZenonConfig(walletManager *wallet.Manager) (*zenon.Config, error) {
	pillarCoinbase, err := c.parseProducer(walletManager)
	if err != nil {
		return nil, err
	}

	return &zenon.Config{
		MinPeers:          c.Net.MinPeers,
		MinConnectedPeers: c.Net.MinConnectedPeers,
		ProducingKeyPair:  pillarCoinbase,
		GenesisConfig:     c.makeGenesisConfig(),
		DataDir:           c.DataPath,
		MaxSubscriptions:  c.RPC.MaxSubscriptions,
	}, nil
}
func (c *Config) makeGenesisConfig() (genesisConfig store.Genesis) {
	var err error
	var path string

	if c.GenesisFile != "" {
		path = c.GenesisFile
		genesisConfig, err = genesis.ReadGenesisConfigFromFile(c.GenesisFile)
	} else {
		genesisConfig, err = genesis.MakeEmbeddedGenesisConfig()
		if err == genesis.ErrNoEmbeddedGenesis {
			log.Crit("no embedded genesis found and no genesis was specified")
			os.Exit(1)
		} else {
			log.Info("using embedded genesis")
			return
		}
	}

	if err == nil {
		fmt.Printf("Loaded a valid genesis config from path '%v'\n", path)
		log.Info("loaded a valid genesis config")
		return
	} else {
		log.Crit("no valid genesis file. Stopping ...", "reason", err)
		fmt.Printf("no valid genesis file. Reason: '%v'. Stopping ...\n", err)
		os.Exit(1)
		return
	}
}
func (c *Config) parseProducer(walletManager *wallet.Manager) (*wallet.KeyPair, error) {
	if c.Producer == nil {
		return nil, nil
	}

	// Unlock in wallet
	if _, err := walletManager.GetKeyFile(c.Producer.KeyFilePath); err != nil {
		log.Error("unable to get keyFile", "keyFilePath", c.Producer.KeyFilePath, "reason", err)
		return nil, err
	}
	if err := walletManager.Unlock(c.Producer.KeyFilePath, string(c.Producer.Password)); err != nil {
		log.Error("unable to unlock keyFile", "keyFilePath", c.Producer.KeyFilePath, "reason", err)
		return nil, err
	}

	// check address field is set & parse it
	if c.Producer.Address == "" {
		return nil, fmt.Errorf("unable to parse producer address. Reason:missing")
	}
	address, err := types.ParseAddress(c.Producer.Address)
	if err != nil {
		return nil, fmt.Errorf("unable to parse producer address. Reason:%w", err)
	}

	// get keyStore which should already be unlocked
	keyStore, err := walletManager.GetKeyStore(c.Producer.KeyFilePath)
	if err != nil {
		return nil, err
	}

	// derive coinbase
	_, keyPair, err := keyStore.DeriveForIndexPath(c.Producer.Index)
	if err != nil {
		return nil, err
	}

	// make sure address matches
	if keyPair.Address != address {
		return nil, errors.Errorf("producer address doesn't match. Expected %v but got %v", address, keyPair.Address)
	}

	return keyPair, nil
}

func (c *Config) makeWalletConfig() *wallet.Config {
	return &wallet.Config{WalletDir: c.WalletPath}
}
func (c *Config) makeNetConfig() *p2p.Net {
	networkDataDir := filepath.Join(c.DataPath, p2p.DefaultNetDirName)
	privateKeyFile := filepath.Join(c.DataPath, p2p.DefaultNetPrivateKeyFile)

	// Default the libp2p peerstore to a sibling of the legacy nodeDb
	// unless the operator set an explicit override in config.json. An
	// explicit empty string disables peer persistence rather than
	// falling back to the default path.
	peerstoreDir := filepath.Join(networkDataDir, p2p.DefaultPeerstoreDirName)
	if c.Net.PeerstoreDir != nil {
		peerstoreDir = *c.Net.PeerstoreDir
	}

	return &p2p.Net{
		PrivateKeyFile:    privateKeyFile,
		MaxPeers:          c.Net.MaxPeers,
		MaxPendingPeers:   c.Net.MaxPendingPeers,
		MinConnectedPeers: c.Net.MinConnectedPeers,
		Name:              fmt.Sprintf("%v %v", metadata.Version, c.Name),
		Seeders:           c.Net.Seeders,
		BootstrapPeers:    c.Net.BootstrapPeers,
		NATPortMap:        c.Net.NATPortMap,
		PeerstoreDir:      peerstoreDir,
		NodeDatabase:      networkDataDir,
		P2PBackend:        p2p.P2PBackend(c.Net.P2PBackend),
		ListenAddr:        c.Net.ListenHost,
		ListenPort:        c.Net.ListenPort,
	}
}

// joinHostPort builds a host:port listen address, handling IPv6 literals
// in both raw and pre-bracketed form.
//
// Malformed bracketed input (e.g. "[]", "[[]]", "[[::]]", "[0.0.0.0",
// "0.0.0.0]") is rejected with an error.
// net.SplitHostPort("[]:35997") returns host "" with a nil error, so an
// explicit error is the only thing callers can act on.
func joinHostPort(host string, port int) (string, error) {
	normalized, ok := normalizeListenHost(host)
	if !ok {
		return "", fmt.Errorf("malformed listen host %q", host)
	}
	return net.JoinHostPort(normalized, strconv.Itoa(port)), nil
}

// normalizeListenHost strips at most one enclosing bracket pair from a
// pre-bracketed IPv6 literal. It reports ok=false for empty bracketed ("[]"),
// unbalanced or nested input, so that such configuration is rejected rather
// than silently repaired into a valid address.
func normalizeListenHost(host string) (string, bool) {
	if host == "" {
		return "", true
	}
	if strings.HasPrefix(host, "[") {
		if !strings.HasSuffix(host, "]") {
			return "", false
		}
		inner := host[1 : len(host)-1]
		// "[]" and "[[]]" strip to an empty or bracket-bearing interior.
		// Returning "" would join to ":port", which net.Listen resolves as
		// a wildcard bind on every interface, so both must fail closed.
		if inner == "" || strings.ContainsAny(inner, "[]") {
			return "", false
		}
		return inner, true
	}
	if strings.ContainsAny(host, "[]") {
		return "", false
	}
	return host, true
}

func (c *Config) HTTPEndpoint() string {
	if c.RPC.HTTPHost == "" {
		return ""
	}
	endpoint, err := joinHostPort(c.RPC.HTTPHost, c.RPC.HTTPPort)
	if err != nil {
		// Display helper with no error path. A malformed host yields no
		// endpoint rather than a wrong one; callers that must not accept a
		// bad configuration use setListenAddr, which propagates.
		return ""
	}
	return endpoint
}
func (c *Config) WSEndpoint() string {
	if c.RPC.WSHost == "" {
		return ""
	}
	endpoint, err := joinHostPort(c.RPC.WSHost, c.RPC.WSPort)
	if err != nil {
		return ""
	}
	return endpoint
}
