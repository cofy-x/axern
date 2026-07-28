package app

import (
	"fmt"
	"io"

	imagecmd "github.com/cofy-x/axern/runtime/axnoded/axctl/commands/image"
	"github.com/cofy-x/axern/runtime/axnoded/axctl/commands/node"
	sandboxcmd "github.com/cofy-x/axern/runtime/axnoded/axctl/commands/sandbox"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/version"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"google.golang.org/grpc/grpclog"
)

var extraCmds = []cli.Command{}

func init() {
	// Discard grpc logs so that they don't mess with our stdio
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, io.Discard, io.Discard))

	cli.VersionPrinter = func(c *cli.Context) {
		fmt.Println(c.App.Name, c.App.Version)
	}
}

// New returns a *cli.App instance.
func New() *cli.App {
	app := cli.NewApp()
	app.Name = "axctl"
	app.Version = version.Version
	app.Description = `
axctl is a Linux operator CLI for interacting
with the local axnoded daemon on the current node.`
	app.Usage = "operate the local axnoded daemon"
	app.EnableBashCompletion = true
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "debug",
			Usage: "Enable debug output in logs",
		},
		cli.StringFlag{
			Name:   "address, a",
			Usage:  "local unix socket path for axnoded",
			Value:  config.DefaultSocketAddress,
			EnvVar: "AXNODED_SOCKET",
		},
		cli.DurationFlag{
			Name:  "timeout",
			Usage: "Total timeout(execute cli cmd and connect timeout) for axctl commands",
			Value: config.DefaultTimeout,
		},
	}
	app.Commands = append([]cli.Command{
		sandboxcmd.Command,
		imagecmd.Command,
		node.Command,
	}, extraCmds...)
	app.Before = func(context *cli.Context) error {
		logrus.SetLevel(logrus.InfoLevel)
		if context.GlobalBool("debug") {
			logrus.SetLevel(logrus.DebugLevel)
		}
		return nil
	}
	return app
}
