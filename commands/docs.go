package commands

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
)

func newDocsCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Manage documents",
	}
	cmd.AddCommand(
		newDocsTreeCmd(cli),
		newDocsSearchCmd(cli),
		newDocsGetCmd(cli),
		newDocsContentCmd(cli),
		newDocsUpdateCmd(cli),
		newDocsMetadataCmd(cli),
		newDocsCreateCmd(cli),
		newDocsDeleteCmd(cli),
	)
	return cmd
}

// ── docs tree ─────────────────────────────────────────────────────────────────

func newDocsTreeCmd(cli *CLI) *cobra.Command {
	var flagParent string

	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Show document tree",
		Long: `Show documents at the workspace root or under a specific parent.

Each node shows its direct child count. Use --parent <id> to drill into a subtree.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			nodes, err := client.DocTree(context.Background(), flagParent)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(nodes)
			}

			if len(nodes) == 0 {
				cli.Printer.Info("No documents found.")
				return nil
			}
			printDocTree(cli, nodes, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&flagParent, "parent", "", "Show subtree under this doc ID (default: workspace root)")
	return cmd
}

func printDocTree(cli *CLI, nodes []api.DocNode, prefix string) {
	for i, node := range nodes {
		last := i == len(nodes)-1
		conn := "├── "
		childPrefix := prefix + "│   "
		if last {
			conn = "└── "
			childPrefix = prefix + "    "
		}
		cli.Printer.PrintLine(fmt.Sprintf("%s%s%s  (%s)", prefix, conn, node.Title, node.ID))

		if len(node.Children) > 0 {
			printDocTree(cli, node.Children, childPrefix)
		}
	}
}

// ── docs search ───────────────────────────────────────────────────────────────

func newDocsSearchCmd(cli *CLI) *cobra.Command {
	var flagSortBy, flagSortOrder string
	var flagLimit int

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search documents",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			hits, err := client.DocSearch(context.Background(), args[0], api.SearchOpts{
				SortBy:    flagSortBy,
				SortOrder: flagSortOrder,
				Limit:     flagLimit,
			})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(hits)
			}

			if len(hits) == 0 {
				cli.Printer.Info("No results for %q.", args[0])
				return nil
			}

			for i, h := range hits {
				match := h.Highlight.MdBody
				if match == "" {
					match = h.Highlight.Title
				}
				cli.Printer.PrintLine(fmt.Sprintf("ID:        %s", h.ID))
				cli.Printer.PrintLine(fmt.Sprintf("Title:     %s", h.Title))
				cli.Printer.PrintLine(fmt.Sprintf("Match:     %s", match))
				if len(h.Parents) > 0 {
					parts := make([]string, len(h.Parents))
					for j, p := range h.Parents {
						parts[j] = fmt.Sprintf("%s (%s)", p.Title, p.ID)
					}
					cli.Printer.PrintLine(fmt.Sprintf("Ancestors: %s", strings.Join(parts, " > ")))
				}
				if i < len(hits)-1 {
					cli.Printer.PrintLine("")
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagSortBy, "sort-by", "", "Sort field: createdAt|updatedAt")
	cmd.Flags().StringVar(&flagSortOrder, "sort-order", "", "Sort direction: asc|desc")
	cmd.Flags().IntVar(&flagLimit, "limit", 0, "Max results (1–50)")
	return cmd
}

// ── docs get ──────────────────────────────────────────────────────────────────

func newDocsGetCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "get [id]",
		Short: "Get document metadata",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			id, err := resolveDocID(args, cfg.DocID)
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(), "Pass a doc ID or set doc_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			doc, err := client.DocGet(context.Background(), id)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(doc)
			}

			cli.Printer.PrintLine(fmt.Sprintf("ID:       %s", doc.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Title:    %s", doc.Title))
			if doc.ParentID != "" {
				cli.Printer.PrintLine(fmt.Sprintf("Parent:   %s", doc.ParentID))
			}
			if doc.OwnerID != "" {
				cli.Printer.PrintLine(fmt.Sprintf("Owner:    %s", doc.OwnerID))
			}
			cli.Printer.PrintLine(fmt.Sprintf("Created:  %s", doc.CreatedAt.Format("2006-01-02 15:04:05")))
			cli.Printer.PrintLine(fmt.Sprintf("Updated:  %s", doc.UpdatedAt.Format("2006-01-02 15:04:05")))
			return nil
		},
	}
}

// ── docs content ──────────────────────────────────────────────────────────────

func newDocsContentCmd(cli *CLI) *cobra.Command {
	var flagNoCache bool

	cmd := &cobra.Command{
		Use:   "content [id]",
		Short: "Get document markdown content",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			id, err := resolveDocID(args, cfg.DocID)
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(), "Pass a doc ID or set doc_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			docCache := newDocCache(cli, cfg)

			if !flagNoCache {
				if content, ok := docCache.Get(id); ok {
					cli.Printer.PrintLine(content)
					return nil
				}
			}

			content, err := client.DocContent(context.Background(), id)
			if err != nil {
				return apiError(cli, err)
			}

			if !flagNoCache {
				docCache.Set(id, content)
			}

			cli.Printer.PrintLine(content)
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagNoCache, "no-cache", false, "Bypass cache and fetch fresh content")
	return cmd
}

// ── docs update ───────────────────────────────────────────────────────────────

func newDocsUpdateCmd(cli *CLI) *cobra.Command {
	var flagOperation string
	var flagContent string

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update document content",
		Long: `Update a document's markdown content.

Content is read from --content or stdin. The update is processed asynchronously
(the server returns 202 Accepted immediately).

Operations:
  replace  — replace entire content (default)
  append   — add to the end
  prepend  — add to the beginning`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			content := flagContent
			if content == "" {
				content, err = readStdinContent(cli)
				if err != nil {
					cli.Printer.PrintError("input_error", err.Error(), "Pipe content via stdin or use --content", false)
					return exitcodes.New(exitcodes.Usage, err)
				}
			}

			if err := client.DocUpdateContent(context.Background(), args[0], flagOperation, content); err != nil {
				return apiError(cli, err)
			}

			// Invalidate cache after successful update
			docCache := newDocCache(cli, cfg)
			docCache.Delete(args[0])

			cli.Printer.Info("Content update queued (operation: %s). Processing is async.", flagOperation)
			return nil
		},
	}
	cmd.Flags().StringVar(&flagOperation, "operation", "replace", "Update operation: replace|append|prepend")
	cmd.Flags().StringVar(&flagContent, "content", "", "Content text (reads stdin if not set)")
	return cmd
}

// ── docs metadata ────────────────────────────────────────────────────────────

func newDocsMetadataCmd(cli *CLI) *cobra.Command {
	var flagTitle, flagIcon, flagCover string

	cmd := &cobra.Command{
		Use:   "metadata <doc-id>",
		Short: "Update document title, icon, or cover",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			updates := map[string]string{}
			if flagTitle != "" {
				updates["title"] = flagTitle
			}
			if flagIcon != "" {
				updates["icon"] = flagIcon
			}
			if flagCover != "" {
				updates["cover"] = flagCover
			}
			if len(updates) == 0 {
				cli.Printer.PrintError("missing_input", "at least one of --title, --icon, or --cover is required", "", false)
				return exitcodes.Newf(exitcodes.Usage, "no updates provided")
			}

			doc, err := client.DocUpdateMeta(context.Background(), args[0], updates)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(doc)
			}

			cli.Printer.PrintLine(fmt.Sprintf("ID:      %s", doc.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Title:   %s", doc.Title))
			cli.Printer.PrintLine(fmt.Sprintf("Updated: %s", doc.UpdatedAt.Format("2006-01-02 15:04")))
			cli.Printer.Info("Document metadata updated.")
			return nil
		},
	}
	cmd.Flags().StringVar(&flagTitle, "title", "", "New document title")
	cmd.Flags().StringVar(&flagIcon, "icon", "", "Document icon value")
	cmd.Flags().StringVar(&flagCover, "cover", "", "Document cover value")
	return cmd
}

// ── docs create ───────────────────────────────────────────────────────────────

func newDocsCreateCmd(cli *CLI) *cobra.Command {
	var flagTitle, flagParent, flagContent string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new document",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagTitle == "" {
				cli.Printer.PrintError("missing_flag", "--title is required", "", false)
				return exitcodes.Newf(exitcodes.Usage, "--title is required")
			}

			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			content := flagContent
			// Read from stdin only if explicitly piped (non-interactive) and no --content flag
			if content == "" && cli.IsNonInteractive() {
				content, _ = readStdinContent(cli)
			}

			doc, err := client.DocCreate(context.Background(), flagTitle, flagParent, content)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(doc)
			}

			cli.Printer.Info("Created document %q (%s)", doc.Title, doc.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&flagTitle, "title", "", "Document title (required)")
	cmd.Flags().StringVar(&flagParent, "parent", "", "Parent doc ID")
	cmd.Flags().StringVar(&flagContent, "content", "", "Initial markdown content (reads stdin if piped)")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

// ── docs delete ───────────────────────────────────────────────────────────────

func newDocsDeleteCmd(cli *CLI) *cobra.Command {
	var flagForce bool

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a document",
		Long:  `Soft-delete a document. Only the document owner can delete.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			if !flagForce && !cli.IsNonInteractive() {
				r := bufio.NewReader(cli.Stdin)
				fmt.Fprintf(cli.Printer.Out, "Delete document %q? [y/N]: ", args[0])
				ans, _ := r.ReadString('\n')
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ans)), "y") {
					cli.Printer.Info("Aborted.")
					return nil
				}
			} else if !flagForce {
				cli.Printer.PrintError("confirmation_required", "use --force to delete without confirmation", "", false)
				return exitcodes.Newf(exitcodes.Usage, "use --force to delete without confirmation")
			}

			if err := client.DocDelete(context.Background(), args[0]); err != nil {
				return apiError(cli, err)
			}

			// Invalidate cache
			docCache := newDocCache(cli, cfg)
			docCache.Delete(args[0])

			cli.Printer.Info("Document %s deleted.", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagForce, "force", false, "Skip confirmation prompt")
	return cmd
}

func resolveDocID(args []string, configDefault string) (string, error) {
	return resolveID(args, "", configDefault, "doc ID")
}

func readStdinContent(cli *CLI) (string, error) {
	var sb strings.Builder
	scanner := bufio.NewScanner(cli.Stdin)
	for scanner.Scan() {
		sb.WriteString(scanner.Text())
		sb.WriteString("\n")
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	content := strings.TrimRight(sb.String(), "\n")
	if content == "" {
		return "", fmt.Errorf("content cannot be empty")
	}
	return content, nil
}

