package main

import (
	"strings"
	"testing"
)

func TestFileDataSourceInit(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		wantSub string
		wantNil bool
	}{
		{"parquet", "data.parquet", "read_parquet('data.parquet')", false},
		{"csv", "data.csv", "read_csv_auto('data.csv')", false},
		{"json", "data.json", "read_json_auto('data.json')", false},
		{"geojson", "data.geojson", "ST_Read('data.geojson')", false},
		{"vortex", "data.vortex", "read_vortex('data.vortex')", false},
		{"unknown ext", "my.db", "", true},
		{"no ext", "mydb", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fileDataSourceInit(tt.path)
			if tt.wantNil {
				if got != "" {
					t.Fatalf("fileDataSourceInit(%q) = %q, want empty", tt.path, got)
				}
				return
			}
			if got == "" {
				t.Fatalf("fileDataSourceInit(%q) returned empty", tt.path)
			}
			if !strings.Contains(got, tt.wantSub) {
				t.Fatalf("fileDataSourceInit(%q) = %q, want it to contain %q", tt.path, got, tt.wantSub)
			}
			if !strings.Contains(got, `CREATE OR REPLACE VIEW "data" AS SELECT * FROM`) {
				t.Fatalf("fileDataSourceInit(%q) missing expected view clause: %q", tt.path, got)
			}
		})
	}
}

func TestFileDataSourceInitEscapesQuotes(t *testing.T) {
	got := fileDataSourceInit(`a'b.json`)
	// The path must be escaped by doubling the single quote in the SQL literal.
	if !strings.Contains(got, `read_json_auto('a''b.json')`) {
		t.Fatalf("quote escaping failed: %q", got)
	}
}

func TestFileDataSourceInitEmptyName(t *testing.T) {
	// A path whose base is only an extension should fall back to "data".
	got := fileDataSourceInit(".parquet")
	if !strings.Contains(got, `CREATE OR REPLACE VIEW "data" AS`) {
		t.Fatalf("expected fallback view name 'data', got: %q", got)
	}
}
