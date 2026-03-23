// Package output handles all stdout/stderr rendering for CLI commands.
// Commands never write to os.Stdout or os.Stderr directly — they always
// go through a Printer so that tests can capture output cleanly.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/mattn/go-isatty"
)

// Format controls how command output is rendered.
type Format string

const (
	FormatAuto  Format = "auto"
	FormatJSON  Format = "json"
	FormatTable Format = "table"
	FormatPlain Format = "plain"
)

// ParseFormat parses a user-supplied format string.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatAuto, FormatJSON, FormatTable, FormatPlain:
		return Format(s), nil
	default:
		return FormatAuto, fmt.Errorf("unknown format %q: must be auto, json, table, or plain", s)
	}
}

// Printer writes structured output to stdout and errors to stderr.
// Construct one per command invocation via New().
type Printer struct {
	format Format
	Out    io.Writer
	Err    io.Writer
	Quiet  bool
}

// New creates a Printer. When format is FormatAuto, it resolves to FormatTable
// if Out is a TTY, otherwise FormatJSON (agent-safe default).
func New(format Format, out, errOut io.Writer, quiet bool) *Printer {
	return &Printer{
		format: format,
		Out:    out,
		Err:    errOut,
		Quiet:  quiet,
	}
}

// Default returns a Printer wired to os.Stdout / os.Stderr with auto format.
func Default() *Printer {
	return New(FormatAuto, os.Stdout, os.Stderr, false)
}

// resolvedFormat resolves FormatAuto by checking whether Out is a TTY.
func (p *Printer) resolvedFormat() Format {
	if p.format != FormatAuto {
		return p.format
	}
	if f, ok := p.Out.(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		return FormatTable
	}
	return FormatJSON
}

// IsJSON reports whether the effective output format is JSON.
// Subcommands use this to skip table setup.
func (p *Printer) IsJSON() bool {
	return p.resolvedFormat() == FormatJSON
}

// IsTable reports whether the effective output format is a human table.
func (p *Printer) IsTable() bool {
	return p.resolvedFormat() == FormatTable
}

// PrintJSON encodes v as indented JSON and writes it to Out.
func (p *Printer) PrintJSON(v any) error {
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// PrintLine writes a plain line to Out.
func (p *Printer) PrintLine(msg string) {
	fmt.Fprintln(p.Out, msg)
}

// Info writes an informational message to Err. Suppressed by --quiet.
func (p *Printer) Info(format string, args ...any) {
	if !p.Quiet {
		fmt.Fprintf(p.Err, format+"\n", args...)
	}
}

// StructuredError is the JSON shape written to stderr on errors.
type StructuredError struct {
	Error      string `json:"error"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
	Transient  bool   `json:"transient"`
}

// PrintError writes a structured JSON error to Err (stderr).
// It is always written regardless of --quiet.
func (p *Printer) PrintError(errType, message, suggestion string, transient bool) {
	e := StructuredError{
		Error:      errType,
		Message:    message,
		Suggestion: suggestion,
		Transient:  transient,
	}
	enc := json.NewEncoder(p.Err)
	_ = enc.Encode(e)
}
