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
		case key.Matches(msg, m.keys.Back):
			if !m.isCustomQuery {
				m.state = listView
				m.query = ""
				return m, m.loadTables
			}
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
		case key.Matches(msg, m.keys.Right):
			m.viewport.ScrollRight(5)
			return m, nil
		case key.Matches(msg, m.keys.Left):
			m.viewport.ScrollLeft(5)
			return m, nil
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
	b.WriteString(m.help.View(m))
	return b.String()
}

func (m mainModel) ShortHelp() []key.Binding {
	return []key.Binding{m.keys.Select, m.keys.Left, m.keys.Right, m.keys.Back, m.keys.Help, m.keys.Quit}
}

func (m mainModel) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{m.table.KeyMap.LineUp, m.table.KeyMap.LineDown, m.keys.Left, m.keys.Right},
		{m.keys.Select, m.keys.Back, m.keys.Quit, m.keys.Help},
	}
}
