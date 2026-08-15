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
		"sandbox",
		"image",
		"node",
	}, commandNames)

	var sandboxCmd *cli.Command
	for idx := range app.Commands {
		if app.Commands[idx].Name == "sandbox" {
			sandboxCmd = &app.Commands[idx]
			break
		}
	}
	if sandboxCmd == nil {
		t.Fatal("sandbox command not registered")
	}

	sandboxSubcommandNames := make([]string, 0, len(sandboxCmd.Subcommands))
	for _, command := range sandboxCmd.Subcommands {
		sandboxSubcommandNames = append(sandboxSubcommandNames, command.Name)
	}
	assert.ElementsMatch(t, []string{"list", "inspect", "diagnostics", "memory", "exec", "wait", "kill", "delete"}, sandboxSubcommandNames)

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
