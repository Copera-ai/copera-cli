package commands

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/copera/copera-cli/internal/api"
	"github.com/spf13/cobra"
)

const designCommentPollInterval = 3 * time.Second

func newDesignWatchCmd(cli *CLI) *cobra.Command {
	var artifact, until, after string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "watch [id|url] --until comment",
		Short: "Wait for a new comment pin on a Design",
		RunE: func(cmd *cobra.Command, args []string) error {
			if until != "comment" {
				return designCollaborationUsage(cli, fmt.Errorf("--until must be comment"))
			}
			if timeout < time.Second || timeout > 24*time.Hour {
				return designCollaborationUsage(cli, fmt.Errorf("--timeout must be between 1s and 24h"))
			}
			id, err := designCollaborationID(cmd, args, artifact)
			if err != nil {
				return designCollaborationUsage(cli, err)
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			interruptible, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			ctx, cancel := context.WithTimeout(interruptible, timeout)
			defer cancel()
			return watchDesignComments(ctx, cli, client, id, after)
		},
	}
	setDesignCollaborationArgs(cmd, 0, 1)
	cmd.Flags().StringVar(&artifact, "artifact", "", "Design ID or URL (defaults to the linked checkout)")
	cmd.Flags().StringVar(&until, "until", "", "Event to wait for (comment)")
	cmd.Flags().StringVar(&after, "after", "", "Resume after this watch cursor")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "How long to watch (1s to 24h)")
	return cmd
}

func watchDesignComments(ctx context.Context, cli *CLI, client *api.Client, id, cursor string) error {
	ready := false
	for {
		result, err := client.ArtifactCommentWatch(ctx, id, cursor)
		if ctx.Err() != nil {
			return designWatchCanceled(cli, ctx.Err())
		}

		delay := designCommentPollInterval
		if err != nil {
			var apiErr *api.APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
				return designCollaborationError(cli, err)
			}
			delay = max(delay, time.Duration(apiErr.RetryAfterSeconds)*time.Second)
		} else {
			if result.Cursor == "" {
				return designCollaborationError(cli, fmt.Errorf("the Design comment watch returned an empty cursor"))
			}
			if !ready {
				fmt.Fprintf(cli.Printer.Err, "Watching for a new comment on %s.\n", id)
				ready = true
			}
			if result.Comment != nil {
				if cli.Printer.IsJSON() {
					return cli.Printer.PrintJSON(result)
				}
				printDesignComment(cli, *result.Comment)
				cli.Printer.PrintLine("Resume with --after " + safeDesignCommentText(result.Cursor))
				return nil
			}
			cursor = result.Cursor
		}
		select {
		case <-ctx.Done():
			return designWatchCanceled(cli, ctx.Err())
		case <-time.After(delay):
		}
	}
}

func designWatchCanceled(cli *CLI, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return designCollaborationError(cli, fmt.Errorf("timed out waiting for a new Design comment"))
	}
	return designCollaborationError(cli, fmt.Errorf("Design comment watch canceled"))
}
