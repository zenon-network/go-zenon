package app

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/metadata"
)

var (
	log         = common.ZenonLogger.New()
	app         = cli.NewApp()
	nodeManager *Manager
)

func Run() {
	err := app.Run(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func Stop() {
	err := nodeManager.Stop()
	common.DealWithErr(err)
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
	if ctx.IsSet(PprofFlag.Name) {
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

func action(ctx *cli.Context) error {
	//Make sure No subCommands were entered,Only the flags
	if args := ctx.Args(); args.Len() > 0 {
		return fmt.Errorf("invalid command: %q", args.Get(0))
	}
	var err error
	nodeManager, err = NewNodeManager(ctx)
	if err != nil {
		return err
	}

	return nodeManager.Start()
}

func afterAction(*cli.Context) error {
	return nil
}
