# vduck

A simple terminal UI for DuckDB, designed to work seamlessly with Quack remote servers.

vduck is a small Bubble Tea TUI that lets you browse tables/views, run queries, and inspect rows from your terminal, just point it at a database (local or remote Quack server).

![avif](img/vduck.avif)

## Features

- Browse tables and views across catalogs
- Open single-file data sources (Parquet, CSV, JSON, Vortex) as browsable views
- Run custom queries
- Auto-decode WKB geometry columns (`geo`, `geom`, `geometry`, `wkb`) to WKT
- Map view (`m`) to render the selected row's geometry on a slippy map (requires a Kitty graphics terminal)
- Vertical row detail view with `n`/`p` navigation between rows
- OSC 52 clipboard copy of the selected row
- One-step Quack server attachment via flags

## Usage

Local DuckDB file:

```sh
vduck -db ./my.db
```

Open a Parquet/CSV/JSON/Vortex file directly:

```sh
vduck -db ./data.parquet
vduck -db ./data.csv
vduck -db ./data.json
vduck -db ./data.vortex
```

The file is exposed as a view named after the file, so it appears in the table list and can be browsed with `Enter`.

Run a one-shot query:

```sh
vduck -db ./my.db -query "SELECT * FROM users LIMIT 10"
# or positionally:
vduck -db ./my.db "SELECT * FROM users LIMIT 10"
```

Connect to a Quack remote server:

```sh
vduck -h data-stage.example.com:443 -t QUACK_TOKEN_HERE
# token can also be read from the QUACK_TOKEN env var:
QUACK_TOKEN=... vduck -h data-stage.example.com:443
```

> **Note:** Quack remote catalogs currently do not expose their table metadata to client introspection. Attached Quack databases appear as `<Hidden Remote Tables>` in the list; query them directly with `-query` or use `-init` to create local views over remote tables.

Provide extra init SQL alongside Quack:

```sh
vduck -h data-stage.example.com:443 -init "SET threads TO 4;"
```

### Flags

| Flag     | Description                                         | Default     |
| -------- | --------------------------------------------------- | ----------- |
| `-db`    | Path to the DuckDB database file or data file (Parquet/CSV/JSON/Vortex) | `:memory:`  |
| `-init`  | Initialization SQL to run on startup               | `""`        |
| `-query` | Execute a query and show results                    | `""`        |
| `-h`     | Quack hostname (`host[:port]`)                      | `""`        |
| `-t`     | Quack token (or `QUACK_TOKEN` env var)              | `""`        |

### Keys

| Key          | Action                          |
| ------------ | ------------------------------- |
| `enter`      | Select / open row detail        |
| `n` / `p`    | Next / previous row in detail   |
| `c`          | Copy row (OSC 52)               |
| `s`          | Copy schema (OSC 52)            |
| `a`          | Toggle geometry display (GeoJSON text / WKT) |
| `m`          | Show the selected row's geometry on a map (table/detail views, `esc` to close) |
| `q`          | Edit query (from table view)    |
| `esc`        | Back (or quit when no previous view) |
| `?`          | Help                            |
| `q` / `^c`   | Quit (list / detail views)      |
| `^c`         | Quit (table / edit views)       |

## License

MIT
