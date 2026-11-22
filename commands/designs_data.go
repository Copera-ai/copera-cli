package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
)

const designDataPollInterval = 3 * time.Second
const designDataRevisionHelp = "Record revision as last read. Public submissions use a 22-character token"

var (
	designDataCollection = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)
	designDataKey        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	designDataSubject    = regexp.MustCompile(`^av_[A-Za-z0-9_-]{43}$`)
	designDataSubmission = regexp.MustCompile(`^(?:av_|pv_)[A-Za-z0-9_-]{43}$`)
)

func newDesignDataCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "data", Short: "Read or write records in an owned Design"}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return exitcodes.New(exitcodes.Usage, err)
	})
	cmd.AddCommand(
		newDesignDataGetCmd(cli),
		newDesignDataListCmd(cli),
		newDesignDataSetCmd(cli),
		newDesignDataBatchCmd(cli),
		newDesignDataDeleteCmd(cli),
		newDesignDataWatchCmd(cli),
	)
	return cmd
}

func designDataArgs(count int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(count)(cmd, args); err != nil {
			return exitcodes.New(exitcodes.Usage, err)
		}
		return nil
	}
}

func designDataClient(cli *CLI, args []string) (*api.Client, string, error) {
	id, err := designID(args[0])
	if err != nil {
		return nil, "", designUsageError(cli, err)
	}
	if !designDataCollection.MatchString(args[1]) {
		return nil, "", designUsageError(cli, fmt.Errorf("invalid Design collection name"))
	}
	if len(args) == 3 && !designDataKey.MatchString(args[2]) {
		return nil, "", designUsageError(cli, fmt.Errorf("invalid Design record key"))
	}
	client, _, err := requireAPIClient(cli)
	return client, id, err
}

func designDataError(cli *CLI, err error) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusBadRequest || apiErr.StatusCode == http.StatusRequestEntityTooLarge) {
		cli.Printer.PrintError(apiErr.Code, apiErr.Message, "", false)
		return exitcodes.New(exitcodes.Usage, err)
	}
	return designError(cli, err)
}

func printDesignData(cli *CLI, response json.RawMessage, err error) error {
	if err != nil {
		return designDataError(cli, err)
	}
	if cli.Printer.IsJSON() {
		return cli.Printer.PrintJSON(response)
	}
	compact, err := json.Marshal(response)
	if err != nil {
		return err
	}
	var escaped strings.Builder
	for _, r := range string(compact) {
		if unicode.IsControl(r) {
			fmt.Fprintf(&escaped, "\\u%04x", r)
		} else {
			escaped.WriteRune(r)
		}
	}
	_, err = fmt.Fprintf(cli.Printer.Out, "Design data: %s\n", escaped.String())
	return err
}

func validDesignDataObject(data []byte) bool {
	return utf8.Valid(data) && json.Valid(data) && bytes.TrimSpace(data)[0] == '{'
}

func readDesignDataObject(cli *CLI, filename string) (json.RawMessage, error) {
	if filename == "" {
		return nil, designUsageError(cli, fmt.Errorf("--file is required"))
	}
	reader := cli.Stdin
	if filename != "-" {
		file, err := os.Open(filename)
		if err != nil {
			return nil, designUsageError(cli, fmt.Errorf("could not open the Design data file"))
		}
		defer file.Close()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, api.ArtifactDataBodyLimit+1))
	if err != nil {
		return nil, designUsageError(cli, fmt.Errorf("could not read Design data"))
	}
	if len(data) > api.ArtifactDataBodyLimit {
		return nil, designUsageError(cli, fmt.Errorf("Design data file exceeds 24 KiB"))
	}
	if !validDesignDataObject(data) {
		return nil, designUsageError(cli, fmt.Errorf("--file must contain one JSON object"))
	}
	return bytes.TrimSpace(data), nil
}

func newDesignDataGetCmd(cli *CLI) *cobra.Command {
	var subject string
	cmd := &cobra.Command{
		Use:   "get <id|url> <collection> <key>",
		Short: "Read a Design record",
		Args:  designDataArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("subject") && !designDataSubmission.MatchString(subject) {
				return designUsageError(cli, fmt.Errorf("--subject must be a Design viewer or public visitor ID"))
			}
			client, id, err := designDataClient(cli, args)
			if err != nil {
				return err
			}
			response, err := client.ArtifactDataGet(cmd.Context(), id, args[1], args[2], subject)
			return printDesignData(cli, response, err)
		},
	}
	cmd.Flags().StringVar(&subject, "subject", "", "Viewer or public visitor ID of a shared submission")
	return cmd
}

func newDesignDataListCmd(cli *CLI) *cobra.Command {
	var opts api.ArtifactDataListOpts
	cmd := &cobra.Command{
		Use:   "list <id|url> <collection>",
		Short: "Read one page of Design records",
		Args:  designDataArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("limit") && (opts.Limit < 1 || opts.Limit > 20) {
				return designUsageError(cli, fmt.Errorf("--limit must be between 1 and 20"))
			}
			if cmd.Flags().Changed("filter") && !validDesignDataObject([]byte(opts.Filter)) {
				return designUsageError(cli, fmt.Errorf("--filter must be a JSON object"))
			}
			client, id, err := designDataClient(cli, args)
			if err != nil {
				return err
			}
			response, err := client.ArtifactDataList(cmd.Context(), id, args[1], opts)
			return printDesignData(cli, response, err)
		},
	}
	cmd.Flags().IntVar(&opts.Limit, "limit", 0, "Maximum records to return (1–20, default 20)")
	cmd.Flags().StringVar(&opts.Cursor, "cursor", "", "Continuation cursor")
	cmd.Flags().StringVar(&opts.Filter, "filter", "", "JSON object matching top-level field values by equality")
	return cmd
}

func newDesignDataSetCmd(cli *CLI) *cobra.Command {
	var file, subject, revisionFlag string
	var create bool
	cmd := &cobra.Command{
		Use:   "set <id|url> <collection> <key>",
		Short: "Create or replace a Design record",
		Args:  designDataArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			withRevision := cmd.Flags().Changed("if-revision")
			revision, err := api.ParseArtifactDataRevision(revisionFlag)
			if create == withRevision || (withRevision && err != nil) {
				return designUsageError(cli, fmt.Errorf("provide exactly one of --create or --if-revision with a positive safe integer or a 22-character token"))
			}
			if cmd.Flags().Changed("subject") && (create || !designDataSubmission.MatchString(subject)) {
				return designUsageError(cli, fmt.Errorf("--subject requires a Design viewer or public visitor ID and an existing revision"))
			}
			value, err := readDesignDataObject(cli, file)
			if err != nil {
				return err
			}
			client, id, err := designDataClient(cli, args)
			if err != nil {
				return err
			}
			input := api.ArtifactDataSetInput{Value: value}
			if withRevision {
				input.IfRevision = &revision
			}
			response, err := client.ArtifactDataSet(cmd.Context(), id, args[1], args[2], subject, input)
			return printDesignData(cli, response, err)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "JSON object file, or - for stdin")
	cmd.Flags().BoolVar(&create, "create", false, "Create the record only if its key is unused")
	cmd.Flags().StringVar(&revisionFlag, "if-revision", "", designDataRevisionHelp)
	cmd.Flags().StringVar(&subject, "subject", "", "Viewer or public visitor ID of an existing shared submission")
	return cmd
}

func newDesignDataBatchCmd(cli *CLI) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "batch <id|url> <collection>",
		Short: "Write multiple Design records atomically",
		Args:  designDataArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			input, err := readDesignDataObject(cli, file)
			if err != nil {
				return err
			}
			client, id, err := designDataClient(cli, args)
			if err != nil {
				return err
			}
			response, err := client.ArtifactDataBatch(cmd.Context(), id, args[1], input)
			return printDesignData(cli, response, err)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "JSON file with a records array of {key,subject?,ifRevision,value}, or - for stdin")
	return cmd
}

func newDesignDataDeleteCmd(cli *CLI) *cobra.Command {
	var subject, revisionFlag string
	cmd := &cobra.Command{
		Use:   "delete <id|url> <collection> <key>",
		Short: "Delete a Design record at a specific revision",
		Args:  designDataArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			revision, err := api.ParseArtifactDataRevision(revisionFlag)
			if err != nil {
				return designUsageError(cli, fmt.Errorf("--if-revision requires a positive safe integer or a 22-character token"))
			}
			if cmd.Flags().Changed("subject") && !designDataSubject.MatchString(subject) {
				return designUsageError(cli, fmt.Errorf("--subject must be a Design viewer ID"))
			}
			client, id, err := designDataClient(cli, args)
			if err != nil {
				return err
			}
			response, err := client.ArtifactDataDelete(cmd.Context(), id, args[1], args[2], subject, api.ArtifactDataDeleteInput{IfRevision: revision})
			return printDesignData(cli, response, err)
		},
	}
	cmd.Flags().StringVar(&revisionFlag, "if-revision", "", designDataRevisionHelp)
	cmd.Flags().StringVar(&subject, "subject", "", "Viewer ID of a shared submission")
	return cmd
}

func newDesignDataWatchCmd(cli *CLI) *cobra.Command {
	var cursor string
	var once bool
	cmd := &cobra.Command{
		Use:   "watch <id|url> <collection>",
		Short: "Check for changes to a Design's records",
		Args:  designDataArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, id, err := designDataClient(cli, args)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			return watchDesignData(ctx, cli, client, id, args[1], cursor, once)
		},
	}
	cmd.Flags().StringVar(&cursor, "cursor", "", "Watch cursor to resume from")
	cmd.Flags().BoolVar(&once, "once", false, "Check once and exit")
	return cmd
}

func watchDesignData(ctx context.Context, cli *CLI, client *api.Client, id, collection, cursor string, once bool) error {
	first := true
	for ctx.Err() == nil {
		response, err := client.ArtifactDataWatch(ctx, id, collection, cursor)
		if ctx.Err() != nil {
			return nil
		}
		if once {
			return printDesignData(cli, response, err)
		}
		delay := designDataPollInterval
		if err != nil {
			var apiErr *api.APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests || apiErr.Code != "rate_limit_exceeded" {
				return designDataError(cli, err)
			}
			delay = max(delay, time.Duration(apiErr.RetryAfterSeconds)*time.Second)
		} else {
			var change struct {
				Changed bool   `json:"changed"`
				Cursor  string `json:"cursor"`
			}
			if err := json.Unmarshal(response, &change); err != nil || change.Cursor == "" {
				return designDataError(cli, fmt.Errorf("invalid Design watch response"))
			}
			if first || change.Changed {
				if cli.Printer.IsJSON() {
					enc := json.NewEncoder(cli.Printer.Out)
					enc.SetEscapeHTML(false)
					err = enc.Encode(response)
				} else {
					err = printDesignData(cli, response, nil)
				}
				if err != nil {
					return err
				}
			}
			first = false
			cursor = change.Cursor
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
	return nil
}
