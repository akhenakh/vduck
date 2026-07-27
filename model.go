package main

import (
	"database/sql"
	"fmt"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

type state int

const (
	listView state = iota
	tableView
	detailView // New state for row details
)

type keyMap struct {
	Quit   key.Binding
	Select key.Binding
	Back   key.Binding
	Help   key.Binding
	Right  key.Binding
	Left   key.Binding
	Copy   key.Binding // New Copy key
	Next   key.Binding
	Prev   key.Binding
}

var defaultKeys = keyMap{
	Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Back:   key.NewBinding(key.WithKeys("esc", "backspace"), key.WithHelp("esc", "back")),
	Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Right:  key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "scroll right")),
	Left:   key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "scroll left")),
	Copy:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy (OSC 52)")),
	Next:   key.NewBinding(key.WithKeys("n", "j"), key.WithHelp("n", "next row")),
	Prev:   key.NewBinding(key.WithKeys("p", "k"), key.WithHelp("p", "prev row")),
}

type mainModel struct {
	state          state
	db             *sql.DB
	list           list.Model
	table          table.Model
	viewport       viewport.Model
	detailViewport viewport.Model // Viewport for the row detail screen
	help           help.Model
	keys           keyMap
	width          int
	height         int
	query          string
	errorMsg       string
	statusMsg      string // For "Copied to clipboard!" messages
	selectedRowTxt string // Holds the raw text to be copied
	isCustomQuery  bool
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

func newModel(db *sql.DB) tea.Model {
	m := &mainModel{
		state:          listView,
		db:             db,
		list:           newEmptyList(),
		table:          newEmptyTable(),
		viewport:       newEmptyViewport(),
		detailViewport: newEmptyViewport(), // Initialize detail viewport
		help:           help.New(),
		keys:           defaultKeys,
	}
	m.initCmd = m.loadTables
	return m
}

func newModelWithQuery(db *sql.DB, query string) tea.Model {
	m := &mainModel{
		state:          tableView,
		db:             db,
		list:           newEmptyList(),
		table:          newEmptyTable(),
		viewport:       newEmptyViewport(),
		detailViewport: newEmptyViewport(), // Initialize detail viewport
		help:           help.New(),
		keys:           defaultKeys,
		query:          query,
		isCustomQuery:  true,
	}
	m.initCmd = m.fetchData
	return m
}

func (m *mainModel) loadTables() tea.Msg {
	tables, err := fetchTablesAndViews(m.db)
	if err != nil {
		return errorMsg(err.Error())
	}
	return fetchedTablesMsg(tables)
}

func (m *mainModel) fetchData() tea.Msg {
	cols, rows, err := fetchTableData(m.db, m.query)
	if err != nil {
		return errorMsg(err.Error())
	}
	return fetchedDataMsg{cols: cols, rows: rows}
}

func (m *mainModel) Init() tea.Cmd {
	return m.initCmd
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
			return m, tea.Quit
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
		}
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}
