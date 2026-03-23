package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// ProfileValues holds the fields that can be written to a profile section.
type ProfileValues struct {
	Token     string `toml:"token,omitempty"`
	BoardID   string `toml:"board_id,omitempty"`
	TableID   string `toml:"table_id,omitempty"`
	RowID     string `toml:"row_id,omitempty"`
	ChannelID string `toml:"channel_id,omitempty"`
	DocID     string `toml:"doc_id,omitempty"`
}

// WriteProfile upserts a profile section in the config file at path.
// The file is created if it does not exist. Existing keys not in vals are preserved.
func WriteProfile(path, profileName string, vals ProfileValues) error {
	// Read existing document (or start fresh)
	doc := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := toml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("config: parse %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("config: read %s: %w", path, err)
	}

	// Ensure profiles map exists
	profiles, _ := doc["profiles"].(map[string]any)
	if profiles == nil {
		profiles = map[string]any{}
		doc["profiles"] = profiles
	}

	// Merge vals into existing profile (preserve keys not in vals)
	existing, _ := profiles[profileName].(map[string]any)
	if existing == nil {
		existing = map[string]any{}
	}
	if vals.Token != "" {
		existing["token"] = vals.Token
	}
	if vals.BoardID != "" {
		existing["board_id"] = vals.BoardID
	}
	if vals.TableID != "" {
		existing["table_id"] = vals.TableID
	}
	if vals.RowID != "" {
		existing["row_id"] = vals.RowID
	}
	if vals.ChannelID != "" {
		existing["channel_id"] = vals.ChannelID
	}
	if vals.DocID != "" {
		existing["doc_id"] = vals.DocID
	}
	profiles[profileName] = existing

	// Marshal and write
	data, err := toml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("config: create directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return nil
}

// DeleteToken removes the token field from a profile in the config file at path.
func DeleteToken(path, profileName string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // nothing to do
	}
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}

	doc := map[string]any{}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("config: parse %s: %w", path, err)
	}

	profiles, _ := doc["profiles"].(map[string]any)
	if profiles == nil {
		return nil
	}
	profile, _ := profiles[profileName].(map[string]any)
	if profile == nil {
		return nil
	}
	delete(profile, "token")
	profiles[profileName] = profile

	out, err := toml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	return os.WriteFile(path, out, 0600)
}
