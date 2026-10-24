// Package app builds the znnd command-line application. It wires the
// urfave/cli app, its global flags and subcommands (version, license), the
// before/action/after hooks, and constructs and runs the node Manager that
// owns the node lifecycle. Run is the entry point invoked by cmd/znnd.
package app

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/metadata"
	"github.com/zenon-network/go-zenon/node"
)

var (
	log           = common.ZenonLogger.New()
	app           = cli.NewApp()
	nodeManager   *Manager
	nodeManagerMu sync.Mutex
)

// Run parses os.Args and runs the znnd cli application. On error it
// prints to stderr and exits the process with a non-zero status; it
// does not return in that case.
func Run() {
	err := app.Run(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// RunWithConfig starts a node from an explicit node.Config, bypassing
// CLI flag parsing entirely. It returns an error instead of exiting
// the process, making it suitable for library consumers (libznn) that
// embed the node and must remain alive on startup failures. The
// caller is responsible for calling Stop to shut the node down.
//
// Before the config is handed to the node it is initialised the same
// way MakeConfig initialises it: paths are made absolute (so that a
// DefaultNodeConfig with an empty WalletPath works) and logging is
// configured from the config's LogLevel and DataPath.
//
// A second call while a node is already running returns an error and
// leaves the first node's stop handle intact.
func RunWithConfig(cfg *node.Config) error {
	nodeManagerMu.Lock()
	defer nodeManagerMu.Unlock()

	if nodeManager != nil {
		return fmt.Errorf("node is already running")
	}
	if cfg == nil {
		return fmt.Errorf("config must not be nil")
	}
	if err := cfg.MakePathsAbsolute(); err != nil {
		return err
	}
	common.InitLogging(cfg.DataPath, cfg.LogLevel)

	m, err := NewNodeManagerFromConfig(cfg)
	if err != nil {
		return err
	}
	if err := m.startNode(); err != nil {
		m.Stop() // clean up partial start
		return err
	}
	nodeManager = m
	return nil
}

// Stop stops the running node Manager and reports a clean shutdown. It is
// exported for libznn and is a no-op when called before Run has assigned
// the manager. Manager.Stop logs a node shutdown error and returns nil, so
// the DealWithErr call here does not panic today.
func Stop() {
	nodeManagerMu.Lock()
	defer nodeManagerMu.Unlock()

	// exported for libznn; a consumer may call Stop before Run assigned the
	// manager, which would otherwise nil-deref inside Manager.Stop
	if nodeManager == nil {
		return
	}
	err := nodeManager.Stop()
	common.DealWithErr(err)
	nodeManager = nil
	fmt.Println("znnd successfully stopped")
}

func init() {
	app.Name = filepath.Base(os.Args[0])
	app.HideVersion = false
	app.Version = metadata.Version
	app.Compiled = time.Now()
	app.Authors = []*cli.Author{
		{
			Name:  "The Zenon Developers",
			Email: "portal@zenon.network",
		},
	}
	app.Copyright = "Copyright 2021, Zenon"
	app.Usage = "znnd Node"

	//Import: Please add the New command here
	app.Commands = []*cli.Command{
		versionCommand,
		licenseCommand,
	}
	sort.Sort(cli.CommandsByName(app.Commands))

	app.Flags = AllFlags

	app.Before = beforeAction
	app.Action = action
	app.After = afterAction
}
func beforeAction(ctx *cli.Context) error {

	max := runtime.NumCPU()
	fmt.Printf("Starting znnd.\n")
	fmt.Printf("current time is %v\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("version: %v\n", metadata.Version)
	fmt.Printf("git-commit-hash: %v\n", metadata.GitCommit)
	fmt.Printf("znnd will use at most %v cpu-cores\n", max)
	runtime.GOMAXPROCS(max)

	// pprof server
	if pprofRequested(ctx) {
		listenHost := ctx.String(PprofAddrFlag.Name)

		port := ctx.Int(PprofPortFlag.Name)

		address := fmt.Sprintf("%s:%d", listenHost, port)

		log.Info("Starting pprof server", "addr", fmt.Sprintf("http://%s/debug/pprof", address))
		go func() {
			if err := http.ListenAndServe(address, nil); err != nil {
				log.Error("Failure in running pprof server", "err", err)
			}
		}()
	}

	return nil
}

// pprofRequested reports whether the pprof server should start: the
// value of --pprof, so that --pprof=false keeps it off. (Testing the
// flag's presence instead used to start it for --pprof=false.)
func pprofRequested(ctx *cli.Context) bool {
	return ctx.Bool(PprofFlag.Name)
}

func action(ctx *cli.Context) error {
	//Make sure No subCommands were entered,Only the flags
	if args := ctx.Args(); args.Len() > 0 {
		return fmt.Errorf("invalid command: %q", args.Get(0))
	}
	m, err := NewNodeManager(ctx)
	if err != nil {
		return err
	}
	nodeManagerMu.Lock()
	nodeManager = m
	nodeManagerMu.Unlock()

	if err := nodeManager.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return nil
}

func afterAction(*cli.Context) error {
	return nil
}
