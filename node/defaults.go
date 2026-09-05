package node

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"

	"github.com/zenon-network/go-zenon/p2p"
)

const (
	DefaultWalletDir = "wallet"
)

var DefaultNodeConfig = Config{
	DataPath: DefaultDataDir(),

	Name: p2p.DefaultNodeName,

	LogLevel: "info",

	// RPC serves local clients only unless the operator opts in: the
	// listeners bind to loopback, HTTP accepts the localhost hostname and IP
	// literals as Host, and no cross-origin browser access is granted (the
	// WebSocket validator still admits localhost origins and non-browser
	// clients when the list is empty). A public endpoint needs explicit
	// hosts, and usually origins, in config.json or on the command line.
	RPC: RPCConfig{
		HTTPPort:   p2p.DefaultHTTPPort,
		HTTPHost:   "127.0.0.1",
		EnableHTTP: true,
		WSPort:     p2p.DefaultWSPort,
		WSHost:     "127.0.0.1",
		EnableWS:   true,

		HTTPVirtualHosts: []string{"localhost"},
	},
	Net: NetConfig{
		ListenHost:        p2p.DefaultListenHost,
		ListenPort:        p2p.DefaultListenPort,
		MinPeers:          p2p.DefaultMinPeers,
		MinConnectedPeers: p2p.DefaultMinConnectedPeers,
		MaxPeers:          p2p.DefaultMaxPeers,
		MaxPendingPeers:   p2p.DefaultMaxPendingPeers,
		Seeders:           p2p.DefaultSeeders,
		BootstrapPeers:    p2p.DefaultBootstrapPeers,
	},
}

// DefaultDataDir is the default data directory to use for the databases and other persistence requirements.
func DefaultDataDir() string {
	// Try to place the data folder in the user's home dir
	home := homeDir()
	if home != "" {
		switch runtime.GOOS {
		case "darwin":
			return filepath.Join(home, "Library", "znn")
		case "windows":
			// We used to put everything in %HOME%\AppData\Roaming, but this caused
			// problems with non-typical setups. If this fallback location exists and
			// is non-empty, use it, otherwise DTRT and check %LOCALAPPDATA%.
			fallback := filepath.Join(home, "AppData", "Roaming", "znn")
			appdata := windowsAppData()
			if appdata == "" || isNonEmptyDir(fallback) {
				return fallback
			}
			return filepath.Join(appdata, "znn")
		default:
			return filepath.Join(home, ".znn")
		}
	}
	// As we cannot guess a stable location, return empty and handle later
	return ""
}

func windowsAppData() string {
	v := os.Getenv("LOCALAPPDATA")
	if v == "" {
		// Windows XP and below don't have LocalAppData. Crash here because
		// we don't support Windows XP and undefining the variable will cause
		// other issues.
		panic("environment variable LocalAppData is undefined")
	}
	return v
}

func isNonEmptyDir(dir string) bool {
	f, err := os.Open(dir)
	if err != nil {
		return false
	}
	names, _ := f.Readdir(1)
	f.Close()
	return len(names) > 0
}

func homeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if usr, err := user.Current(); err == nil {
		return usr.HomeDir
	}
	return ""
}

func ReplaceHomeVariable(path string) string {
	if len(path) == 0 {
		return ""
	}
	if path[0] == '~' {
		return filepath.Join(homeDir(), path[1:])
	}
	return path
}
