package cli

import (
	"github.com/ashi-labs/gg/pkg/sync"
	"github.com/spf13/cobra"
)

func newAbortCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "abort",
		Short: "abort a paused sync/restack and reset every branch to its pre-sync sha.",
		Args:  cobra.NoArgs,
		RunE:  runAbort,
	}
}

func runAbort(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	if err := sync.Abort(repo, bare); err != nil {
		return err
	}
	successf("aborted")
	return nil
}
