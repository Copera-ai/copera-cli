package commands

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/copera/copera-cli/internal/output"
	"github.com/spf13/cobra"
)

const mediaMaxAudioBytes = 20 << 20

var mediaKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func newMediaCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "media", Short: "Create paid media or retrieve your media results"}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		// Flag parsing runs before the root initializes the normal printer.
		return exitcodes.New(exitcodes.Usage, err)
	})
	cmd.AddCommand(newMediaImageCmd(cli), newMediaSpeechCmd(cli), newMediaTranscriptionCmd(cli),
		newMediaStatusCmd(cli), newMediaVoicesCmd(cli), newMediaUsageCmd(cli))
	return cmd
}

type mediaSubmission struct {
	key    string
	design string
	out    string
}

func (opts *mediaSubmission) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&opts.key, "idempotency-key", "", "Required retry key. Reuse it only for the same request")
	cmd.Flags().StringVar(&opts.design, "design", "", "Target Design ID")
	cmd.Flags().StringVarP(&opts.out, "out", "o", "", "Path for the result")
}

type submitMedia func(context.Context, *api.Client) (*api.MediaReceipt, error)

func (opts *mediaSubmission) run(cmd *cobra.Command, cli *CLI, count int, submit submitMedia) error {
	if !mediaKeyPattern.MatchString(opts.key) {
		return publicInputError(cli, "--idempotency-key must contain 1–128 ASCII letters, digits, dots, underscores, colons, or dashes")
	}
	if opts.design != "" {
		id, err := parsePublicObjectID(opts.design, "design")
		if err != nil {
			return publicInputError(cli, err.Error())
		}
		opts.design = id
	}
	if err := checkMediaOutput(opts.out, count); err != nil {
		return publicInputError(cli, err.Error())
	}

	client, _, err := requireAPIClient(cli)
	if err != nil {
		return err
	}
	signalCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signalCtx, 25*time.Minute)
	defer cancel()

	receipt, err := submit(ctx, client)
	if err != nil {
		if ctx.Err() != nil {
			cli.Printer.PrintError("media_poll_interrupted", "Retry the same submission with the same idempotency key to recover its receipt.", "", true)
			return exitcodes.New(exitcodes.Error, ctx.Err())
		}
		return printPublicAPIError(cli, err)
	}
	id, err := parsePublicObjectID(receipt.OperationID, "operation")
	if err != nil {
		return printPublicAPIError(cli, err)
	}
	for receipt.Active() {
		select {
		case <-ctx.Done():
			return mediaResumeError(cli, id, ctx.Err())
		case <-time.After(2 * time.Second):
		}
		receipt, err = client.PollMediaStatus(ctx, id)
		if err != nil {
			return mediaResumeError(cli, id, err)
		}
	}
	if ctx.Err() != nil {
		return mediaResumeError(cli, id, ctx.Err())
	}
	return printMediaReceipt(ctx, cli, client, receipt, opts.out)
}

func mediaResumeError(cli *CLI, id string, err error) error {
	suggestion := "Resume with copera media status " + id
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		cli.Printer.PrintError(apiErr.Code, apiErr.Message, suggestion, apiErr.StatusCode == 429 || apiErr.StatusCode == 503)
		return exitcodes.New(apiErr.ExitCode(), err)
	}
	cli.Printer.PrintError("media_poll_interrupted", "The media operation was accepted.", suggestion, true)
	return exitcodes.New(exitcodes.Error, err)
}

func newMediaImageCmd(cli *CLI) *cobra.Command {
	var opts mediaSubmission
	var input api.MediaImageInput
	var size string
	var references []string
	cmd := &cobra.Command{
		Use:   "image <prompt>",
		Short: "Generate or edit images using workspace AI credits",
		Args:  publicCommandArgs(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			input.Prompt, err = mediaText(args[0], "prompt", 1000)
			if err != nil {
				return publicInputError(cli, err.Error())
			}
			if input.Count < 1 || input.Count > 4 || len(references) > 4 {
				return publicInputError(cli, "--count must be 1–4 and at most four references are allowed")
			}
			if size != "" && input.AspectRatio != "" {
				return publicInputError(cli, "--size and --aspect-ratio cannot be used together")
			}
			if input.AspectRatio != "" && !slices.Contains([]string{"1:1", "3:2", "2:3", "16:9", "9:16"}, input.AspectRatio) {
				return publicInputError(cli, "unsupported --aspect-ratio")
			}
			switch size {
			case "1024x1024":
				input.Size = &api.MediaSize{Width: 1024, Height: 1024}
			case "1536x1024":
				input.Size = &api.MediaSize{Width: 1536, Height: 1024}
			case "1024x1536":
				input.Size = &api.MediaSize{Width: 1024, Height: 1536}
			case "":
			default:
				return publicInputError(cli, "--size must be 1024x1024, 1536x1024, or 1024x1536")
			}
			if input.Quality != "" && input.Quality != "standard" && input.Quality != "high" {
				return publicInputError(cli, "--quality must be standard or high")
			}
			for _, reference := range references {
				source, err := parseMediaReference(reference)
				if err != nil {
					return publicInputError(cli, err.Error())
				}
				input.References = append(input.References, source)
			}
			return opts.run(cmd, cli, input.Count, func(ctx context.Context, client *api.Client) (*api.MediaReceipt, error) {
				input.Design = opts.design
				if !cmd.Flags().Changed("count") {
					input.Count = 0
				}
				return client.MediaImage(ctx, opts.key, input)
			})
		},
	}
	opts.addFlags(cmd)
	cmd.Flags().StringVar(&input.AspectRatio, "aspect-ratio", "", "Image aspect ratio")
	cmd.Flags().StringVar(&size, "size", "", "Exact width x height, such as 1536x1024")
	cmd.Flags().IntVar(&input.Count, "count", 1, "Number of images")
	cmd.Flags().StringArrayVar(&references, "reference", nil, "Ordered reference: drive:<id> or asset:<design-id>:<asset-id>")
	cmd.Flags().BoolVar(&input.Transparent, "transparent", false, "Require transparent PNG output")
	cmd.Flags().StringVar(&input.Quality, "quality", "", "Image quality: standard or high")
	return cmd
}

func newMediaSpeechCmd(cli *CLI) *cobra.Command {
	var opts mediaSubmission
	var input api.MediaSpeechInput
	cmd := &cobra.Command{
		Use:   "speech <text>",
		Short: "Synthesize narration using workspace AI credits",
		Args:  publicCommandArgs(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			input.Text, err = mediaText(args[0], "text", 5000)
			if err != nil {
				return publicInputError(cli, err.Error())
			}
			if input.Language == "" {
				return publicInputError(cli, "--language is required. Use copera media voices to see supported languages.")
			}
			if input.Format != "" && !slices.Contains([]string{"mp3", "wav", "opus"}, input.Format) {
				return publicInputError(cli, "--format must be mp3, wav, or opus")
			}
			return opts.run(cmd, cli, 1, func(ctx context.Context, client *api.Client) (*api.MediaReceipt, error) {
				input.Design = opts.design
				return client.MediaSpeech(ctx, opts.key, input)
			})
		},
	}
	opts.addFlags(cmd)
	cmd.Flags().StringVar(&input.Language, "language", "", "Narration language tag")
	cmd.Flags().StringVar(&input.Voice, "voice", "", "Voice ID. Defaults to the language's default voice.")
	cmd.Flags().StringVar(&input.Format, "format", "", "Output format: mp3, wav, or opus")
	return cmd
}

func newMediaTranscriptionCmd(cli *CLI) *cobra.Command {
	var opts mediaSubmission
	var drive, asset string
	cmd := &cobra.Command{
		Use:   "transcribe [local-file]",
		Short: "Transcribe audio and generate captions using workspace AI credits",
		Args:  publicCommandArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("drive-file") && cmd.Flags().Changed("asset") ||
				len(args) == 1 && (cmd.Flags().Changed("drive-file") || cmd.Flags().Changed("asset")) ||
				len(args) == 0 && !cmd.Flags().Changed("drive-file") && !cmd.Flags().Changed("asset") {
				return publicInputError(cli, "choose exactly one local file, --drive-file, or --asset")
			}
			if len(args) == 1 {
				mediaType := designAssetMediaType(filepath.Base(args[0]))
				if !slices.Contains([]string{"audio/mpeg", "audio/wav", "audio/ogg", "audio/mp4", "video/mp4", "video/webm"}, mediaType) {
					return publicInputError(cli, "local media must be MP3, WAV, Ogg Opus, M4A, MP4, or WebM")
				}
				file, err := readDesignUpload(args[0], mediaMaxAudioBytes)
				if err != nil {
					return publicInputError(cli, err.Error())
				}
				return opts.run(cmd, cli, 0, func(ctx context.Context, client *api.Client) (*api.MediaReceipt, error) {
					return client.MediaTranscribeFile(ctx, opts.key, opts.design, filepath.Base(args[0]), mediaType, file)
				})
			}
			reference := "drive:" + drive
			if cmd.Flags().Changed("asset") {
				reference = "asset:" + asset
			}
			source, err := parseMediaReference(reference)
			if err != nil {
				return publicInputError(cli, err.Error())
			}
			return opts.run(cmd, cli, 0, func(ctx context.Context, client *api.Client) (*api.MediaReceipt, error) {
				return client.MediaTranscribe(ctx, opts.key, api.MediaTranscriptionInput{Design: opts.design, Source: source})
			})
		},
	}
	opts.addFlags(cmd)
	cmd.Flags().StringVar(&drive, "drive-file", "", "Drive file ID")
	cmd.Flags().StringVar(&asset, "asset", "", "Design asset as <design-id>:<asset-id>")
	return cmd
}

func newMediaStatusCmd(cli *CLI) *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "status <operation-id>",
		Short: "Read a media operation and optionally download completed output",
		Args:  publicCommandArgs(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parsePublicObjectID(args[0], "operation")
			if err != nil {
				return publicInputError(cli, err.Error())
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			receipt, err := client.MediaStatus(cmd.Context(), id)
			if err != nil {
				return printPublicAPIError(cli, err)
			}
			if receipt.Status == "succeeded" {
				if err := checkMediaOutput(out, receipt.RequestedCount); err != nil {
					return publicInputError(cli, err.Error())
				}
			}
			return printMediaReceipt(cmd.Context(), cli, client, receipt, out)
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", "Path for the result")
	return cmd
}

func newMediaVoicesCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use: "voices", Short: "List available narration voices", Args: publicCommandArgs(0, 0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			response, err := client.MediaVoices(cmd.Context())
			if err != nil {
				return printPublicAPIError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(response)
			}
			for _, voice := range response.Voices {
				cli.Printer.PrintLine(fmt.Sprintf("%s (%s): %s", voice.Name, voice.ID, strings.Join(voice.Languages, ", ")))
			}
			return nil
		},
	}
}

func newMediaUsageCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use: "usage", Short: "Read this credential's media spend and limits", Args: publicCommandArgs(0, 0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			response, err := client.MediaUsage(cmd.Context())
			if err != nil {
				return printPublicAPIError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(response)
			}
			cli.Printer.PrintLine(fmt.Sprintf("Daily: %d¢ spent, %d¢ reserved, %d¢ limit (resets %s)", response.Daily.SpentCents,
				response.Daily.ReservedCents, response.Daily.LimitCents, response.Daily.ResetsAt))
			cli.Printer.PrintLine(fmt.Sprintf("Lifetime: %d¢ spent, %d¢ reserved, %d¢ limit", response.Total.SpentCents,
				response.Total.ReservedCents, response.Total.LimitCents))
			return nil
		},
	}
}

func mediaText(value, label string, limit int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(utf16.Encode([]rune(value))) > limit {
		return "", fmt.Errorf("%s must contain 1–%d UTF-16 code units", label, limit)
	}
	return value, nil
}

func parseMediaReference(value string) (api.MediaInput, error) {
	parts := strings.Split(value, ":")
	switch {
	case len(parts) == 2 && parts[0] == "drive":
		id, err := parsePublicObjectID(parts[1], "Drive file")
		return api.MediaInput{Kind: "drive_file", FileID: id}, err
	case len(parts) == 3 && parts[0] == "asset":
		design, err := parsePublicObjectID(parts[1], "reference Design")
		if err != nil {
			return api.MediaInput{}, err
		}
		asset, err := parsePublicObjectID(parts[2], "asset")
		return api.MediaInput{Kind: "design_asset", DesignID: design, AssetID: asset}, err
	default:
		return api.MediaInput{}, errors.New("reference must be drive:<file-id> or asset:<design-id>:<asset-id>")
	}
}

func checkMediaOutput(out string, count int) error {
	if out == "" {
		return nil
	}
	if count <= 1 {
		if _, err := os.Lstat(out); err == nil {
			return fmt.Errorf("output already exists: %q", out)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	dir := out
	if count <= 1 {
		dir = filepath.Dir(out)
	}
	info, err := os.Stat(dir)
	if count <= 1 && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() {
		return fmt.Errorf("output directory must exist: %q", dir)
	}
	return nil
}

func printMediaReceipt(ctx context.Context, cli *CLI, client *api.Client, receipt *api.MediaReceipt, out string) error {
	var fileErr error
	if out != "" && !receipt.Active() {
		fileErr = writeMediaOutput(ctx, client, receipt, out)
	}
	if cli.Printer.IsJSON() {
		if err := cli.Printer.PrintJSON(receipt); err != nil {
			return err
		}
	} else if receipt.Transcript != nil && out == "" && !cli.Printer.IsTable() {
		cli.Printer.PrintLine(receipt.Transcript.Text)
	} else {
		cli.Printer.PrintLine(fmt.Sprintf("%s %s: %d¢ charged, %d¢ assessed, %d¢ reserved", receipt.OperationID, receipt.Status,
			receipt.Spend.ChargedCents, receipt.Spend.AssessedCents, receipt.Spend.ReservedCents))
		for _, file := range receipt.Files {
			if file.Asset != nil {
				cli.Printer.PrintLine("Design asset: " + file.Asset.Path)
			}
		}
	}
	if receipt.Status == "failed" && receipt.Error == nil {
		return printPublicAPIError(cli, errors.New("media operation failed"))
	}
	if receipt.Error != nil {
		message := receipt.Error.Message
		if fileErr != nil {
			message += " " + fileErr.Error()
		}
		cli.Printer.PrintError(receipt.Error.Code, message, "", false)
		return exitcodes.Newf(exitcodes.Error, "%s", message)
	}
	if fileErr != nil {
		return printPublicAPIError(cli, fileErr)
	}
	return nil
}

func writeMediaOutput(ctx context.Context, client *api.Client, receipt *api.MediaReceipt, out string) error {
	if err := checkMediaOutput(out, receipt.RequestedCount); err != nil {
		return err
	}
	if receipt.Transcript != nil {
		if len(receipt.Transcript.VTT) > 1<<20 {
			return errors.New("WebVTT exceeds the 1 MiB output limit")
		}
		return writeNewMediaFile(out, func(dst io.Writer) error {
			_, err := io.WriteString(dst, receipt.Transcript.VTT)
			return err
		})
	}
	if len(receipt.Files) == 0 && receipt.Status == "succeeded" {
		return errors.New("the operation has no available output")
	}
	dir := filepath.Dir(out)
	if receipt.RequestedCount > 1 {
		dir = out
	}
	project, state, relativeDir := mediaOutputDesign(dir, receipt.Design)
	if project != nil {
		defer project.Close()
	}
	for _, file := range receipt.Files {
		if err := writeMediaResultFile(ctx, client, receipt, out, file, project, state, relativeDir); err != nil {
			return err
		}
	}
	return nil
}

func writeMediaResultFile(ctx context.Context, client *api.Client, receipt *api.MediaReceipt, out string,
	file api.MediaFile, project *os.Root, state *designState, relativeDir string) error {
	destination := out
	if receipt.RequestedCount > 1 {
		name := file.FileName
		if file.Asset != nil && project != nil && relativeDir == filepath.ToSlash(filepath.Dir(file.Asset.Path)) {
			if err := validateDesignContentPath(file.Asset.Path); err != nil {
				return err
			}
			name = filepath.Base(file.Asset.Path)
		}
		if filepath.Base(name) != name || name == "" || name == "." || strings.ContainsAny(name, `/\`) {
			return errors.New("invalid media download name")
		}
		if err := validateDesignContentPath(name); err != nil {
			return err
		}
		destination = filepath.Join(out, name)
	}
	if file.Download == nil {
		return errors.New("the result link is unavailable. Try copera media status again.")
	}
	limit := int64(mediaMaxAudioBytes)
	if receipt.Kind == "image" {
		limit = 10 << 20
	}
	if file.ByteLength <= 0 || file.ByteLength > limit {
		return errors.New("media result exceeds its output limit or has no declared length")
	}
	err := writeNewMediaFile(destination, func(dst io.Writer) error {
		_, err := downloadDesignBytes(ctx, client, file.Download.URL, dst, file.ByteLength, limit, file.SHA256)
		if err != nil {
			// Some HTTP errors include the signed URL. Keep it out of diagnostics.
			return fmt.Errorf("could not download media or verify its length and SHA-256")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if project == nil || file.Asset == nil {
		return nil
	}
	projectPath := filepath.ToSlash(filepath.Join(relativeDir, filepath.Base(destination)))
	asset := file.Asset
	if projectPath != asset.Path || asset.ContentDigest != file.SHA256 || asset.ByteLength != file.ByteLength ||
		asset.MediaType != file.MediaType || validateDesignContentPath(projectPath) != nil {
		return nil
	}
	usage := "reference"
	if strings.HasPrefix(file.MediaType, "image/") {
		usage = "image"
	}
	return rememberDesignAsset(project, state, projectPath, api.ArtifactAsset{
		ID: asset.ID, Path: asset.Path, MediaType: asset.MediaType, ContentDigest: asset.ContentDigest, Usage: usage,
	})
}

func mediaOutputDesign(dir string, design *string) (*os.Root, *designState, string) {
	if design == nil {
		return nil, nil, ""
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, ""
	}
	for current := absolute; ; current = filepath.Dir(current) {
		if _, err := os.Lstat(filepath.Join(current, designStateFile)); err == nil {
			root, err := openDesignDir(current, false)
			if err != nil {
				break
			}
			state, err := loadDesignState(root)
			relative, relErr := filepath.Rel(current, absolute)
			if err == nil && relErr == nil && state.ID == *design && checkDesignSymlinks(root, relative) == nil {
				return root, state, filepath.ToSlash(relative)
			}
			_ = root.Close()
			break
		}
		if current == filepath.Dir(current) {
			break
		}
	}
	return nil, nil, ""
}

func writeNewMediaFile(name string, write func(io.Writer) error) error {
	dir := filepath.Dir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".copera-media-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temp.Name()) }()
	if err := errors.Join(write(temp), temp.Close()); err != nil {
		return err
	}
	// A same-directory hard link publishes the completed file atomically
	// and fails if another writer has created the destination.
	return os.Link(temp.Name(), name)
}

func parsePublicObjectID(value, label string) (string, error) {
	if len(value) == 24 {
		if _, err := hex.DecodeString(value); err == nil {
			return strings.ToLower(value), nil
		}
	}
	return "", fmt.Errorf("%s ID must be 24 hexadecimal characters", label)
}

func publicCommandArgs(minimum, maximum int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < minimum || len(args) > maximum {
			message := "usage: " + cmd.CommandPath() + strings.TrimPrefix(cmd.Use, cmd.Name())
			// Cobra validates arguments before the root initializes cli.Printer.
			printer := output.New(output.FormatJSON, cmd.OutOrStdout(), cmd.ErrOrStderr(), false)
			printer.PrintError("invalid_request", message, "", false)
			return exitcodes.Newf(exitcodes.Usage, "%s", message)
		}
		return nil
	}
}

func publicInputError(cli *CLI, message string) error {
	cli.Printer.PrintError("invalid_request", message, "", false)
	return exitcodes.Newf(exitcodes.Usage, "%s", message)
}

func printPublicAPIError(cli *CLI, err error) error {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return apiError(cli, err)
	}
	code := apiErr.Code
	if code == "" {
		code = "api_error"
	}
	exit := apiErr.ExitCode()
	switch apiErr.StatusCode {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		exit = exitcodes.Usage
	}
	cli.Printer.PrintError(code, apiErr.Message, "",
		apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode == http.StatusServiceUnavailable)
	return exitcodes.New(exit, err)
}
