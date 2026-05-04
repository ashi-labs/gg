package cli

import (
	"fmt"

	"github.com/ashi-labs/gg/pkg/tui/style"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// styledHelpFunc replaces cobra's default help output with a palette-
// aware version. Structure mirrors cobra's defaultHelpTemplate /
// defaultUsageTemplate (keep in sync with cobra.go if upstream
// templates change): section labels render in Hint, command names in
// Branch, descriptions in Dim — so the eye has a left column of
// identifiers and a right column of prose.
//
// Wired up via rootCmd.SetHelpFunc so every subcommand inherits the
// look without opting in.
func styledHelpFunc(cmd *cobra.Command, _ []string) {
	header := style.Stderr.Dirty
	shell := style.Stderr.Trunk
	dim := style.Stderr.Dim
	cmdName := style.Stderr.Branch
	indent := "  "
	// Description (long, else short).
	if long := cmd.Long; long != "" {
		plainln(long)
		plainln("")
	} else if short := cmd.Short; short != "" {
		plainln(short)
		plainln("")
	}
	plainln(header.Render("Usage:"))
	if cmd.Runnable() {
		plainln(indent + cmd.UseLine())
	}
	if cmd.HasAvailableSubCommands() {
		line := fmt.Sprintf(indent+"%s %s", cmd.CommandPath(), dim.Render("[command]"))
		plainln(line)
	}
	if len(cmd.Aliases) > 0 {
		plainln("")
		plainln(header.Render("Aliases:"))
		plainln(indent + cmd.NameAndAliases())
	}
	if cmd.HasExample() {
		plainln("")
		plainln(header.Render("Examples:"))
		plainln(cmd.Example)
	}
	if cmd.HasAvailableSubCommands() {
		writeCommandListing(cmd, header, cmdName, dim)
	}
	if cmd.HasAvailableLocalFlags() {
		plainln("")
		plainln(header.Render("Flags:"))
		plainln(cmd.LocalFlags().FlagUsages())
	}
	if cmd.HasAvailableInheritedFlags() {
		plainln("")
		plainln(header.Render("Global Flags:"))
		plainln(cmd.InheritedFlags().FlagUsages())
	}
	if cmd.HasAvailableSubCommands() {
		plainln("")
		command := shell.Render(fmt.Sprintf("%s [command] --help", cmd.CommandPath()))
		line := fmt.Sprintf("Use %s for more information about a command.", command)
		plainln(line)
	}
}

// writeCommandListing emits "Available Commands:" (or grouped variants)
// with command names in cmdName style, descriptions in dim, and group
// titles in the header style.
func writeCommandListing(cmd *cobra.Command, headerSty, cmdName, dim lipgloss.Style) {
	cmds := cmd.Commands()
	pad := cmd.NamePadding() // longest name width; used to column-align descriptions
	// Flat list when there are no groups.
	if len(cmd.Groups()) == 0 {
		plainln("")
		plainln(headerSty.Render("Available Commands:"))
		for _, sub := range cmds {
			if !sub.IsAvailableCommand() && sub.Name() != "help" {
				continue
			}
			writeCmdRow(sub, pad, cmdName, dim)
		}
		return
	}
	// Grouped layout: one section per group, optional "Additional Commands"
	// bucket for strays.
	for _, group := range cmd.Groups() {
		plainln("")
		plainln(headerSty.Render(group.Title))
		for _, sub := range cmds {
			if sub.GroupID != group.ID {
				continue
			}
			if !sub.IsAvailableCommand() && sub.Name() != "help" {
				continue
			}
			writeCmdRow(sub, pad, cmdName, dim)
		}
	}
	if !cmd.AllChildCommandsHaveGroup() {
		plainln("")
		plainln(headerSty.Render("Additional Commands:"))
		for _, sub := range cmds {
			if sub.GroupID != "" {
				continue
			}
			if !sub.IsAvailableCommand() && sub.Name() != "help" {
				continue
			}
			writeCmdRow(sub, pad, cmdName, dim)
		}
	}
}

// writeCmdRow prints "  <name>    <short>" with name left-padded to the
// longest-name width so descriptions line up across rows.
func writeCmdRow(sub *cobra.Command, pad int, cmdName, dim lipgloss.Style) {
	// Pad BEFORE styling so ANSI escapes don't inflate the width.
	padded := fmt.Sprintf("%-*s", pad, sub.Name())
	indent := "  "
	line := fmt.Sprintf(indent+"%s  %s", cmdName.Render(padded), dim.Render(sub.Short))
	plainln(line)
}
