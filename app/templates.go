package app

import (
	"github.com/urfave/cli/v2"
)

func init() {
	cli.AppHelpTemplate = `NAME:
	{{.Name}} - the Zenon command line interface

USAGE:
	{{.Name}} {{if .Flags}}[global options] {{end}}command{{if .Flags}} [command options]{{end}} [arguments...]

VERSION:
   {{.Version}}

COMMANDS:
   {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
   {{end}}{{if .Flags}}
ZENON OPTIONS:
   {{range .Flags}}{{.}}
   {{end}}{{end}}
COPYRIGHT:
	Copyright 2020-2021 The Zenon Developers
`

	cli.CommandHelpTemplate = `{{.app.Name}}{{if .app.Subcommands}} command{{end}}{{if .app.Flags}} [command options]{{end}} [arguments...]
{{if .app.Description}}{{.app.Description}}
{{end}}{{if .app.Subcommands}}
SUBCOMMANDS:
	{{range .app.Subcommands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
	{{end}}{{end}}{{if .categorizedFlags}}
{{range $idx, $categorized := .categorizedFlags}}{{$categorized.Name}} OPTIONS:
{{range $categorized.Flags}}{{"\t"}}{{.}}
{{end}}
{{end}}{{end}}`
}
