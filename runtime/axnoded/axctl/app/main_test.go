package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli"
)

func TestNewRegistersSupportedOperatorCommands(t *testing.T) {
	app := New()

	commandNames := make([]string, 0, len(app.Commands))
	for _, command := range app.Commands {
		commandNames = append(commandNames, command.Name)
	}

	assert.ElementsMatch(t, []string{
		"allocation",
		"image",
		"node",
	}, commandNames)

	var allocationCmd *cli.Command
	for idx := range app.Commands {
		if app.Commands[idx].Name == "allocation" {
			allocationCmd = &app.Commands[idx]
			break
		}
	}
	if allocationCmd == nil {
		t.Fatal("allocation command not registered")
	}

	allocationSubcommandNames := make([]string, 0, len(allocationCmd.Subcommands))
	for _, command := range allocationCmd.Subcommands {
		allocationSubcommandNames = append(allocationSubcommandNames, command.Name)
	}
	assert.ElementsMatch(t, []string{"list", "inspect", "diagnostics", "network-policy", "memory", "exec", "wait", "force-terminate", "force-cleanup"}, allocationSubcommandNames)

	var nodeCmd *cli.Command
	for idx := range app.Commands {
		if app.Commands[idx].Name == "node" {
			nodeCmd = &app.Commands[idx]
			break
		}
	}
	if nodeCmd == nil {
		t.Fatal("node command not registered")
	}

	subcommandNames := make([]string, 0, len(nodeCmd.Subcommands))
	for _, command := range nodeCmd.Subcommands {
		subcommandNames = append(subcommandNames, command.Name)
	}
	assert.ElementsMatch(t, []string{"check", "resources"}, subcommandNames)

	var imageCmd *cli.Command
	for idx := range app.Commands {
		if app.Commands[idx].Name == "image" {
			imageCmd = &app.Commands[idx]
			break
		}
	}
	if imageCmd == nil {
		t.Fatal("image command not registered")
	}

	imageSubcommandNames := make([]string, 0, len(imageCmd.Subcommands))
	for _, command := range imageCmd.Subcommands {
		imageSubcommandNames = append(imageSubcommandNames, command.Name)
	}
	assert.ElementsMatch(t, []string{"import", "list", "inspect", "mounts", "drop-page-cache"}, imageSubcommandNames)
}
