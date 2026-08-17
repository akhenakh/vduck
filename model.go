package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type state int

const (
	listView state = iota
	tableView
	detailView    // Row details
	queryEditView // Edit the current query
)

type keyMap struct {
	Quit      key.Binding
	Select    key.Binding
	Schema    key.Binding // Describe current table and copy schema to clipboard
	EditQuery key.Binding // Edit the current query
	GeoJSON   key.Binding // Toggle geo display as JSON text
	Back      key.Binding
	Esc       key.Binding // Escape only (used where backspace has another meaning, e.g. the query editor)
	Help      key.Binding
	Right     key.Binding
	Left      key.Binding
	Copy      key.Binding // New Copy key
	Next      key.Binding
	Prev      key.Binding
}

var defaultKeys = keyMap{
	Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Select:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Schema:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "copy schema")),
	EditQuery: key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "edit query")),
	GeoJSON:   key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "switch geo format")),
	Back:      key.NewBinding(key.WithKeys("esc", "backspace"), key.WithHelp("esc", "back")),
	Esc:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Right:     key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "scroll right")),
	Left:      key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "scroll left")),
	Copy:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy (OSC 52)")),
	Next:      key.NewBinding(key.WithKeys("n", "j"), key.WithHelp("n", "next row")),
	Prev:      key.NewBinding(key.WithKeys("p", "k"), key.WithHelp("p", "prev row")),
}

type mainModel struct {
	state          state
	db             *sql.DB
	list           list.Model
	table          table.Model
	viewport       viewport.Model
	detailViewport viewport.Model // Viewport for the row detail screen
	textInput      textinput.Model
	help           help.Model
	spinner        spinner.Model
	keys           keyMap
	width          int
	height         int
	query          string
	errorMsg       string
	statusMsg      string // For "Copied to clipboard!" messages
	selectedRowTxt string // Holds the raw text to be copied
	isCustomQuery  bool
	geoAsJSON      bool // Display geometry columns as GeoJSON text
	loading        bool // True while fetching data/tables
	queryTimeout   time.Duration
	initCmd        tea.Cmd
}

type (
	fetchedTablesMsg []string
	fetchedDataMsg   struct {
		cols []table.Column
		rows []table.Row
	}
	errorMsg string
)

func newEmptyTable() table.Model {
	t := table.New(table.WithFocused(true))
	s := table.DefaultStyles()
	s.Header = s.Header.Bold(true)
	t.SetStyles(s)
	return t
}

func newEmptyList() list.Model {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "DuckDB Tables & Views"
	l.SetShowHelp(false)
	return l
}

func newEmptyViewport() viewport.Model {
	vp := viewport.New(viewport.WithWidth(0), viewport.WithHeight(0))
	vp.SoftWrap = false
	return vp
}

func newQueryInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "Query: "
	ti.Placeholder = "SELECT * FROM ..."
	ti.CharLimit = 0
	ti.SetWidth(0)
	return ti
}

func newModel(db *sql.DB, queryTimeout time.Duration) tea.Model {
	m := &mainModel{
		state:          listView,
		db:             db,
		list:           newEmptyList(),
		table:          newEmptyTable(),
		viewport:       newEmptyViewport(),
		detailViewport: newEmptyViewport(), // Initialize detail viewport
		textInput:      newQueryInput(),
		help:           help.New(),
		spinner:        spinner.New(),
		keys:           defaultKeys,
		geoAsJSON:      true,
		queryTimeout:   queryTimeout,
	}
	m.spinner.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("69"))
	m.spinner.Spinner = spinner.Dot
	m.initCmd = m.loadTables
	return m
}

func newModelWithQuery(db *sql.DB, query string, queryTimeout time.Duration) tea.Model {
	m := &mainModel{
		state:          tableView,
		db:             db,
		list:           newEmptyList(),
		table:          newEmptyTable(),
		viewport:       newEmptyViewport(),
		detailViewport: newEmptyViewport(), // Initialize detail viewport
		textInput:      newQueryInput(),
		help:           help.New(),
		spinner:        spinner.New(),
		keys:           defaultKeys,
		query:          query,
		isCustomQuery:  true,
		geoAsJSON:      true,
		loading:        true,
		queryTimeout:   queryTimeout,
	}
	m.spinner.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("69"))
	m.spinner.Spinner = spinner.Dot
	m.initCmd = m.fetchData
	return m
}

// queryCtx returns a context that bounds how long a single database operation
// (table list, data fetch, describe) may run before being cancelled.
func (m *mainModel) queryCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), m.queryTimeout)
}

func (m *mainModel) loadTables() tea.Msg {
	ctx, cancel := m.queryCtx()
	defer cancel()
	tables, err := fetchTablesAndViews(ctx, m.db)
	if err != nil {
		return errorMsg(err.Error())
	}
	return fetchedTablesMsg(tables)
}

func (m *mainModel) fetchData() tea.Msg {
	ctx, cancel := m.queryCtx()
	defer cancel()
	cols, rows, err := fetchTableData(ctx, m.db, m.query, m.geoAsJSON)
	if err != nil {
		return errorMsg(err.Error())
	}
	return fetchedDataMsg{cols: cols, rows: rows}
}

func (m *mainModel) Init() tea.Cmd {
	return tea.Batch(m.initCmd, m.spinner.Tick)
}

func (m *mainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// Clear the status message on any keystroke so it doesn't linger forever
	if _, ok := msg.(tea.KeyPressMsg); ok {
		m.statusMsg = ""
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.SetWidth(msg.Width)
		m.list.SetSize(msg.Width, msg.Height-1)

		vpHeight := msg.Height - 5
		if vpHeight < 1 {
			vpHeight = 1
		}

		m.viewport.SetWidth(msg.Width)
		m.viewport.SetHeight(vpHeight)

		m.detailViewport.SetWidth(msg.Width)
		m.detailViewport.SetHeight(vpHeight)

		m.textInput.SetWidth(msg.Width)

		tableHeight := msg.Height - 7
		if tableHeight < 1 {
			tableHeight = 1
		}
		m.table.SetHeight(tableHeight)

		if m.state == tableView {
			m.viewport.SetContent(baseStyle.Render(m.table.View()))
		}
		if m.state == detailView {
			m.detailViewport.SetContent(baseStyle.Render(m.selectedRowTxt))
		}

	case tea.KeyPressMsg:
		if key.Matches(msg, m.keys.Quit) {
			// In table and query-edit views, 'q' is repurposed to edit the query,
			// unless we're showing an error where 'q' should always quit.
			if !(key.Matches(msg, m.keys.EditQuery) && m.errorMsg == "" && (m.state == tableView || m.state == queryEditView)) {
				return m, tea.Quit
			}
		}
		if m.errorMsg != "" {
			if key.Matches(msg, m.keys.Back) {
				m.errorMsg = ""
				if m.isCustomQuery {
					return m, tea.Quit
				}
				m.state = listView
				return m, m.loadTables
			}
			return m, nil
		}
	}

	switch m.state {
	case listView:
		return m.updateListView(msg)
	case tableView:
		return m.updateTableView(msg)
	case detailView:
		return m.updateDetailView(msg) // Route to new view
	case queryEditView:
		return m.updateQueryEditView(msg)
	}

	return m, cmd
}

func (m *mainModel) View() tea.View {
	var content string

	if m.errorMsg != "" {
		content = fmt.Sprintf("Database Error:\n\n%s\n\nPress 'esc' to go back, or 'q' to quit.", m.errorMsg)
	} else {
		switch m.state {
		case listView:
			content = m.listView()
		case tableView:
			content = m.tableView()
		case detailView:
			content = m.detailView()
		case queryEditView:
			content = m.queryEditView()
		}
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}
