package commands

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/copera/copera-cli/internal/api"
	"github.com/spf13/cobra"
)

func newDesignAskCmd(cli *CLI) *cobra.Command {
	var artifact, page, base, key string
	var yes bool
	cmd := &cobra.Command{
		Use:   "ask <request>",
		Short: "Queue a Design change with Copera AI",
		Long:  "Queue a Design change with Copera AI. Requires a personal access token with both Designs and AI permissions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			intent := strings.TrimSpace(args[0])
			if intent == "" || !utf8.ValidString(intent) || len(utf16.Encode([]rune(intent))) > 8192 {
				return designCollaborationUsage(cli, fmt.Errorf("request must contain 1–8,192 UTF-16 code units"))
			}
			id, err := designCollaborationID(cmd, nil, artifact)
			if err != nil {
				return designCollaborationUsage(cli, err)
			}
			if cmd.Flags().Changed("page") && !designCollaborationPageID.MatchString(page) {
				return designCollaborationUsage(cli, fmt.Errorf("--page must be a valid Design page ID"))
			}
			baseVersionID := base
			if cmd.Flags().Changed("base-version-id") {
				baseVersionID, err = designCollaborationObjectID(base)
				if err != nil {
					return designCollaborationUsage(cli, fmt.Errorf("--base-version-id: %w", err))
				}
			}
			requestKey, err := designCollaborationKey(key, cmd.Flags().Changed("idempotency-key"))
			if err != nil {
				return designCollaborationUsage(cli, err)
			}
			if err := confirmDesignAsk(cli, yes); err != nil {
				return err
			}

			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if baseVersionID == "" {
				remote, err := client.ArtifactGet(ctx, id)
				if err != nil {
					return designCollaborationError(cli, err)
				}
				baseVersionID, err = designCollaborationObjectID(remote.CurrentDraftVersionID)
				if err != nil {
					return designCollaborationError(cli, fmt.Errorf("the API did not return a current draft version ID"))
				}
			}
			result, err := client.ArtifactAsk(ctx, id, api.ArtifactAskInput{
				BaseVersionID: baseVersionID, PageID: page, Intent: intent,
				IdempotencyKey: requestKey, ConfirmAICredits: true,
			})
			if err == nil {
				if _, idErr := designCollaborationObjectID(result.Request.ID); idErr != nil || result.Request.ArtifactID != id || !result.SpendsAICredits {
					err = fmt.Errorf("the API did not return a valid Design authoring receipt")
				}
			}
			if err != nil {
				var apiErr *api.APIError
				if !errors.As(err, &apiErr) || apiErr.StatusCode >= http.StatusInternalServerError {
					fmt.Fprintf(cli.Printer.Err, "The request may have been accepted. Retry with --base-version-id %s and --idempotency-key %q, using the same Design, page and request.\n", baseVersionID, requestKey)
				}
				return designCollaborationError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}
			cli.Printer.PrintLine(fmt.Sprintf("Request %s (%s)", safeDesignCommentText(result.Request.ID), safeDesignCommentText(result.Request.State)))
			cli.Printer.PrintLine("Open the Design in Copera and check its activity for progress.")
			return nil
		},
	}
	setDesignCollaborationArgs(cmd, 1, 1)
	cmd.Flags().StringVar(&artifact, "artifact", "", "Design ID or URL (defaults to the linked checkout)")
	cmd.Flags().StringVar(&page, "page", "", "Design page ID (defaults to the base version's default page)")
	cmd.Flags().StringVar(&base, "base-version-id", "", "Base version ID (defaults to the current server draft)")
	cmd.Flags().StringVar(&key, "idempotency-key", "", "To retry, use the same key, base version, page and request (default: a new UUID)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm that the workspace will spend AI credits")
	return cmd
}

func confirmDesignAsk(cli *CLI, yes bool) error {
	fmt.Fprintln(cli.Printer.Err, "This request uses your workspace's AI credits when Copera AI runs it.")
	if yes {
		return nil
	}
	input, ok := cli.Stdin.(*os.File)
	if cli.IsNonInteractive() || !ok || !stderrIsTTY(input) {
		return designCollaborationUsage(cli, fmt.Errorf("pass --yes to confirm spending AI credits without an interactive terminal"))
	}
	fmt.Fprint(cli.Printer.Err, "Queue this request? [y/N] ")
	answer, err := bufio.NewReader(input).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if err != nil || (answer != "y" && answer != "yes") {
		return designCollaborationUsage(cli, fmt.Errorf("AI credit spending was not confirmed"))
	}
	return nil
}
