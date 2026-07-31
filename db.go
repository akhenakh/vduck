package main

import (
	"database/sql"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
)

// stringifyValue converts a scanned driver value into a display string.
// DuckDB may return composite types (lists, structs, maps) as Go slices/maps
// that cannot be stored in sql.RawBytes, so we use interface{} containers and
// format the values here. JSON and geometry columns are pre-converted to text
// by the query rewrite in fetchTableData, so they arrive as plain strings.
func stringifyValue(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(val)
	case []interface{}:
		parts := make([]string, len(val))
		for i, p := range val {
			parts[i] = stringifyValue(p)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case fmt.Stringer:
		return val.String()
	default:
		return fmt.Sprintf("%v", val)
	}
}

// rewriteColumns inspects the result metadata of an already-executed query and,
// if any column needs interpretation, returns a rewritten query that produces
// plain text values for it:
//
//   - JSON columns are cast to VARCHAR so DuckDB returns the raw JSON text
//     instead of parsed Go maps/slices.
//   - Geometry columns (DuckDB type GEOMETRY) are rendered via the spatial
//     extension. When geoAsJSON is true they are converted to GeoJSON text,
//     otherwise to WKT.
//   - Columns whose name hints at geometry but hold raw WKB (BLOB) or WKT
//     (VARCHAR) data are parsed with ST_GeomFromWKB / ST_GeomFromText first.
//
// The returned bool reports whether the rewritten query requires the spatial
// extension to be loaded. The original query is returned unchanged when there
// is nothing to rewrite (or the metadata cannot be read).
func rewriteColumns(query string, sqlRows *sql.Rows, geoAsJSON bool) (string, bool, error) {
	cts, err := sqlRows.ColumnTypes()
	if err != nil {
		return query, false, err
	}

	jsonIdx := make([]bool, len(cts))
	geoIdx := make([]bool, len(cts))
	needsSpatial := false
	hasRewrite := false
	for i, ct := range cts {
		typ := strings.ToUpper(ct.DatabaseTypeName())
		name := ct.Name()
		switch {
		case typ == "JSON":
			jsonIdx[i] = true
			hasRewrite = true
		case typ == "GEOMETRY":
			geoIdx[i] = true
			needsSpatial = true
			hasRewrite = true
		case (typ == "BLOB" || typ == "VARCHAR") && isWKBColumn(name):
			geoIdx[i] = true
			needsSpatial = true
			hasRewrite = true
		}
	}
	if !hasRewrite {
		return query, false, nil
	}

	names := make([]string, len(cts))
	for i, ct := range cts {
		names[i] = ct.Name()
	}

	var b strings.Builder
	b.WriteString("SELECT ")
	for i, name := range names {
		if i > 0 {
			b.WriteString(", ")
		}
		ident := quoteIdent(name)
		switch {
		case jsonIdx[i]:
			b.WriteString("CAST(" + ident + " AS VARCHAR) AS " + ident)
		case geoIdx[i]:
			// Try WKB first, then WKT, for BLOB/VARCHAR columns; GEOMETRY
			// columns are passed straight to the formatting function.
			geomExpr := ""
			switch strings.ToUpper(cts[i].DatabaseTypeName()) {
			case "BLOB":
				geomExpr = "ST_GeomFromWKB(" + ident + ")"
			case "VARCHAR":
				geomExpr = "ST_GeomFromText(" + ident + ")"
			default:
				geomExpr = ident
			}
			if geoAsJSON {
				b.WriteString("CAST(ST_AsGeoJSON(" + geomExpr + ") AS VARCHAR) AS " + ident)
			} else {
				b.WriteString("ST_AsText(" + geomExpr + ") AS " + ident)
			}
		default:
			b.WriteString(ident)
		}
	}
	inner := strings.TrimSuffix(strings.TrimSpace(query), ";")
	b.WriteString(" FROM (" + inner + ") AS vduck_sub")
	return b.String(), needsSpatial, nil
}

// ensureSpatialLoaded makes the spatial extension available for geometry
// rewriting. It is best-effort: if the extension cannot be installed/loaded the
// caller falls back to the original query.
func ensureSpatialLoaded(db *sql.DB) {
	db.Exec("INSTALL spatial")
	db.Exec("LOAD spatial")
}

// quoteIdent quotes a column name as a DuckDB double-quoted identifier.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// isWKBColumn reports whether a column name hints that its values are geometry
// data encoded as WKB or WKT.
func isWKBColumn(name string) bool {
	switch strings.ToLower(name) {
	case "geo", "geom", "geometry", "wkb", "wkt":
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

// fetchTableSchema runs DESCRIBE on the given table/view and returns a formatted
// string representation of its schema, suitable for copying to the clipboard.
func fetchTableSchema(db *sql.DB, source string) (string, error) {
	source = strings.TrimSuffix(strings.TrimSpace(source), ";")
	query := "DESCRIBE " + source + ";"
	rows, err := db.Query(query)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	colNames, err := rows.Columns()
	if err != nil {
		return "", err
	}

	vals := make([]interface{}, len(colNames))
	for i := range colNames {
		vals[i] = new(interface{})
	}

	var records []table.Row
	for rows.Next() {
		if err := rows.Scan(vals...); err != nil {
			return "", err
		}
		row := make(table.Row, len(colNames))
		for i := range colNames {
			row[i] = stringifyValue(*vals[i].(*interface{}))
		}
		records = append(records, row)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	// Build a simple aligned text representation.
	widths := make([]int, len(colNames))
	for i, name := range colNames {
		widths[i] = len(name)
	}
	for _, row := range records {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	var b strings.Builder
	for i, name := range colNames {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(fmt.Sprintf("%-*s", widths[i], name))
	}
	b.WriteString("\n")
	for i, width := range widths {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(strings.Repeat("-", width))
	}
	b.WriteString("\n")
	for _, row := range records {
		for i, cell := range row {
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(fmt.Sprintf("%-*s", widths[i], cell))
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// fetchTableData executes a query and returns the columns and rows for our table model.
// The geoAsJSON flag controls how geometry columns are rendered (GeoJSON text
// when true, WKT otherwise).
func fetchTableData(db *sql.DB, query string, geoAsJSON bool) ([]table.Column, []table.Row, error) {
	sqlRows, err := db.Query(query)
	if err != nil {
		return nil, nil, err
	}

	// The go-duckdb driver hands back JSON columns as parsed Go maps/slices
	// (rendered as e.g. "map[a:1 ...]") and geometry columns as raw WKB bytes.
	// Detect them from the result metadata and rewrite the query so DuckDB
	// serializes them back to text.
	rewritten, needsSpatial, err := rewriteColumns(query, sqlRows, geoAsJSON)
	if err != nil {
		sqlRows.Close()
		return nil, nil, err
	}
	if rewritten != query {
		sqlRows.Close()
		if needsSpatial {
			ensureSpatialLoaded(db)
		}
		sqlRows, err = db.Query(rewritten)
		if err != nil {
			// Fall back to the original query if the rewrite is unsupported.
			sqlRows, err = db.Query(query)
			if err != nil {
				return nil, nil, err
			}
		}
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
			row[i] = stringifyValue(*vals[i].(*interface{}))
		}
		rows = append(rows, row)
	}

	return columns, rows, nil
}
