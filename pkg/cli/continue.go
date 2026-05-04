package cli

import (
	"github.com/ashi-labs/gg/pkg/sync"
	"github.com/spf13/cobra"
)

func newContinueCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "continue",
		Short: "Resume a paused sync or restack after resolving conflicts.",
		Args:  cobra.NoArgs,
		RunE:  runContinue,
	}
}

func runContinue(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	title := "resuming " + repo.ShortName()
	return runContinueWithProgress(sync.RunOpts{}, title)
}
