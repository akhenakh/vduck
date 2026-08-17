package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
)

func newMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestFetchTableDataJSONRenderedAsText(t *testing.T) {
	db := newMemDB(t)
	if _, err := db.Exec(`CREATE TABLE t (id INT, j JSON, b BLOB)`); err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO t VALUES (1, '{"a":1,"b":[2,3]}', x'DEAD')`)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cols, rows, err := fetchTableData(ctx, db, "SELECT * FROM t", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 3 || len(rows) != 1 {
		t.Fatalf("want 3 cols / 1 row, got %d cols / %d rows", len(cols), len(rows))
	}
	cell := rows[0][1]
	// JSON must be rendered as raw text, not a Go map (which would look like map[a:1 ...]).
	if !strings.Contains(cell, `"a"`) || strings.Contains(cell, "map[") {
		t.Fatalf("JSON column not rendered as text: %q", cell)
	}
	if rows[0][0] != "1" {
		t.Fatalf("id = %q, want 1", rows[0][0])
	}
}

func TestFetchTableDataCanceledContext(t *testing.T) {
	db := newMemDB(t)
	db.Exec(`CREATE TABLE t (id INT)`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel up-front: the fetch must respect the context.

	if _, _, err := fetchTableData(ctx, db, "SELECT * FROM t", false); err == nil {
		t.Fatal("expected an error from a canceled context")
	}
}

func TestFetchTableDataTimeout(t *testing.T) {
	db := newMemDB(t)

	// A query far too slow for a 1ms deadline must be cut off by the context.
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, _, err := fetchTableData(ctx, db, "SELECT sum(range) FROM range(1000000000)", false); err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 15*time.Second {
		t.Fatalf("timeout did not cut off the query (elapsed %s)", time.Since(start))
	}
}

func TestStringifyValue(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want string
	}{
		{"nil", nil, "NULL"},
		{"bytes", []byte("hello"), "hello"},
		{"string", "world", "world"},
		{"int", 42, "42"},
		{"float", 3.14, "3.14"},
		{"bool", true, "true"},
		{"list", []interface{}{1, 2, "x"}, "[1, 2, x]"},
		{"nested list", []interface{}{[]interface{}{1}, "a"}, "[[1], a]"},
		{"empty list", []interface{}{}, "[]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stringifyValue(tt.in); got != tt.want {
				t.Fatalf("stringifyValue(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildRewrite(t *testing.T) {
	q := "SELECT * FROM t"

	// No geometry/JSON columns: query returned unchanged.
	got, spatial := buildRewrite(q, []resultColumnPair{{"id", "INTEGER"}}, false)
	if got != q || spatial {
		t.Fatalf("no-rewrite: got %q spatial=%v, want %q false", got, spatial, q)
	}

	// JSON column cast to VARCHAR.
	got, spatial = buildRewrite(q, []resultColumnPair{{"id", "INTEGER"}, {"j", "JSON"}}, false)
	wantJSON := `SELECT "id", CAST("j" AS VARCHAR) AS "j" FROM (SELECT * FROM t) AS vduck_sub`
	if got != wantJSON || spatial {
		t.Fatalf("json: got %q spatial=%v, want %q", got, spatial, wantJSON)
	}

	// GEOMETRY column rendered as GeoJSON.
	got, spatial = buildRewrite(q, []resultColumnPair{{"geom", "GEOMETRY"}}, true)
	wantGeo := `SELECT CAST(ST_AsGeoJSON("geom") AS VARCHAR) AS "geom" FROM (SELECT * FROM t) AS vduck_sub`
	if got != wantGeo || !spatial {
		t.Fatalf("geometry: got %q spatial=%v, want %q true", got, spatial, wantGeo)
	}

	// BLOB column with a geometry-ish name parsed from WKB to WKT.
	got, spatial = buildRewrite(q, []resultColumnPair{{"the_geom", "BLOB"}}, false)
	wantWKB := `SELECT ST_AsText(ST_GeomFromWKB("the_geom")) AS "the_geom" FROM (SELECT * FROM t) AS vduck_sub`
	if got != wantWKB || !spatial {
		t.Fatalf("wkb: got %q spatial=%v, want %q true", got, spatial, wantWKB)
	}

	// Trailing semicolon on the query is stripped in the inner subquery.
	got, _ = buildRewrite(q+";", []resultColumnPair{{"j", "JSON"}}, false)
	if !strings.Contains(got, "FROM (SELECT * FROM t) AS vduck_sub") {
		t.Fatalf("semicolon not stripped: %q", got)
	}
}

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"name", `"name"`},
		{`a"b`, `"a""b"`},
		{"", `""`},
	}
	for _, tt := range tests {
		if got := quoteIdent(tt.in); got != tt.want {
			t.Fatalf("quoteIdent(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsWKBColumn(t *testing.T) {
	yes := []string{
		"geo", "geom", "geometry", "wkb", "wkt",
		"the_geom", "GEOM", "geom_2154", "location_wkb", "poly_wkt",
	}
	no := []string{"id", "name", "description", "created_at"}

	for _, n := range yes {
		if !isWKBColumn(n) {
			t.Errorf("isWKBColumn(%q) = false, want true", n)
		}
	}
	for _, n := range no {
		if isWKBColumn(n) {
			t.Errorf("isWKBColumn(%q) = true, want false", n)
		}
	}
}
