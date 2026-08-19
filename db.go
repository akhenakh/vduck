package main

import (
	"context"
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

// resultColumnPair is the name and DuckDB type of a single result column, as
// reported by DESCRIBE.
type resultColumnPair struct {
	name string
	typ  string
}

// analyzeSchema returns the result column names and types of the given query by
// asking DuckDB to describe it. Describing only plans the query and does not
// scan any rows, so it is cheap even for large tables and avoids executing the
// data query just to inspect its types. When the query cannot be described
// (e.g. a non-SELECT statement) it returns an error and the caller falls back
// to running the query without any rewriting.
func analyzeSchema(ctx context.Context, db *sql.DB, query string) ([]resultColumnPair, error) {
	inner := strings.TrimSuffix(strings.TrimSpace(query), ";")
	rows, err := db.QueryContext(ctx, "DESCRIBE ("+inner+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []resultColumnPair
	var name, typ, nul, key, def, extra interface{}
	for rows.Next() {
		if err := rows.Scan(&name, &typ, &nul, &key, &def, &extra); err != nil {
			return nil, err
		}
		out = append(out, resultColumnPair{name: strval(name), typ: strval(typ)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// strval best-effort converts a value scanned from a SQL column to a string
// (NULL becomes "").
func strval(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// buildRewrite inspects the result column types and, if any column needs
// interpretation, returns a rewritten query that produces plain text for it:
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
// is nothing to rewrite.
func buildRewrite(query string, cols []resultColumnPair, geoAsJSON bool) (string, bool) {
	jsonIdx := make([]bool, len(cols))
	geoIdx := make([]bool, len(cols))
	needsSpatial := false
	hasRewrite := false
	for i, c := range cols {
		typ := strings.ToUpper(c.typ)
		switch {
		case typ == "JSON":
			jsonIdx[i] = true
			hasRewrite = true
		case isGeometryType(typ):
			geoIdx[i] = true
			needsSpatial = true
			hasRewrite = true
		case (typ == "BLOB" || typ == "VARCHAR") && isWKBColumn(c.name):
			geoIdx[i] = true
			needsSpatial = true
			hasRewrite = true
		}
	}
	if !hasRewrite {
		return query, false
	}

	var b strings.Builder
	b.WriteString("SELECT ")
	for i, c := range cols {
		if i > 0 {
			b.WriteString(", ")
		}
		ident := quoteIdent(c.name)
		switch {
		case jsonIdx[i]:
			b.WriteString("CAST(" + ident + " AS VARCHAR) AS " + ident)
		case geoIdx[i]:
			// GEOMETRY columns go straight to the formatting function;
			// BLOB/VARCHAR columns are parsed from WKB/WKT first.
			geomExpr := ident
			switch strings.ToUpper(c.typ) {
			case "BLOB":
				geomExpr = "ST_GeomFromWKB(" + ident + ")"
			case "VARCHAR":
				geomExpr = "ST_GeomFromText(" + ident + ")"
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
	return b.String(), needsSpatial
}

// ensureSpatialLoaded makes the spatial extension available for geometry
// rewriting. It is best-effort: if the extension cannot be installed/loaded the
// caller falls back to the original query.
func ensureSpatialLoaded(ctx context.Context, db *sql.DB) {
	db.ExecContext(ctx, "INSTALL spatial")
	db.ExecContext(ctx, "LOAD spatial")
}

// quoteIdent quotes a column name as a DuckDB double-quoted identifier.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// isWKBColumn reports whether a column name hints that its values are geometry
// data encoded as WKB or WKT. It matches common PostGIS names (the_geom,
// geom_2154, *_wkb, *_wkt) in addition to the bare names. A false positive is
// safe: the geometry rewrite will fail and fall back to the original query.
func isWKBColumn(name string) bool {
	n := strings.ToLower(name)
	if n == "geo" {
		return true
	}
	return strings.Contains(n, "geom") || strings.HasSuffix(n, "wkb") || strings.HasSuffix(n, "wkt")
}

// isGeometryColumn reports whether a result column holds geometry data: a
// DuckDB geometry type, or a BLOB/VARCHAR column whose name hints at WKB/WKT.
func isGeometryColumn(c resultColumnPair) bool {
	typ := strings.ToUpper(c.typ)
	if isGeometryType(typ) {
		return true
	}
	return (typ == "BLOB" || typ == "VARCHAR") && isWKBColumn(c.name)
}

// isGeometryType reports whether a DuckDB type string is a geometry type. It
// matches GEOMETRY as well as the spatial extension's typed geometry columns
// (POINT_2D, POLYGON, ...) and their CRS-qualified variants, e.g.
// GEOMETRY('EPSG:4326') or GEOMETRY('OGC:CRS84').
func isGeometryType(typ string) bool {
	t := strings.ToUpper(strings.TrimSpace(typ))
	if i := strings.IndexByte(t, '('); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	for _, dim := range []string{"_2D", "_3D", "_4D"} {
		t = strings.TrimSuffix(t, dim)
	}
	switch t {
	case "GEOMETRY", "POINT", "LINESTRING", "POLYGON",
		"MULTIPOINT", "MULTILINESTRING", "MULTIPOLYGON", "GEOMETRYCOLLECTION":
		return true
	}
	return false
}

// geometryColumns returns the indices of the geometry columns in a result set.
func geometryColumns(cols []resultColumnPair) []int {
	var idx []int
	for i, c := range cols {
		if isGeometryColumn(c) {
			idx = append(idx, i)
		}
	}
	return idx
}

// fetchTablesAndViews retrieves a list of all tables and views across all catalogs.
// It uses information_schema.tables as the primary source, falling back to
// duckdb_tables()/duckdb_views(), and adds a placeholder for attached catalogs
// (e.g. Quack remote) that don't expose their metadata to client introspection.
func fetchTablesAndViews(ctx context.Context, db *sql.DB) ([]string, error) {
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
	rows, err := db.QueryContext(ctx, query)
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
func fetchTableSchema(ctx context.Context, db *sql.DB, source string) (string, error) {
	source = strings.TrimSuffix(strings.TrimSpace(source), ";")
	query := "DESCRIBE " + source + ";"
	rows, err := db.QueryContext(ctx, query)
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

// fetchTableData executes a query and returns the columns and rows for the table
// model, plus the indices of any geometry columns in the result. It asks DuckDB
// to describe the query first (no row scan), decides whether JSON/geometry
// columns need a text rewrite, and then executes the data query exactly once.
// The geoAsJSON flag controls how geometry columns are rendered (GeoJSON text
// when true, WKT otherwise). The context bounds how long the fetch may run.
func fetchTableData(ctx context.Context, db *sql.DB, query string, geoAsJSON bool) ([]table.Column, []table.Row, []int, error) {
	// Inspect the result schema cheaply and build a rewrite if needed. If the
	// query cannot be described, run it as-is with no rewrite.
	finalQuery := query
	var geoCols []int
	if cols, err := analyzeSchema(ctx, db, query); err == nil {
		geoCols = geometryColumns(cols)
		rewritten, needsSpatial := buildRewrite(query, cols, geoAsJSON)
		if needsSpatial {
			ensureSpatialLoaded(ctx, db)
		}
		finalQuery = rewritten
	}

	sqlRows, err := db.QueryContext(ctx, finalQuery)
	if err != nil {
		// Fall back to the original query if the rewrite is unsupported
		// (e.g. the spatial extension is unavailable).
		if finalQuery != query {
			sqlRows, err = db.QueryContext(ctx, query)
			if err != nil {
				return nil, nil, nil, err
			}
		} else {
			return nil, nil, nil, err
		}
	}
	defer sqlRows.Close()

	colNames, err := sqlRows.Columns()
	if err != nil {
		return nil, nil, nil, err
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
		if err := sqlRows.Scan(vals...); err != nil {
			return nil, nil, nil, err
		}
		row := make(table.Row, len(colNames))
		for i := range colNames {
			row[i] = stringifyValue(*vals[i].(*interface{}))
		}
		rows = append(rows, row)
	}

	return columns, rows, geoCols, nil
}
