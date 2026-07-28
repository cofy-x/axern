package registryauth

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
)

// Entry represents authentication credentials for a registry.
type Entry struct {
	Auth string `json:"Auth"`
}

// Config maps registry hosts/repos to their authentication credentials.
type Config map[string]Entry

// Resolve returns the most specific auth entry for a registry repository.
func (c Config) Resolve(host, repo string) string {
	if entry, ok := c[host+"/"+repo]; ok && entry.Auth != "" {
		return entry.Auth
	}
	return c[host].Auth
}

// Load reads and parses a registry auths file.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read registry auths file: %w", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse registry auths file: %w", err)
	}
	return cfg, nil
}

// Parse supports both:
// 1) flat format: {"registry.host": {"Auth": "..."}}
// 2) docker config format: {"auths": {"registry.host": {"auth": "..."}}}
func Parse(data []byte) (Config, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	entries := root
	for key, raw := range root {
		if strings.EqualFold(key, "auths") {
			var dockerAuths map[string]json.RawMessage
			if err := json.Unmarshal(raw, &dockerAuths); err != nil {
				return nil, fmt.Errorf("invalid auths section: %w", err)
			}
			entries = dockerAuths
			break
		}
	}

	result := make(Config, len(entries))
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		entry, err := parseEntry(entries[key])
		if err != nil {
			return nil, fmt.Errorf("invalid auth entry for %q: %w", key, err)
		}

		normalizedKey := normalizeKey(key)
		if normalizedKey == "" {
			continue
		}

		// Keep non-empty auth when multiple keys normalize to the same target.
		if prev, ok := result[normalizedKey]; ok && prev.Auth != "" && entry.Auth == "" {
			continue
		}
		result[normalizedKey] = entry
	}

	return result, nil
}

type rawEntry struct {
	AuthUpper string `json:"Auth"`
	AuthLower string `json:"auth"`
}

func parseEntry(raw json.RawMessage) (Entry, error) {
	var entry rawEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return Entry{}, err
	}

	auth := entry.AuthUpper
	if auth == "" {
		auth = entry.AuthLower
	}
	return Entry{Auth: auth}, nil
}

func normalizeKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	key = strings.TrimSuffix(key, "/")

	// Docker configs may store keys as URLs (for example index.docker.io/v1).
	if strings.HasPrefix(key, "http://") || strings.HasPrefix(key, "https://") {
		u, err := url.Parse(key)
		if err == nil && u.Host != "" {
			path := strings.Trim(u.Path, "/")
			if path == "" || path == "v1" || path == "v2" {
				return u.Host
			}
			return u.Host + "/" + path
		}
	}

	return key
}
