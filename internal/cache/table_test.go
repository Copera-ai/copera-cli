package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableCache_RoundTrip(t *testing.T) {
	tc := NewTableCacheWithStore(NewMemStore(), 5*time.Minute)

	td := &TableData{
		TableID: "table123",
		Name:    "Tasks",
		Columns: map[string]TableColumn{
			"col1": {Label: "Status", Type: "STATUS", Options: map[string]string{
				"opt1": "Todo",
				"opt2": "Done",
			}},
			"col2": {Label: "Title", Type: "TEXT", Options: map[string]string{}},
		},
	}

	tc.Set(td)

	got, ok := tc.Get("table123")
	require.True(t, ok)
	assert.Equal(t, "table123", got.TableID)
	assert.Equal(t, "Tasks", got.Name)
	assert.Equal(t, "Status", got.Columns["col1"].Label)
	assert.Equal(t, "STATUS", got.Columns["col1"].Type)
	assert.Equal(t, "Todo", got.Columns["col1"].Options["opt1"])
	assert.Equal(t, "Done", got.Columns["col1"].Options["opt2"])
	assert.Equal(t, "Title", got.Columns["col2"].Label)
}

func TestTableCache_Expired(t *testing.T) {
	tc := NewTableCacheWithStore(NewMemStore(), 1*time.Millisecond)

	td := &TableData{
		TableID: "t1",
		Name:    "X",
		Columns: map[string]TableColumn{},
	}
	tc.Set(td)

	time.Sleep(5 * time.Millisecond)

	_, ok := tc.Get("t1")
	assert.False(t, ok)
}

func TestTableCache_MissingEntry(t *testing.T) {
	tc := NewTableCacheWithStore(NewMemStore(), 5*time.Minute)

	_, ok := tc.Get("nonexistent")
	assert.False(t, ok)
}

func TestTableData_ResolveColumnLabel(t *testing.T) {
	td := &TableData{
		Columns: map[string]TableColumn{
			"c1": {Label: "Status"},
		},
	}
	assert.Equal(t, "Status", td.ResolveColumnLabel("c1"))
	assert.Equal(t, "unknown_col", td.ResolveColumnLabel("unknown_col"))
}

func TestTableData_ResolveOptionLabel(t *testing.T) {
	td := &TableData{
		Columns: map[string]TableColumn{
			"c1": {Label: "Status", Type: "STATUS", Options: map[string]string{
				"opt1": "Todo",
				"opt2": "In Progress",
			}},
			"c2": {Label: "Tags", Type: "LABELS", Options: map[string]string{
				"l1": "Bug",
				"l2": "Feature",
			}},
			"c3": {Label: "Title", Type: "TEXT", Options: map[string]string{}},
		},
	}

	assert.Equal(t, "Todo", td.ResolveOptionLabel("c1", "opt1"))
	assert.Equal(t, "custom_val", td.ResolveOptionLabel("c1", "custom_val"))
	assert.Equal(t, "Bug, Feature", td.ResolveOptionLabel("c2", []any{"l1", "l2"}))
	assert.Equal(t, "hello", td.ResolveOptionLabel("c3", "hello"))
	assert.Equal(t, "42", td.ResolveOptionLabel("nope", 42))
}

func TestTableCache_FileFormat(t *testing.T) {
	store := NewMemStore()
	tc := NewTableCacheWithStore(store, 5*time.Minute)

	td := &TableData{
		TableID: "abc",
		Name:    "My Table",
		Columns: map[string]TableColumn{
			"c1": {Label: "Status", Type: "STATUS", Options: map[string]string{
				"o1": "Open",
			}},
		},
	}
	tc.Set(td)

	raw, err := store.Read("table-abc.cache")
	require.NoError(t, err)
	content := string(raw)
	assert.Contains(t, content, "table-data:abc; version: 1;")
	assert.Contains(t, content, "name:My Table;")
	assert.Contains(t, content, "col:c1; label:Status; type:STATUS;")
	assert.Contains(t, content, "opt:o1=Open;")
	assert.Contains(t, content, "timestamp: ")
}

func TestDocCache_WithMemStore(t *testing.T) {
	c := NewWithStore(NewMemStore(), 5*time.Minute)

	_, ok := c.Get("missing")
	assert.False(t, ok)

	c.Set("doc1", "hello world")
	val, ok := c.Get("doc1")
	assert.True(t, ok)
	assert.Equal(t, "hello world", val)

	c.Delete("doc1")
	_, ok = c.Get("doc1")
	assert.False(t, ok)
}
