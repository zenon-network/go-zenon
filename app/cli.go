package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"gopkg.in/urfave/cli.v1"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/metadata"
)

var (
	log = common.ZenonLogger.New()
	app = cli.NewApp()
)

func Run() {
	err := app.Run(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	app.Name = filepath.Base(os.Args[0])
	app.HideVersion = false
	app.Version = metadata.Version
	app.Compiled = time.Now()
	app.Authors = []cli.Author{
		{
			Name:  "The Zenon Developers",
			Email: "portal@zenon.network",
		},
	}
	app.Copyright = "Copyright 2021, Zenon"
	app.Usage = "znnd Node"

	//Import: Please add the New command here
	app.Commands = []cli.Command{
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
	if len(ctx.Args()) == 0 {
		max := runtime.NumCPU()
		fmt.Printf("Starting znnd.\n")
		fmt.Printf("current time is %v\n", time.Now().Format("2006-01-02 15:04:05"))
		fmt.Printf("version: %v\n", metadata.Version)
		fmt.Printf("git-commit-hash: %v\n", metadata.GitCommit)
		fmt.Printf("znnd will use at most %v cpu-cores\n", max)
		runtime.GOMAXPROCS(max)
	}
	return nil
}
func action(ctx *cli.Context) error {
	//Make sure No subCommands were entered,Only the flags
	if args := ctx.Args(); len(args) > 0 {
		return fmt.Errorf("invalid command: %q", args[0])
	}

	nodeManager, err := NewNodeManager(ctx)
	if err != nil {
		return err
	}

	return nodeManager.Start()
}
func afterAction(*cli.Context) error {
	return nil
}
