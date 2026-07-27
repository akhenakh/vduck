package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	_ "github.com/duckdb/duckdb-go/v2"
)

func main() {
	dbPath := flag.String("db", ":memory:", "Path to the DuckDB database file")
	initSQL := flag.String("init", "", "Initialization SQL to run on startup")
	queryStr := flag.String("query", "", "Directly execute a query and show the results")

	flag.Parse()

	db, err := sql.Open("duckdb", *dbPath)
	if err != nil {
		fmt.Println("Error opening database:", err)
		os.Exit(1)
	}
	defer db.Close()

	// CRITICAL FIX: Limit the pool to exactly 1 connection.
	// This ensures that stateful commands like 'LOAD quack' and 'ATTACH'
	// persist for all subsequent queries in the app.
	db.SetMaxOpenConns(1)

	// Execute any pre-string / initialization SQL
	if *initSQL != "" {
		_, err = db.Exec(*initSQL)
		if err != nil {
			fmt.Printf("Error executing initialization SQL:\n%s\n", err)
			os.Exit(1)
		}
	}

	finalQuery := *queryStr
	if finalQuery == "" && flag.NArg() > 0 {
		finalQuery = flag.Arg(0)
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
