package cache

import (
	"fmt"
	"strings"
	"time"
)

// TableColumn is a cached column definition with pre-indexed option labels.
type TableColumn struct {
	Label   string
	Type    string
	Options map[string]string // optionID → label
}

// TableData holds cached table schema for fast column/option lookup.
type TableData struct {
	TableID string
	Name    string
	Columns map[string]TableColumn // columnID → TableColumn
}

// ResolveColumnLabel returns the column label or the raw columnID if not found.
func (td *TableData) ResolveColumnLabel(columnID string) string {
	if col, ok := td.Columns[columnID]; ok {
		return col.Label
	}
	return columnID
}

// ResolveOptionLabel returns the option label for a value if it matches an option ID.
// For non-option columns or unknown values, returns the original value as a string.
func (td *TableData) ResolveOptionLabel(columnID string, value any) string {
	col, ok := td.Columns[columnID]
	if !ok {
		return fmt.Sprintf("%v", value)
	}
	if len(col.Options) == 0 {
		return fmt.Sprintf("%v", value)
	}
	// value might be a string option ID or an array of option IDs (LABELS type)
	switch v := value.(type) {
	case string:
		if label, ok := col.Options[v]; ok {
			return label
		}
		return v
	case []any:
		labels := make([]string, 0, len(v))
		for _, item := range v {
			s := fmt.Sprintf("%v", item)
			if label, ok := col.Options[s]; ok {
				labels = append(labels, label)
			} else {
				labels = append(labels, s)
			}
		}
		return strings.Join(labels, ", ")
	default:
		return fmt.Sprintf("%v", value)
	}
}

const tableDataVersion = "1"

// TableCache stores table schema via a Store backend in a custom text format.
type TableCache struct {
	store Store
	ttl   time.Duration
}

// NewTableCache creates a TableCache backed by disk. dir is the base cache directory;
// defaults to os.TempDir()/copera-cli-{version}/tables. ttl defaults to 15m.
func NewTableCache(dir string, ttl time.Duration) *TableCache {
	if ttl == 0 {
		ttl = 15 * time.Minute
	}
	return &TableCache{store: NewDiskStore(TablesDir(dir)), ttl: ttl}
}

// NewTableCacheWithStore creates a TableCache with a custom Store (e.g. MemStore for tests).
func NewTableCacheWithStore(store Store, ttl time.Duration) *TableCache {
	if ttl == 0 {
		ttl = 15 * time.Minute
	}
	return &TableCache{store: store, ttl: ttl}
}

// Get reads and parses the cached table data. Returns nil, false if missing or expired.
func (tc *TableCache) Get(tableID string) (*TableData, bool) {
	data, err := tc.store.Read("table-" + tableID + ".cache")
	if err != nil {
		return nil, false
	}
	td, ts, err := parseTableFile(string(data), tableID)
	if err != nil {
		return nil, false
	}
	if time.Now().After(ts.Add(tc.ttl)) {
		return nil, false
	}
	return td, true
}

// Set serializes and stores table data.
func (tc *TableCache) Set(td *TableData) {
	content := serializeTableFile(td)
	_ = tc.store.Write("table-"+td.TableID+".cache", []byte(content))
}

// File format:
//   table-data:{tableID}; version: 1;
//   name:{tableName};
//   col:{columnID}; label:{label}; type:{type};
//   col:{columnID}; label:{label}; type:{type}; opt:{optID}={optLabel}; opt:{optID}={optLabel};
//   timestamp: {unix};

func serializeTableFile(td *TableData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "table-data:%s; version: %s;\n", td.TableID, tableDataVersion)
	fmt.Fprintf(&b, "name:%s;\n", td.Name)
	for colID, col := range td.Columns {
		fmt.Fprintf(&b, "col:%s; label:%s; type:%s;", colID, col.Label, col.Type)
		for optID, optLabel := range col.Options {
			fmt.Fprintf(&b, " opt:%s=%s;", optID, optLabel)
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "timestamp: %d;", time.Now().Unix())
	return b.String()
}

func parseTableFile(content, expectedTableID string) (*TableData, time.Time, error) {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) < 3 {
		return nil, time.Time{}, fmt.Errorf("invalid table cache file")
	}

	header := lines[0]
	tableID, err := extractField(header, "table-data:")
	if err != nil {
		return nil, time.Time{}, err
	}
	if tableID != expectedTableID {
		return nil, time.Time{}, fmt.Errorf("table ID mismatch: got %s, want %s", tableID, expectedTableID)
	}
	version, err := extractField(header, "version: ")
	if err != nil || version != tableDataVersion {
		return nil, time.Time{}, fmt.Errorf("unsupported version")
	}

	td := &TableData{
		TableID: tableID,
		Columns: make(map[string]TableColumn),
	}

	var timestamp time.Time

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			name, err := extractField(line, "name:")
			if err == nil {
				td.Name = name
			}
		} else if strings.HasPrefix(line, "col:") {
			col := parseColumnLine(line)
			if col.id != "" {
				td.Columns[col.id] = col.tc
			}
		} else if strings.HasPrefix(line, "timestamp: ") {
			tsStr, err := extractField(line, "timestamp: ")
			if err == nil {
				var unix int64
				fmt.Sscanf(tsStr, "%d", &unix)
				timestamp = time.Unix(unix, 0)
			}
		}
	}

	if timestamp.IsZero() {
		return nil, time.Time{}, fmt.Errorf("missing timestamp")
	}

	return td, timestamp, nil
}

type parsedColumn struct {
	id string
	tc TableColumn
}

func parseColumnLine(line string) parsedColumn {
	parts := strings.Split(line, "; ")
	var pc parsedColumn
	pc.tc.Options = make(map[string]string)

	for _, part := range parts {
		part = strings.TrimSuffix(part, ";")
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "col:") {
			pc.id = strings.TrimPrefix(part, "col:")
		} else if strings.HasPrefix(part, "label:") {
			pc.tc.Label = strings.TrimPrefix(part, "label:")
		} else if strings.HasPrefix(part, "type:") {
			pc.tc.Type = strings.TrimPrefix(part, "type:")
		} else if strings.HasPrefix(part, "opt:") {
			optPart := strings.TrimPrefix(part, "opt:")
			if eqIdx := strings.Index(optPart, "="); eqIdx > 0 {
				pc.tc.Options[optPart[:eqIdx]] = optPart[eqIdx+1:]
			}
		}
	}
	return pc
}

func extractField(line, prefix string) (string, error) {
	idx := strings.Index(line, prefix)
	if idx < 0 {
		return "", fmt.Errorf("field %q not found", prefix)
	}
	rest := line[idx+len(prefix):]
	end := strings.Index(rest, ";")
	if end < 0 {
		return "", fmt.Errorf("field %q not terminated", prefix)
	}
	return rest[:end], nil
}
