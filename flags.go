package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/kong"
)

type VersionFlag bool

func (v VersionFlag) Decode(ctx *kong.DecodeContext) error { return nil }
func (v VersionFlag) IsBool() bool                         { return true }
func (v VersionFlag) BeforeApply(app *kong.Kong, vars kong.Vars) error {
	fmt.Println(serviceName + ": " + serviceVersion)
	os.Exit(0)
	return nil
}

// Flags supplied to cli.
type Flags struct {
	Version    VersionFlag `name:"version" help:"Print version information and quit"`
	ConfigPath string      `help:"The path to the config file" optional:"" type:"existingfile"`
	Log        *LogConfig  `embed:"" prefix:"log-"`
	Server     ServerCmd   `cmd:"" help:"Run the Virtual VXLAN service"`
	Service    ServiceCmd  `cmd:"" help:"Manage the Virtual VXLAN service"`
	Listener   ListenerCmd `cmd:"" help:"Manage listeners"`
	Config     ConfigCmd   `cmd:"" help:"Manage configuration"`
	Update     UpdateCmd   `cmd:"" help:"Check for updates and apply"`
}

var flags *Flags

// Parse the supplied flags.
func ParseFlags() *kong.Context {
	flags = new(Flags)

	ctx := kong.Parse(flags,
		kong.Name(serviceName),
		kong.Description(serviceDescription),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
		}),
		kong.Vars{
			"serviceActions": strings.Join(ServiceAction, ","),
		},
	)
	return ctx
}
