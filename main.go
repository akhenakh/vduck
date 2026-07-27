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

	db, err := sql.Open("duckdb", *dbPath)
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
