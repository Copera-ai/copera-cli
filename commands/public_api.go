package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

type endpointSpec struct {
	Use          string
	Short        string
	Method       string
	Path         string
	Args         int
	Body         bool
	BodyRequired bool
	Destructive  bool
}

// newEndpointCmd is the common escape hatch for structured public API
// operations. --data accepts inline JSON, @file, or - (stdin); --field values
// override top-level JSON properties and are decoded as JSON scalars when
// possible. --query is repeatable and URL-encodes query parameters.
func newEndpointCmd(cli *CLI, spec endpointSpec) *cobra.Command {
	var rawData string
	var fields, queries []string
	var force bool
	cmd := &cobra.Command{
		Use:   spec.Use,
		Short: spec.Short,
		Args:  cobra.ExactArgs(spec.Args),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			if spec.Destructive {
				ok, err := confirmDestructive(cli, force, fmt.Sprintf("Proceed with %s? [y/N]: ", cmd.CommandPath()))
				if err != nil || !ok {
					return err
				}
			}

			path := spec.Path
			for i, arg := range args {
				path = strings.ReplaceAll(path, fmt.Sprintf("{%d}", i), url.PathEscape(arg))
			}
			query, err := parsePairs(queries, false)
			if err != nil {
				return usageError(cli, err)
			}
			if len(query) > 0 {
				values := url.Values{}
				for key, value := range query {
					values.Set(key, fmt.Sprint(value))
				}
				path += "?" + values.Encode()
			}

			var body any
			if spec.Body {
				payload, err := readObjectInput(cli, rawData, spec.BodyRequired && len(fields) == 0)
				if err != nil {
					return usageError(cli, err)
				}
				overrides, err := parsePairs(fields, true)
				if err != nil {
					return usageError(cli, err)
				}
				if payload == nil {
					payload = map[string]any{}
				}
				for key, value := range overrides {
					payload[key] = value
				}
				body = payload
			}

			var result any
			if err := client.DoJSON(context.Background(), spec.Method, path, body, &result); err != nil {
				return apiError(cli, err)
			}
			if result == nil {
				result = map[string]any{"success": true}
			}
			return cli.Printer.PrintJSON(result)
		},
	}
	cmd.Flags().StringVar(&rawData, "data", "", "JSON body inline, @file, or - for stdin")
	cmd.Flags().StringArrayVar(&fields, "field", nil, "Set/override a top-level body field (key=value; repeatable)")
	cmd.Flags().StringArrayVar(&queries, "query-param", nil, "Add a URL query parameter (key=value; repeatable)")
	if !spec.Body {
		_ = cmd.Flags().MarkHidden("data")
		_ = cmd.Flags().MarkHidden("field")
	}
	if spec.Destructive {
		cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	}
	return cmd
}

func usageError(cli *CLI, err error) error {
	cli.Printer.PrintError("invalid_input", err.Error(), "Use --data '{...}', --data @file.json, or --data -", false)
	return exitcodes.New(exitcodes.Usage, err)
}

func readObjectInput(cli *CLI, source string, required bool) (map[string]any, error) {
	var data []byte
	var err error
	switch {
	case source == "-":
		data, err = io.ReadAll(cli.Stdin)
	case strings.HasPrefix(source, "@"):
		if len(source) == 1 {
			return nil, fmt.Errorf("missing file path after @")
		}
		data, err = os.ReadFile(source[1:])
	case source != "":
		data = []byte(source)
	case required:
		if f, ok := cli.Stdin.(*os.File); ok && (isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())) {
			return nil, fmt.Errorf("request body required")
		}
		data, err = io.ReadAll(cli.Stdin)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		if required {
			return nil, fmt.Errorf("request body required")
		}
		return nil, nil
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, fmt.Errorf("invalid JSON object: %w", err)
	}
	if object == nil {
		return nil, fmt.Errorf("request body must be a JSON object")
	}
	return object, nil
}

func parsePairs(pairs []string, decodeJSON bool) (map[string]any, error) {
	result := make(map[string]any, len(pairs))
	for _, pair := range pairs {
		key, raw, ok := strings.Cut(pair, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("expected key=value, got %q", pair)
		}
		var value any = raw
		if decodeJSON {
			var decoded any
			if json.Unmarshal([]byte(raw), &decoded) == nil {
				value = decoded
			}
		}
		result[strings.TrimSpace(key)] = value
	}
	return result, nil
}

func addEndpoints(parent *cobra.Command, cli *CLI, specs ...endpointSpec) {
	for _, spec := range specs {
		parent.AddCommand(newEndpointCmd(cli, spec))
	}
}

func newTimeCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "tempo", Aliases: []string{"time"}, Short: "Manage Copera Tempo calendars, shelf items, and activity blocks"}
	calendars := &cobra.Command{Use: "calendars", Short: "Manage Tempo calendars"}
	addEndpoints(calendars, cli,
		endpointSpec{Use: "list", Short: "List calendars", Method: http.MethodGet, Path: "/time/calendars"},
		endpointSpec{Use: "create", Short: "Create a calendar", Method: http.MethodPost, Path: "/time/calendars", Body: true, BodyRequired: true},
		endpointSpec{Use: "update <calendar-id>", Short: "Update a calendar", Method: http.MethodPatch, Path: "/time/calendars/{0}", Args: 1, Body: true, BodyRequired: true},
		endpointSpec{Use: "archive <calendar-id>", Short: "Archive a calendar", Method: http.MethodPost, Path: "/time/calendars/{0}/archive", Args: 1, Destructive: true},
		endpointSpec{Use: "leave <calendar-id>", Short: "Leave a shared calendar", Method: http.MethodPost, Path: "/time/calendars/{0}/leave", Args: 1, Destructive: true},
	)
	shelf := &cobra.Command{Use: "shelf", Short: "Manage unscheduled Tempo items"}
	addEndpoints(shelf, cli,
		endpointSpec{Use: "list", Short: "List shelf items", Method: http.MethodGet, Path: "/time/shelf-items"},
		endpointSpec{Use: "create", Short: "Create a shelf item", Method: http.MethodPost, Path: "/time/shelf-items", Body: true, BodyRequired: true},
		endpointSpec{Use: "update <item-id>", Short: "Update a shelf item", Method: http.MethodPatch, Path: "/time/shelf-items/{0}", Args: 1, Body: true, BodyRequired: true},
		endpointSpec{Use: "delete <item-id>", Short: "Delete a shelf item", Method: http.MethodDelete, Path: "/time/shelf-items/{0}", Args: 1, Destructive: true},
		endpointSpec{Use: "schedule <item-id>", Short: "Schedule a shelf item", Method: http.MethodPost, Path: "/time/shelf-items/{0}/schedule", Args: 1, Body: true, BodyRequired: true},
	)
	activities := &cobra.Command{Use: "activities", Short: "Inspect Tempo activity blocks"}
	addEndpoints(activities, cli, endpointSpec{Use: "list", Short: "List activity blocks", Method: http.MethodGet, Path: "/time/activity-blocks"})
	cmd.AddCommand(calendars, shelf, activities)
	return cmd
}

func newAgendaCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "agenda", Short: "Manage agenda events"}
	events := &cobra.Command{Use: "events", Short: "Manage events"}
	addEndpoints(events, cli,
		endpointSpec{Use: "list", Short: "List events", Method: http.MethodGet, Path: "/agenda/events"},
		endpointSpec{Use: "get <event-id>", Short: "Get an event", Method: http.MethodGet, Path: "/agenda/events/{0}", Args: 1},
		endpointSpec{Use: "upcoming <channel-id>", Short: "List upcoming channel events", Method: http.MethodGet, Path: "/agenda/channels/{0}/upcoming", Args: 1},
		endpointSpec{Use: "create", Short: "Create an event", Method: http.MethodPost, Path: "/agenda/events", Body: true, BodyRequired: true},
		endpointSpec{Use: "update <event-id>", Short: "Update an event", Method: http.MethodPatch, Path: "/agenda/events/{0}", Args: 1, Body: true, BodyRequired: true},
		endpointSpec{Use: "delete <event-id>", Short: "Delete an event", Method: http.MethodDelete, Path: "/agenda/events/{0}", Args: 1, Destructive: true},
		endpointSpec{Use: "respond <event-id>", Short: "Respond to an event", Method: http.MethodPost, Path: "/agenda/events/{0}/respond", Args: 1, Body: true, BodyRequired: true},
		endpointSpec{Use: "ring <event-id>", Short: "Set ring-at-start preference", Method: http.MethodPost, Path: "/agenda/events/{0}/ring-preference", Args: 1, Body: true, BodyRequired: true},
	)
	cmd.AddCommand(events)
	return cmd
}

func newInboxCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "inbox", Aliases: []string{"inboxes"}, Short: "Read and manage inbox mail"}
	addEndpoints(cmd, cli,
		endpointSpec{Use: "list", Short: "List inboxes", Method: http.MethodGet, Path: "/inboxes"},
		endpointSpec{Use: "get <inbox-id>", Short: "Get an inbox", Method: http.MethodGet, Path: "/inboxes/{0}", Args: 1},
		endpointSpec{Use: "folders <inbox-id>", Short: "List inbox folders", Method: http.MethodGet, Path: "/inboxes/{0}/folders", Args: 1},
		endpointSpec{Use: "send", Short: "Send a message", Method: http.MethodPost, Path: "/inboxes/messages", Body: true, BodyRequired: true},
		endpointSpec{Use: "flag <inbox-id>", Short: "Update email flags", Method: http.MethodPost, Path: "/inboxes/{0}/emails/flags", Args: 1, Body: true, BodyRequired: true},
		endpointSpec{Use: "move <inbox-id>", Short: "Move emails between folders", Method: http.MethodPost, Path: "/inboxes/{0}/emails/move", Args: 1, Body: true, BodyRequired: true},
		endpointSpec{Use: "delete <inbox-id>", Short: "Permanently delete emails", Method: http.MethodDelete, Path: "/inboxes/{0}/emails", Args: 1, Body: true, BodyRequired: true, Destructive: true},
		endpointSpec{Use: "purge-junk <inbox-id>", Short: "Permanently purge junk", Method: http.MethodPost, Path: "/inboxes/{0}/junk/purge", Args: 1, Body: true, Destructive: true},
		endpointSpec{Use: "rsvp <inbox-id>", Short: "Respond to a calendar invitation", Method: http.MethodPost, Path: "/inboxes/{0}/rsvp", Args: 1, Body: true, BodyRequired: true},
	)
	threads := &cobra.Command{Use: "threads", Short: "Read inbox threads"}
	addEndpoints(threads, cli,
		endpointSpec{Use: "list <inbox-id>", Short: "List threads", Method: http.MethodGet, Path: "/inboxes/{0}/threads", Args: 1},
		endpointSpec{Use: "get <inbox-id> <thread-id>", Short: "Get a thread", Method: http.MethodGet, Path: "/inboxes/{0}/threads/{1}", Args: 2},
	)
	drafts := &cobra.Command{Use: "drafts", Short: "Manage drafts"}
	addEndpoints(drafts, cli,
		endpointSpec{Use: "create", Short: "Create a draft", Method: http.MethodPost, Path: "/inboxes/drafts", Body: true, BodyRequired: true},
		endpointSpec{Use: "update <draft-id>", Short: "Update a draft", Method: http.MethodPatch, Path: "/inboxes/drafts/{0}", Args: 1, Body: true, BodyRequired: true},
		endpointSpec{Use: "send <inbox-id> <draft-id>", Short: "Send a draft", Method: http.MethodPost, Path: "/inboxes/{0}/drafts/{1}/send", Args: 2},
	)
	attachments := &cobra.Command{Use: "attachments", Short: "Upload message attachments"}
	attachments.AddCommand(newInboxAttachmentUploadCmd(cli))
	cmd.AddCommand(threads, drafts, attachments)
	return cmd
}

func newInboxAttachmentUploadCmd(cli *CLI) *cobra.Command {
	var file string
	cmd := &cobra.Command{Use: "upload <inbox-id>", Short: "Upload an attachment (maximum 25 MiB)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		info, err := os.Stat(file)
		if err != nil {
			return usageError(cli, fmt.Errorf("inspect attachment: %w", err))
		}
		if info.Size() > 25*1024*1024 {
			return usageError(cli, fmt.Errorf("attachment exceeds 25 MiB"))
		}
		client, _, err := requireAPIClient(cli)
		if err != nil {
			return err
		}
		var result any
		if err := client.UploadFile(context.Background(), "/inboxes/"+url.PathEscape(args[0])+"/attachments", file, &result); err != nil {
			return apiError(cli, err)
		}
		return cli.Printer.PrintJSON(result)
	}}
	cmd.Flags().StringVar(&file, "file", "", "File to upload (required)")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newAutomationsCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "automations", Short: "Inspect and operate automations"}
	addEndpoints(cmd, cli,
		endpointSpec{Use: "list", Short: "List automations", Method: http.MethodGet, Path: "/automations/"},
		endpointSpec{Use: "get <automation-id>", Short: "Get an automation", Method: http.MethodGet, Path: "/automations/{0}", Args: 1},
		endpointSpec{Use: "runs <automation-id>", Short: "List automation runs", Method: http.MethodGet, Path: "/automations/{0}/runs", Args: 1},
		endpointSpec{Use: "run-get <run-id>", Short: "Get an automation run", Method: http.MethodGet, Path: "/automations/runs/{0}", Args: 1},
		endpointSpec{Use: "run <automation-id>", Short: "Run an automation", Method: http.MethodPost, Path: "/automations/{0}/run", Args: 1, Body: true},
		endpointSpec{Use: "retry <run-id>", Short: "Retry a failed run", Method: http.MethodPost, Path: "/automations/runs/{0}/retry", Args: 1},
		endpointSpec{Use: "enable <automation-id>", Short: "Enable an automation", Method: http.MethodPost, Path: "/automations/{0}/enable", Args: 1},
		endpointSpec{Use: "disable <automation-id>", Short: "Disable an automation", Method: http.MethodPost, Path: "/automations/{0}/disable", Args: 1, Destructive: true},
	)
	return cmd
}

func newExportsCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "exports", Short: "Inspect and create asynchronous exports"}
	addEndpoints(cmd, cli,
		endpointSpec{Use: "job <job-id>", Short: "Get an export job", Method: http.MethodGet, Path: "/export/job/{0}", Args: 1},
		endpointSpec{Use: "result-set", Short: "Export a materialized row result set", Method: http.MethodPost, Path: "/export/row-set", Body: true, BodyRequired: true},
	)
	return cmd
}

func bookingLifecycleCommands(cli *CLI) []*cobra.Command {
	return []*cobra.Command{
		newEndpointCmd(cli, endpointSpec{Use: "confirm <booking-id>", Short: "Confirm a booking", Method: http.MethodPost, Path: "/bookings/{0}/confirm", Args: 1, Body: true}),
		newEndpointCmd(cli, endpointSpec{Use: "decline <booking-id>", Short: "Decline a booking", Method: http.MethodPost, Path: "/bookings/{0}/decline", Args: 1, Body: true, Destructive: true}),
		newEndpointCmd(cli, endpointSpec{Use: "cancel <booking-id>", Short: "Cancel a booking", Method: http.MethodPost, Path: "/bookings/{0}/cancel", Args: 1, Body: true, Destructive: true}),
		newEndpointCmd(cli, endpointSpec{Use: "reschedule <booking-id>", Short: "Reschedule a booking", Method: http.MethodPost, Path: "/bookings/{0}/reschedule", Args: 1, Body: true}),
		newEndpointCmd(cli, endpointSpec{Use: "no-show <booking-id>", Short: "Mark a booking as no-show", Method: http.MethodPost, Path: "/bookings/{0}/no-show", Args: 1, Body: true, Destructive: true}),
		newEndpointCmd(cli, endpointSpec{Use: "reassign <booking-id>", Short: "Reassign a booking", Method: http.MethodPost, Path: "/bookings/{0}/reassign", Args: 1, Body: true, BodyRequired: true}),
	}
}

func bookingTypeManagementCommands(cli *CLI) []*cobra.Command {
	return []*cobra.Command{
		newEndpointCmd(cli, endpointSpec{Use: "host", Short: "List booking types managed by the current host", Method: http.MethodGet, Path: "/booking-types/host"}),
		newEndpointCmd(cli, endpointSpec{Use: "get <booking-type-id>", Short: "Get a booking type", Method: http.MethodGet, Path: "/booking-types/{0}", Args: 1}),
		newEndpointCmd(cli, endpointSpec{Use: "create", Short: "Create a booking type", Method: http.MethodPost, Path: "/booking-types/", Body: true, BodyRequired: true}),
		newEndpointCmd(cli, endpointSpec{Use: "update <booking-type-id>", Short: "Update a booking type", Method: http.MethodPatch, Path: "/booking-types/{0}", Args: 1, Body: true, BodyRequired: true}),
		newEndpointCmd(cli, endpointSpec{Use: "delete <booking-type-id>", Short: "Delete a booking type", Method: http.MethodDelete, Path: "/booking-types/{0}", Args: 1, Destructive: true}),
		newEndpointCmd(cli, endpointSpec{Use: "duplicate <booking-type-id>", Short: "Duplicate a booking type", Method: http.MethodPost, Path: "/booking-types/{0}/duplicate", Args: 1}),
		newEndpointCmd(cli, endpointSpec{Use: "reorder", Short: "Reorder booking types", Method: http.MethodPost, Path: "/booking-types/reorder", Body: true, BodyRequired: true}),
		newEndpointCmd(cli, endpointSpec{Use: "set-flag <booking-type-id> <flag>", Short: "Set hidden or active on a booking type", Method: http.MethodPatch, Path: "/booking-types/{0}/flags/{1}", Args: 2, Body: true, BodyRequired: true}),
	}
}

func newBookingHostCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "booking-host", Short: "Manage booking host profiles, schedules, and private links"}
	profile := &cobra.Command{Use: "profile", Short: "Manage the current host profile"}
	addEndpoints(profile, cli,
		endpointSpec{Use: "get", Short: "Get the host profile", Method: http.MethodGet, Path: "/booking-host/profile"},
		endpointSpec{Use: "set-handle", Short: "Set the host booking handle", Method: http.MethodPatch, Path: "/booking-host/profile/handle", Body: true, BodyRequired: true},
	)
	teams := &cobra.Command{Use: "teams", Short: "Manage team booking profiles"}
	addEndpoints(teams, cli,
		endpointSpec{Use: "list", Short: "List team booking profiles", Method: http.MethodGet, Path: "/booking-host/teams"},
		endpointSpec{Use: "set-slug <team-id>", Short: "Set a team booking slug", Method: http.MethodPatch, Path: "/booking-host/teams/{0}/slug", Args: 1, Body: true, BodyRequired: true},
	)
	schedules := &cobra.Command{Use: "schedules", Short: "Manage host availability schedules"}
	addEndpoints(schedules, cli,
		endpointSpec{Use: "list", Short: "List schedules", Method: http.MethodGet, Path: "/booking-host/schedules"},
		endpointSpec{Use: "get <schedule-id>", Short: "Get a schedule", Method: http.MethodGet, Path: "/booking-host/schedules/{0}", Args: 1},
		endpointSpec{Use: "create", Short: "Create a schedule", Method: http.MethodPost, Path: "/booking-host/schedules", Body: true, BodyRequired: true},
		endpointSpec{Use: "update <schedule-id>", Short: "Update a schedule", Method: http.MethodPatch, Path: "/booking-host/schedules/{0}", Args: 1, Body: true, BodyRequired: true},
		endpointSpec{Use: "delete <schedule-id>", Short: "Delete a schedule", Method: http.MethodDelete, Path: "/booking-host/schedules/{0}", Args: 1, Destructive: true},
		endpointSpec{Use: "set-default <schedule-id>", Short: "Set the default schedule", Method: http.MethodPost, Path: "/booking-host/schedules/{0}/default", Args: 1},
	)
	links := &cobra.Command{Use: "links", Short: "Manage private booking links"}
	addEndpoints(links, cli,
		endpointSpec{Use: "list <booking-type-id>", Short: "List private links", Method: http.MethodGet, Path: "/booking-types/{0}/links", Args: 1},
		endpointSpec{Use: "create <booking-type-id>", Short: "Create a private link", Method: http.MethodPost, Path: "/booking-types/{0}/links", Args: 1, Body: true},
		endpointSpec{Use: "revoke <link-id>", Short: "Revoke a private link", Method: http.MethodDelete, Path: "/booking-types/links/{0}", Args: 1, Destructive: true},
	)
	cmd.AddCommand(profile, teams, schedules, links)
	return cmd
}

func rowParityCommands(cli *CLI) []*cobra.Command {
	return []*cobra.Command{
		newEndpointCmd(cli, endpointSpec{Use: "query", Short: "Query rows across tables", Method: http.MethodPost, Path: "/rows/query", Body: true, BodyRequired: true}),
		newEndpointCmd(cli, endpointSpec{Use: "aggregate <board-id> <table-id>", Short: "Aggregate table rows", Method: http.MethodPost, Path: "/board/{0}/table/{1}/rows/aggregate", Args: 2, Body: true, BodyRequired: true}),
		newEndpointCmd(cli, endpointSpec{Use: "bulk <board-id> <table-id>", Short: "Create, update, or delete rows in bulk", Method: http.MethodPost, Path: "/board/{0}/table/{1}/rows/bulk", Args: 2, Body: true, BodyRequired: true}),
		newEndpointCmd(cli, endpointSpec{Use: "views <board-id> <table-id>", Short: "List table views", Method: http.MethodGet, Path: "/board/{0}/table/{1}/views", Args: 2}),
	}
}
