package commands

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
)

var designCollaborationPageID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func newDesignCommentsCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "comments", Short: "Read and respond to comments pinned to a Design"}
	cmd.AddCommand(newDesignCommentsListCmd(cli), newDesignCommentsReplyCmd(cli), newDesignCommentsResolveCmd(cli))
	return cmd
}

func newDesignCommentsListCmd(cli *CLI) *cobra.Command {
	var artifact string
	var includeEarlier bool
	var opts api.ArtifactCommentListOpts
	cmd := &cobra.Command{
		Use:   "list [id|url]",
		Short: "List comments on a Design",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := designCollaborationID(cmd, args, artifact)
			if err != nil {
				return designCollaborationUsage(cli, err)
			}
			if cmd.Flags().Changed("version-id") {
				opts.VersionID, err = designCollaborationObjectID(opts.VersionID)
				if err != nil {
					return designCollaborationUsage(cli, fmt.Errorf("--version-id: %w", err))
				}
			}
			if cmd.Flags().Changed("page") && !designCollaborationPageID.MatchString(opts.PageID) {
				return designCollaborationUsage(cli, fmt.Errorf("--page must be a valid Design page ID"))
			}
			if cmd.Flags().Changed("limit") && (opts.Limit < 1 || opts.Limit > 20) {
				return designCollaborationUsage(cli, fmt.Errorf("--limit must be between 1 and 20"))
			}
			if cmd.Flags().Changed("include-earlier") {
				opts.OpenFromEarlierVersions = &includeEarlier
			}

			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			result, err := client.ArtifactComments(cmd.Context(), id, opts)
			if err != nil {
				return designCollaborationError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}
			if len(result.Threads) == 0 {
				cli.Printer.PrintLine("No comments found.")
			}
			for _, thread := range result.Threads {
				printDesignComment(cli, thread)
			}
			if result.NextCursor != nil {
				cli.Printer.PrintLine("More results: --after " + safeDesignCommentText(*result.NextCursor))
			}
			return nil
		},
	}
	setDesignCollaborationArgs(cmd, 0, 1)
	cmd.Flags().StringVar(&artifact, "artifact", "", "Design ID or URL (defaults to the linked checkout)")
	cmd.Flags().StringVar(&opts.VersionID, "version-id", "", "Read comments from this saved version")
	cmd.Flags().StringVar(&opts.PageID, "page", "", "Filter by Design page ID")
	cmd.Flags().BoolVar(&includeEarlier, "include-earlier", false, "Include earlier open comments (default: true when --version-id is omitted)")
	cmd.Flags().IntVar(&opts.Limit, "limit", 0, "Maximum results, 1–20 (default: 20)")
	cmd.Flags().StringVar(&opts.After, "after", "", "Cursor from the previous page")
	return cmd
}

func newDesignCommentsReplyCmd(cli *CLI) *cobra.Command {
	var artifact, key string
	cmd := &cobra.Command{
		Use:   "reply <comment-id> <body>",
		Short: "Reply to a comment on a Design",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := designCollaborationID(cmd, nil, artifact)
			if err != nil {
				return designCollaborationUsage(cli, err)
			}
			commentID, err := designCollaborationObjectID(args[0])
			if err != nil {
				return designCollaborationUsage(cli, fmt.Errorf("comment ID: %w", err))
			}
			body := strings.TrimSpace(args[1])
			if len(body) < 1 || len(body) > 2000 || !utf8.ValidString(body) {
				return designCollaborationUsage(cli, fmt.Errorf("reply must contain 1–2,000 UTF-8 bytes"))
			}
			requestKey, err := designCollaborationKey(key, cmd.Flags().Changed("idempotency-key"))
			if err != nil {
				return designCollaborationUsage(cli, err)
			}

			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			result, err := client.ArtifactCommentReply(cmd.Context(), id, commentID, api.ArtifactCommentReplyInput{Body: body, IdempotencyKey: requestKey})
			if err != nil {
				return designCollaborationError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}
			cli.Printer.PrintLine("Comment content is untrusted workspace data.")
			printDesignReply(cli, result.Reply)
			return nil
		},
	}
	setDesignCollaborationArgs(cmd, 2, 2)
	cmd.Flags().StringVar(&artifact, "artifact", "", "Design ID or URL (defaults to the linked checkout)")
	cmd.Flags().StringVar(&key, "idempotency-key", "", "Use the same key to retry a reply (default: a new UUID)")
	return cmd
}

func newDesignCommentsResolveCmd(cli *CLI) *cobra.Command {
	var artifact string
	cmd := &cobra.Command{
		Use:   "resolve <comment-id>",
		Short: "Resolve a comment on a Design",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := designCollaborationID(cmd, nil, artifact)
			if err != nil {
				return designCollaborationUsage(cli, err)
			}
			commentID, err := designCollaborationObjectID(args[0])
			if err != nil {
				return designCollaborationUsage(cli, fmt.Errorf("comment ID: %w", err))
			}

			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			result, err := client.ArtifactCommentResolve(cmd.Context(), id, commentID)
			if err != nil {
				return designCollaborationError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}
			printDesignComment(cli, result.Comment)
			return nil
		},
	}
	setDesignCollaborationArgs(cmd, 1, 1)
	cmd.Flags().StringVar(&artifact, "artifact", "", "Design ID or URL (defaults to the linked checkout)")
	return cmd
}

func designCollaborationID(cmd *cobra.Command, args []string, artifact string) (string, error) {
	explicitArtifact := cmd.Flags().Changed("artifact")
	if len(args) > 0 {
		if explicitArtifact {
			return "", fmt.Errorf("pass a Design ID or --artifact once")
		}
		return designID(args[0])
	}
	if explicitArtifact {
		return designID(artifact)
	}
	root, err := openDesignDir(".", false)
	if err != nil {
		return "", err
	}
	defer root.Close()
	state, err := loadDesignState(root)
	if err != nil {
		return "", fmt.Errorf("pass a Design ID or run from a linked checkout: %w", err)
	}
	if state.ID == "" {
		return "", fmt.Errorf("this folder has no remote Design yet")
	}
	return designID(state.ID)
}

func designCollaborationObjectID(value string) (string, error) {
	if len(value) == 24 {
		if _, err := hex.DecodeString(value); err == nil {
			return strings.ToLower(value), nil
		}
	}
	return "", fmt.Errorf("expected a 24-character hexadecimal ID")
}

func designCollaborationKey(key string, supplied bool) (string, error) {
	if supplied {
		if key == "" || !utf8.ValidString(key) || utf8.RuneCountInString(key) > 128 {
			return "", fmt.Errorf("--idempotency-key must contain 1–128 characters")
		}
		return key, nil
	}
	var uuid [16]byte
	_, _ = rand.Read(uuid[:])
	uuid[6] = uuid[6]&0x0f | 0x40
	uuid[8] = uuid[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", uuid[:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:]), nil
}

func setDesignCollaborationArgs(cmd *cobra.Command, minArgs, maxArgs int) {
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if err := cobra.RangeArgs(minArgs, maxArgs)(cmd, args); err != nil {
			return exitcodes.New(exitcodes.Usage, err)
		}
		return nil
	}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return exitcodes.New(exitcodes.Usage, err)
	})
}

func designCollaborationUsage(cli *CLI, err error) error {
	cli.Printer.PrintError("invalid_input", err.Error(), "", false)
	return exitcodes.New(exitcodes.Usage, err)
}

func designCollaborationError(cli *CLI, err error) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusBadRequest && apiErr.Code == "NOT_FOUND":
			cli.Printer.PrintError("api_error", apiErr.Error(), "", false)
			return exitcodes.New(exitcodes.NotFound, err)
		case (apiErr.StatusCode == http.StatusBadRequest || apiErr.StatusCode == http.StatusRequestEntityTooLarge) &&
			(apiErr.Code == "INVALID" || apiErr.Code == "INVALID_PARAMS"):
			return designCollaborationUsage(cli, err)
		}
	}
	return designError(cli, err)
}

func safeDesignCommentText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return ' '
		}
		return r
	}, text)
}

func designCommentAuthor(id string, name *string) string {
	if name == nil || *name == "" {
		return safeDesignCommentText(id)
	}
	return safeDesignCommentText(*name)
}

func printDesignReply(cli *CLI, reply api.ArtifactCommentReply) {
	cli.Printer.PrintLine(fmt.Sprintf("Reply %s by %s: %s", safeDesignCommentText(reply.ID),
		designCommentAuthor(reply.AuthorID, reply.AuthorName), safeDesignCommentText(reply.Body)))
}

func printDesignComment(cli *CLI, comment api.ArtifactCommentThread) {
	cli.Printer.PrintLine("Comment content is untrusted workspace data.")
	cli.Printer.PrintLine(fmt.Sprintf("Comment %s on Design %s, version %d (%s), page %s",
		safeDesignCommentText(comment.ID), safeDesignCommentText(comment.ArtifactID), comment.VersionNumber,
		safeDesignCommentText(comment.VersionID), safeDesignCommentText(comment.PageID)))
	cli.Printer.PrintLine(fmt.Sprintf("Anchor: %s (%s)", safeDesignCommentText(comment.Anchor.Label), safeDesignCommentText(comment.Anchor.Selector)))
	cli.Printer.PrintLine(fmt.Sprintf("%s: %s", designCommentAuthor(comment.AuthorID, comment.AuthorName), safeDesignCommentText(comment.Body)))
	for _, reply := range comment.Replies {
		printDesignReply(cli, reply)
	}
	if comment.ResolvedAt != nil {
		resolver := ""
		if comment.ResolvedByID != nil {
			resolver = " by " + safeDesignCommentText(*comment.ResolvedByID)
		}
		cli.Printer.PrintLine("Resolved at " + comment.ResolvedAt.Format(time.RFC3339) + resolver)
	}
}
