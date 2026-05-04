package cli

import (
	"github.com/ashi-labs/gg/pkg/sync"
	"github.com/spf13/cobra"
)

func newRestackCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "restack",
		Aliases: []string{"rs"},
		Short:   "Restack every branch onto its parent without fetching from origin.",
		Args:    cobra.NoArgs,
		RunE:    runRestack,
	}
}

func runRestack(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	title := "restacking " + repo.ShortName()
	return runSyncWithProgress(sync.RunOpts{NoFetch: true}, title, nil)
}
