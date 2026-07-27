package main

import (
	"database/sql"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	"github.com/peterstace/simplefeatures/geom"
)

// stringifyValue converts a scanned driver value into a display string.
// DuckDB may return composite types (lists, structs, maps) as Go slices/maps
// that cannot be stored in sql.RawBytes, so we use interface{} containers
// and format the values here.
//
// For binary columns whose name suggests they contain WKB (geo, geom, geometry,
// wkb), we attempt to decode the bytes as Well Known Binary and render the
// resulting geometry as WKT.
func stringifyValue(v interface{}, colName string) string {
	switch val := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		if isWKBColumn(colName) {
			if g, err := geom.UnmarshalWKB(val); err == nil {
				return g.AsText()
			}
		}
		return string(val)
	case []interface{}:
		parts := make([]string, len(val))
		for i, p := range val {
			parts[i] = stringifyValue(p, "")
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case fmt.Stringer:
		return val.String()
	default:
		return fmt.Sprintf("%v", val)
	}
}

// isWKBColumn reports whether a column name hints that its values are WKB.
func isWKBColumn(name string) bool {
	switch strings.ToLower(name) {
	case "geo", "geom", "geometry", "wkb":
		return true
	default:
		return false
	}
}

// fetchTablesAndViews retrieves a list of all tables and views across all catalogs.
// It uses information_schema.tables as the primary source, falling back to
// duckdb_tables()/duckdb_views(), and adds a placeholder for attached catalogs
// (e.g. Quack remote) that don't expose their metadata to client introspection.
func fetchTablesAndViews(db *sql.DB) ([]string, error) {
	query := `
		WITH all_objects AS (
			SELECT table_catalog AS database_name,
			       table_schema  AS schema_name,
			       table_name    AS table_name
			FROM information_schema.tables
			WHERE table_catalog NOT IN ('system', 'temp')
			  AND table_schema NOT IN ('information_schema', 'pg_catalog')

			UNION ALL

			SELECT database_name, schema_name, table_name
			FROM duckdb_tables()
			WHERE database_name NOT IN ('system', 'temp')

			UNION ALL

			SELECT database_name, schema_name, view_name AS table_name
			FROM duckdb_views()
			WHERE database_name NOT IN ('system', 'temp')
		),
		hidden AS (
			SELECT database_name,
			       '<unknown>' AS schema_name,
			       '<Hidden Remote Tables>' AS table_name
			FROM duckdb_databases()
			WHERE database_name NOT IN ('system', 'temp')
			  AND database_name NOT IN (SELECT DISTINCT database_name FROM all_objects)
		)
		SELECT DISTINCT database_name, schema_name, table_name FROM all_objects
		UNION ALL
		SELECT database_name, schema_name, table_name FROM hidden
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
		vals[i] = new(interface{})
	}

	var rows []table.Row
	for sqlRows.Next() {
		err = sqlRows.Scan(vals...)
		if err != nil {
			return nil, nil, err
		}

		row := make(table.Row, len(colNames))
		for i := range colNames {
			row[i] = stringifyValue(*vals[i].(*interface{}), colNames[i])
		}
		rows = append(rows, row)
	}

	return columns, rows, nil
}
