package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	_ "github.com/duckdb/duckdb-go/v2"
)

func main() {
	// Use a custom FlagSet so we can safely use "-h" for hostname instead of help
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	dbPath := fs.String("db", ":memory:", "Path to the DuckDB database file")
	initSQL := fs.String("init", "", "Initialization SQL to run on startup")
	queryStr := fs.String("query", "", "Directly execute a query and show the results")

	// New Quack flags
	host := fs.String("h", "", "Quack hostname (e.g., data-stage.tech.dronespotterlabs.com:443)")
	token := fs.String("t", "", "Quack token (can also be provided via QUACK_TOKEN env var)")

	fs.Parse(os.Args[1:])

	// Resolve Quack Token
	tok := *token
	if tok == "" {
		tok = os.Getenv("QUACK_TOKEN")
	}

	// Auto-generate the Quack loading SQL if a host is provided
	if *host != "" {
		quackInit := fmt.Sprintf("INSTALL quack; LOAD quack; ATTACH 'quack:%s' AS remote (TOKEN '%s', DISABLE_SSL false);", *host, tok)
		if *initSQL != "" {
			*initSQL = quackInit + "\n" + *initSQL
		} else {
			*initSQL = quackInit
		}
	}

	// If the "database" path is actually a single-file data source (Parquet,
	// CSV, JSON, etc.), open an in-memory DuckDB and create a view over it so
	// it shows up in the table list and can be browsed like any other table.
	openPath := *dbPath
	isDataFile := false
	if fileInit := fileDataSourceInit(*dbPath); fileInit != "" {
		if _, err := os.Stat(*dbPath); err != nil {
			fmt.Printf("Error: cannot open data file %q: %s\n", *dbPath, err)
			os.Exit(1)
		}
		isDataFile = true
		openPath = ":memory:"
		if *initSQL != "" {
			*initSQL = fileInit + "\n" + *initSQL
		} else {
			*initSQL = fileInit
		}
	}

	db, err := sql.Open("duckdb", openPath)
	if err != nil {
		fmt.Println("Error opening database:", err)
		os.Exit(1)
	}
	defer db.Close()

	// Limit pool to 1 connection to maintain ATTACH/LOAD state
	db.SetMaxOpenConns(1)

	if *initSQL != "" {
		_, err = db.Exec(*initSQL)
		if err != nil {
			fmt.Printf("Error executing initialization SQL:\n%s\n", err)
			if isDataFile {
				fmt.Println("\nHint: the -db path was treated as a data file (Parquet/CSV/JSON/Vortex). If the file is corrupt or not in that format, use -query to run a custom read command or fix the file.")
			}
			os.Exit(1)
		}
	}

	finalQuery := *queryStr
	if finalQuery == "" && fs.NArg() > 0 {
		finalQuery = fs.Arg(0)
	}

	var initialModel tea.Model

	if finalQuery != "" {
		initialModel = newModelWithQuery(db, finalQuery)
	} else {
		initialModel = newModel(db)
	}

	p := tea.NewProgram(initialModel)

	if _, err := p.Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}

// fileDataSourceInit returns initialization SQL for single-file DuckDB-readable
// formats (Parquet, CSV, JSON). The returned view name is derived from the
// file's base name. An empty string means the path is not a recognised file
// source and should be opened as a normal DuckDB database.
func fileDataSourceInit(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	name := strings.TrimSuffix(filepath.Base(path), ext)
	// Sanitise the view name a bit.
	name = strings.ReplaceAll(name, "\"", "")
	if name == "" {
		name = "data"
	}

	// Escape single quotes in the path for safe SQL string literals.
	safePath := strings.ReplaceAll(path, "'", "''")

	switch ext {
	case ".parquet":
		return fmt.Sprintf("CREATE OR REPLACE VIEW \"%s\" AS SELECT * FROM read_parquet('%s');", name, safePath)
	case ".csv":
		return fmt.Sprintf("CREATE OR REPLACE VIEW \"%s\" AS SELECT * FROM read_csv_auto('%s');", name, safePath)
	case ".json":
		return fmt.Sprintf("CREATE OR REPLACE VIEW \"%s\" AS SELECT * FROM read_json_auto('%s');", name, safePath)
	case ".vortex":
		return fmt.Sprintf("INSTALL vortex; LOAD vortex; CREATE OR REPLACE VIEW \"%s\" AS SELECT * FROM read_vortex('%s');", name, safePath)
	default:
		return ""
	}
}
