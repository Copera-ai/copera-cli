package output

import (
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// Table wraps go-pretty's table.Writer with a simpler API suited to CLI output.
type Table struct {
	tw table.Writer
}

// NewTable creates a Table that writes to p.Out using the CLI house style.
func NewTable(p *Printer) *Table {
	tw := table.NewWriter()
	tw.SetOutputMirror(p.Out)
	tw.SetStyle(table.StyleLight)
	tw.Style().Color.Header = text.Colors{text.Bold}
	tw.Style().Options.DrawBorder = false
	tw.Style().Options.SeparateHeader = true
	tw.Style().Options.SeparateRows = false
	return &Table{tw: tw}
}

// Header sets the column headers.
func (t *Table) Header(cols ...string) {
	row := make(table.Row, len(cols))
	for i, c := range cols {
		row[i] = c
	}
	t.tw.AppendHeader(row)
}

// Row appends a data row.
func (t *Table) Row(cols ...any) {
	row := make(table.Row, len(cols))
	copy(row, cols)
	t.tw.AppendRow(row)
}

// Render writes the table to the underlying writer.
func (t *Table) Render() {
	t.tw.Render()
}
