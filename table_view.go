package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var baseStyle = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240"))

func (m *mainModel) setupTableView(cols []table.Column, rows []table.Row) {
	totalWidth := 0
	for _, c := range cols {
		totalWidth += c.Width + 3
	}
	totalWidth += 2

	if totalWidth < m.width {
		totalWidth = m.width
	}

	m.table.SetColumns(cols)
	m.table.SetRows(rows)
	m.table.SetWidth(totalWidth)

	m.viewport.SetXOffset(0)
	m.viewport.SetContent(baseStyle.Render(m.table.View()))
}

func (m *mainModel) updateTableView(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case errorMsg:
		m.errorMsg = string(msg)
		m.loading = false
		return m, nil
	case fetchedDataMsg:
		m.setupTableView(msg.cols, msg.rows)
		m.loading = false
	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyPressMsg:
		if m.loading {
			return m, nil
		}

		switch {
		// New: Pressing Enter switches to Row Detail view
		case key.Matches(msg, m.keys.Select):
			if len(m.table.Rows()) > 0 {
				m.setupDetailView(m.table.Columns(), m.table.SelectedRow())
				m.state = detailView
				return m, nil
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			if !m.isCustomQuery {
				m.state = listView
				m.query = ""
				return m, m.loadTables
			}
			// When started with --query there's nowhere to go back to, so esc quits.
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
		case key.Matches(msg, m.keys.Right):
			m.viewport.ScrollRight(5)
			return m, nil
		case key.Matches(msg, m.keys.Left):
			m.viewport.ScrollLeft(5)
			return m, nil
		case key.Matches(msg, m.keys.Schema):
			schema, err := fetchTableSchema(m.db, m.query)
			if err != nil {
				m.statusMsg = fmt.Sprintf("Describe failed: %s", err)
				return m, nil
			}
			m.statusMsg = "Schema copied to clipboard!"
			return m, tea.SetClipboard(schema)
		case key.Matches(msg, m.keys.EditQuery):
			m.state = queryEditView
			return m, m.setupQueryEditView()
		case key.Matches(msg, m.keys.GeoJSON):
			m.geoAsJSON = !m.geoAsJSON
			m.loading = true
			m.statusMsg = fmt.Sprintf("Geometry: %s", geoFormatLabel(m.geoAsJSON))
			return m, tea.Batch(setLoading(true), m.fetchData)
		}
	}

	m.table, cmd = m.table.Update(msg)
	m.viewport.SetContent(baseStyle.Render(m.table.View()))

	return m, cmd
}

func (m *mainModel) tableView() string {
	var b strings.Builder
	b.WriteString("Query: " + m.query + "\n")
	if m.loading {
		b.WriteString(fmt.Sprintf("\n  %s Loading query...\n", m.spinner.View()))
	} else {
		b.WriteString(m.viewport.View() + "\n")
	}
	if m.statusMsg != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render(m.statusMsg) + "  ")
	}
	b.WriteString(m.help.View(m))
	return b.String()
}

// geoFormatLabel returns a short label for the current geometry display format.
func geoFormatLabel(asJSON bool) string {
	if asJSON {
		return "GeoJSON text"
	}
	return "WKT"
}

func (m *mainModel) ShortHelp() []key.Binding {
	// In table view, 'q' edits the query, so show ctrl+c as quit instead.
	quitHelp := key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit"))
	// esc goes back to the list view, or quits when started with --query.
	backDesc := "back"
	if m.isCustomQuery {
		backDesc = "quit"
	}
	escHelp := key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", backDesc))
	return []key.Binding{m.keys.Select, m.keys.Left, m.keys.Right, m.keys.Schema, m.keys.EditQuery, m.keys.GeoJSON, escHelp, m.keys.Help, quitHelp}
}

func (m mainModel) FullHelp() [][]key.Binding {
	quitHelp := key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit"))
	backDesc := "back"
	if m.isCustomQuery {
		backDesc = "quit"
	}
	escHelp := key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", backDesc))
	return [][]key.Binding{
		{m.table.KeyMap.LineUp, m.table.KeyMap.LineDown, m.keys.Left, m.keys.Right},
		{m.keys.Select, m.keys.Schema, m.keys.EditQuery, m.keys.GeoJSON, escHelp, quitHelp, m.keys.Help},
	}
}
