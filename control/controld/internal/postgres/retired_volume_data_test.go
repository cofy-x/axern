package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestCheckRetiredVolumeData(t *testing.T) {
	for _, tc := range []struct {
		name      string
		tables    map[string]bool
		rows      map[string]bool
		wantTable string
	}{
		{name: "fresh database"},
		{name: "empty retired tables", tables: map[string]bool{"storage_volume_claims": true, "storage_volume_bindings": true}},
		{name: "claim data", tables: map[string]bool{"storage_volume_claims": true}, rows: map[string]bool{"storage_volume_claims": true}, wantTable: "storage_volume_claims"},
		{name: "binding data without claims", tables: map[string]bool{"storage_volume_bindings": true}, rows: map[string]bool{"storage_volume_bindings": true}, wantTable: "storage_volume_bindings"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := &retiredVolumeTestQuery{tables: tc.tables, rows: tc.rows}
			err := checkRetiredVolumeData(context.Background(), query)
			if tc.wantTable == "" {
				if err != nil {
					t.Fatalf("checkRetiredVolumeData() error = %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantTable) || !strings.Contains(err.Error(), "archived Axern version") {
				t.Fatalf("checkRetiredVolumeData() error = %v, want export refusal for %s", err, tc.wantTable)
			}
			for _, sql := range query.queries {
				if !strings.HasPrefix(sql, "SELECT ") {
					t.Fatalf("non-read-only query: %s", sql)
				}
				if strings.Contains(sql, "WHERE") {
					t.Fatalf("legacy rows must include tombstones: %s", sql)
				}
			}
		})
	}
}

func TestCheckRetiredVolumeDataFailsClosedOnReadError(t *testing.T) {
	want := errors.New("database read failed")
	err := checkRetiredVolumeData(context.Background(), &retiredVolumeTestQuery{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestCheckRetiredVolumeDataRequiresDatabase(t *testing.T) {
	var db *DB
	if err := db.CheckRetiredVolumeData(context.Background()); err == nil {
		t.Fatal("missing database accepted")
	}
}

type retiredVolumeTestQuery struct {
	tables  map[string]bool
	rows    map[string]bool
	err     error
	queries []string
}

func (q *retiredVolumeTestQuery) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	q.queries = append(q.queries, sql)
	if q.err != nil {
		return retiredVolumeTestRow{err: q.err}
	}
	switch sql {
	case "SELECT to_regclass($1) IS NOT NULL":
		return retiredVolumeTestRow{value: q.tables[args[0].(string)]}
	case "SELECT EXISTS (SELECT 1 FROM storage_volume_claims)":
		return retiredVolumeTestRow{value: q.rows["storage_volume_claims"]}
	case "SELECT EXISTS (SELECT 1 FROM storage_volume_bindings)":
		return retiredVolumeTestRow{value: q.rows["storage_volume_bindings"]}
	default:
		return retiredVolumeTestRow{err: fmt.Errorf("unexpected query: %s", sql)}
	}
}

type retiredVolumeTestRow struct {
	value bool
	err   error
}

func (r retiredVolumeTestRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*bool)) = r.value
	return nil
}
