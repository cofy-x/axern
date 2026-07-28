package postgres

import (
	"strings"
	"testing"
)

func TestLoadMigrations(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("loadMigrations() returned no migrations")
	}
	if migrations[0].Version != 1 {
		t.Fatalf("first migration version = %d, want 1", migrations[0].Version)
	}
	if migrations[0].Name != "initial" {
		t.Fatalf("first migration name = %q, want initial", migrations[0].Name)
	}
	if migrations[0].Checksum == "" {
		t.Fatal("first migration checksum is empty")
	}
	if strings.TrimSpace(migrations[0].SQL) == "" {
		t.Fatal("first migration has no SQL statements")
	}
	for index, migration := range migrations {
		if migration.Version != int64(index+1) {
			t.Fatalf("migration[%d].Version = %d, want %d", index, migration.Version, index+1)
		}
		if strings.Contains(strings.ToLower(migration.SQL), "create table invokes") {
			t.Fatalf("migration %d still creates removed invokes table", migration.Version)
		}
	}
}

func TestParseMigrationFileName(t *testing.T) {
	version, name, ok := parseMigrationFileName("000123_add_widgets.sql")
	if !ok {
		t.Fatal("parseMigrationFileName() ok = false, want true")
	}
	if version != 123 {
		t.Fatalf("version = %d, want 123", version)
	}
	if name != "add_widgets" {
		t.Fatalf("name = %q, want add_widgets", name)
	}
}

func TestParseMigrationFileNameRejectsInvalidNames(t *testing.T) {
	for _, fileName := range []string{
		"schema.sql",
		"1_initial.sql",
		"000000_initial.sql",
		"000001_initial-up.sql",
		"000001_Initial.sql",
	} {
		if _, _, ok := parseMigrationFileName(fileName); ok {
			t.Fatalf("parseMigrationFileName(%q) ok = true, want false", fileName)
		}
	}
}
