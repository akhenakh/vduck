package main

import (
	"database/sql"
	"fmt"

	"charm.land/bubbles/v2/table"
)

// fetchTablesAndViews retrieves a list of all tables and views across all catalogs.
func fetchTablesAndViews(db *sql.DB) ([]string, error) {
	query := `
		SELECT database_name, schema_name, table_name 
		FROM duckdb_tables() 
		WHERE database_name NOT IN ('system', 'temp')
		
		UNION ALL 
		
		SELECT database_name, schema_name, view_name AS table_name 
		FROM duckdb_views() 
		WHERE database_name NOT IN ('system', 'temp')
		
		UNION ALL
		
		-- Workaround for DuckDB Quack bug #175: 
		-- Remote catalogs don't expose their tables to client introspection yet.
		-- We show a placeholder so the user knows the database is attached.
		SELECT database_name, '<unknown>' AS schema_name, '<Hidden Remote Tables>' AS table_name
		FROM duckdb_databases()
		WHERE database_name NOT IN ('system', 'temp')
		  AND database_name NOT IN (SELECT database_name FROM duckdb_tables())
		  AND database_name NOT IN (SELECT database_name FROM duckdb_views())
		
		ORDER BY database_name, schema_name, table_name;
	`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var catalog, schema, name string
		if err := rows.Scan(&catalog, &schema, &name); err != nil {
			return nil, err
		}

		fullName := fmt.Sprintf(`"%s"."%s"."%s"`, catalog, schema, name)
		names = append(names, fullName)
	}
	return names, nil
}

// fetchTableData executes a query and returns the columns and rows for our table model.
func fetchTableData(db *sql.DB, query string) ([]table.Column, []table.Row, error) {
	sqlRows, err := db.Query(query)
	if err != nil {
		return nil, nil, err
	}
	defer sqlRows.Close()

	colNames, err := sqlRows.Columns()
	if err != nil {
		return nil, nil, err
	}

	columns := make([]table.Column, len(colNames))
	for i, name := range colNames {
		columns[i] = table.Column{Title: name, Width: 15}
	}

	vals := make([]interface{}, len(colNames))
	for i := range colNames {
		vals[i] = new(sql.RawBytes)
	}

	var rows []table.Row
	for sqlRows.Next() {
		err = sqlRows.Scan(vals...)
		if err != nil {
			return nil, nil, err
		}

		row := make(table.Row, len(colNames))
		for i := range colNames {
			if b, ok := vals[i].(*sql.RawBytes); ok {
				row[i] = string(*b)
			} else {
				row[i] = "NULL"
			}
		}
		rows = append(rows, row)
	}

	return columns, rows, nil
}
