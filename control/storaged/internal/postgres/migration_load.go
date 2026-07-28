package postgres

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var migrationFilePattern = regexp.MustCompile(`^(\d{6})_([a-z0-9_]+)\.sql$`)

func loadMigrations() ([]Migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read storaged postgres migrations: %w", err)
	}
	migrations := make([]Migration, 0, len(entries))
	seen := make(map[int64]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		version, name, ok := parseMigrationFileName(entry.Name())
		if !ok {
			return nil, fmt.Errorf("invalid storaged postgres migration file name %q", entry.Name())
		}
		if prior, exists := seen[version]; exists {
			return nil, fmt.Errorf("duplicate storaged postgres migration version %06d: %s and %s", version, prior, entry.Name())
		}
		contents, err := migrationFS.ReadFile(path.Join("migrations", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read storaged postgres migration %q: %w", entry.Name(), err)
		}
		sum := sha256.Sum256(contents)
		migrations = append(migrations, Migration{
			Version:  version,
			Name:     name,
			Checksum: hex.EncodeToString(sum[:]),
			SQL:      string(contents),
		})
		seen[version] = entry.Name()
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	if len(migrations) == 0 {
		return nil, fmt.Errorf("no storaged postgres migrations embedded")
	}
	return migrations, nil
}

func parseMigrationFileName(fileName string) (int64, string, bool) {
	matches := migrationFilePattern.FindStringSubmatch(fileName)
	if matches == nil {
		return 0, "", false
	}
	version, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil || version <= 0 {
		return 0, "", false
	}
	return version, matches[2], true
}
