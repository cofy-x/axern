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
	if len(splitSQLStatements(migrations[0].SQL)) == 0 {
		t.Fatal("first migration has no SQL statements")
	}
	if len(migrations) != 1 {
		t.Fatalf("migrations = %#v, want one complete initial schema", migrations)
	}
	for _, fragment := range []string{
		"claim_id text PRIMARY KEY",
		"storage_volume_claims_active_name_idx",
		"WHERE status <> 'VOLUME_STATUS_DELETED'",
	} {
		if !strings.Contains(migrations[0].SQL, fragment) {
			t.Errorf("initial migration does not contain %q", fragment)
		}
	}
	for _, fragment := range []string{"reclaim_lease_token_hash bytea", "storage_volume_claims_reclaim_due_idx"} {
		if !strings.Contains(migrations[0].SQL, fragment) {
			t.Errorf("initial migration does not contain %q", fragment)
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

func TestSplitSQLStatements(t *testing.T) {
	got := splitSQLStatements("CREATE TABLE a(id INT);\n\nCREATE TABLE b(id INT);")
	if len(got) != 2 {
		t.Fatalf("len(splitSQLStatements()) = %d, want 2", len(got))
	}
	if got[0] != "CREATE TABLE a(id INT)" {
		t.Fatalf("first statement = %q", got[0])
	}
}
