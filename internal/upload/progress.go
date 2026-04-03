// Package upload provides multipart file upload orchestration with progress tracking.
package upload

import (
	"io"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/schollz/progressbar/v3"
)

// Progress tracks upload progress for a single file.
type Progress interface {
	Init(fileName string, totalBytes int64)
	Add(bytes int64)
	Finish()
}

// BarProgress renders a curl/wget-style progress bar on a TTY.
type BarProgress struct {
	out io.Writer
	bar *progressbar.ProgressBar
}

// NewBarProgress creates a progress bar that writes to out (typically os.Stderr).
func NewBarProgress(out io.Writer) *BarProgress {
	return &BarProgress{out: out}
}

func (p *BarProgress) Init(fileName string, totalBytes int64) {
	p.bar = progressbar.NewOptions64(
		totalBytes,
		progressbar.OptionSetDescription(fileName),
		progressbar.OptionSetWriter(p.out),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(40),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionOnCompletion(func() {
			// newline after bar completes
			_, _ = io.WriteString(p.out, "\n")
		}),
	)
}

func (p *BarProgress) Add(bytes int64) {
	if p.bar != nil {
		_ = p.bar.Add64(bytes)
	}
}

func (p *BarProgress) Finish() {
	if p.bar != nil {
		_ = p.bar.Finish()
	}
}

// NoopProgress discards all progress updates (used in JSON/quiet/CI mode).
type NoopProgress struct{}

func (NoopProgress) Init(string, int64) {}
func (NoopProgress) Add(int64)          {}
func (NoopProgress) Finish()            {}

// ShouldShowProgress reports whether a progress bar should be displayed.
// Returns true when out is a TTY file descriptor.
func ShouldShowProgress(out io.Writer) bool {
	f, ok := out.(*os.File)
	return ok && isatty.IsTerminal(f.Fd())
}
