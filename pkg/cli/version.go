package cli

import (
	"fmt"

	"github.com/ashi-labs/gg/pkg/tui/style"
	"github.com/ashi-labs/gg/pkg/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Aliases: []string{"v"},
		Short:   "print version and build info.",
		Run: func(cmd *cobra.Command, args []string) {
			name := "git-good (gg)"
			vBadge := style.StdoutRenderer.NewStyle().
				Foreground(style.Current().Background).
				Background(style.Current().Green).
				Padding(0, 1)
			line := fmt.Sprintf(
				"%s%s",
				style.Stdout.Badge.Render(name),
				vBadge.Render(version.Build()),
			)
			plainln(line)
		},
	}
}
