# ADR-005: File-Based Caching with Store Abstraction

**Date:** 2026-03-23
**Status:** Accepted

---

## Context

The CLI makes repeated API calls that return data that changes infrequently:

1. **Doc content** — markdown bodies fetched via `docs content <id>`. Users may view the same doc multiple times in a session. The API returns the full body each time.
2. **Table schemas** — column definitions (labels, types, options) fetched to resolve raw column/option IDs into human-readable labels in `rows get`. Schema changes are rare; row reads are frequent.

Without caching, every `rows get` triggers an extra `tables get` call just to display column names, and every `docs content` call re-downloads the full markdown body.

---

## Decision

### Cache directory layout

All cache files live under a single versioned directory:

```
$TMPDIR/copera-cli-{version}/
├── docs/           # doc content cache (JSON entries with TTL)
│   └── {docId}.json
├── tables/         # table schema cache (custom text format)
│   └── table-{tableId}.cache
└── version-check.json   # auto-update version check
```

The directory is versioned (`copera-cli-0.1.0`) so that binary upgrades get a fresh cache without manual cleanup. Old version directories can be removed by users if needed.

### Two cache types

**Doc content cache** — stores markdown content as JSON entries with an expiry timestamp:

```json
{"value":"# Hello\n...","expires_at":"2026-03-23T15:00:00Z"}
```

- Default TTL: 1 hour
- Invalidated on `docs update` and `docs delete`
- Bypassed with `--no-cache` flag

**Table schema cache** — stores column definitions in a custom text format optimized for easy ID-based lookup:

```
table-data:{tableID}; version: 1;
name:{tableName};
col:{columnID}; label:{label}; type:{type}; opt:{optID}={optLabel}; opt:{optID}={optLabel};
col:{columnID}; label:{label}; type:{type};
timestamp: {unix};
```

- Default TTL: 15 minutes
- Data is pre-indexed into `map[string]TableColumn` on read for O(1) lookup by column ID
- Each `TableColumn` holds an `Options` map (`optionID -> label`) for STATUS, DROPDOWN, and LABELS columns
- Non-fatal: if the table fetch or cache fails, `rows get` falls back to raw column IDs

### Store abstraction

Both cache types operate through a `Store` interface:

```go
type Store interface {
    Read(key string) ([]byte, error)
    Write(key string, data []byte) error
    Delete(key string) error
}
```

Two implementations:
- **`DiskStore`** — reads/writes to a directory on the real filesystem. Used in production.
- **`MemStore`** — in-memory map with no disk I/O. Used in tests.

The active store is injected via `CLI.CacheStore`. When nil (production), commands create a `DiskStore` using the configured cache directory. Tests inject a `MemStore` via `ExecOpts.CacheStore` so that:
- Tests never touch the filesystem for cache operations
- Tests that verify cache hits share a single `MemStore` instance across calls
- Tests that should have independent caches get separate `MemStore` instances automatically

### Configuration

```toml
[cache]
dir = "/custom/path"   # base directory (default: $TMPDIR/copera-cli-{version})
ttl = "1h"             # default TTL for doc content cache
```

The table schema cache always uses 15 minutes regardless of the configured TTL — schema changes are rare and stale schema only affects display labels, not data correctness.

---

## Alternatives Considered

### 1. In-memory only (no disk cache)

Rejected. The CLI is invoked as a separate process each time, so in-memory state is lost between calls. Disk caching is the only way to avoid redundant API calls across invocations.

### 2. SQLite or BoltDB

Rejected. Adds a CGo dependency (SQLite) or a heavier binary. The cache data is simple key-value pairs with TTL — flat files are sufficient and easier to inspect/debug.

### 3. JSON for table schema cache

Rejected in favor of the custom text format. The text format is human-readable when inspected directly, self-describing with version headers, and trivial to parse without a full JSON round-trip. It also makes the cache file format independent of Go struct serialization.

### 4. Cache in config directory instead of temp directory

Rejected. Cache files are ephemeral and can be regenerated. Temp directories are cleaned by the OS and don't clutter user config. The versioned directory name ensures binary upgrades start fresh.

---

## Consequences

- `copera cache status` shows cache location and size; `copera cache clean` removes all cached data.
- Users on slow connections benefit from cached doc content and table schemas.
- Tests run faster and more reliably with no filesystem side effects.
- The `Store` interface makes it straightforward to add new cache backends (e.g., Redis for shared environments) if ever needed.
