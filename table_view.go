package main

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var baseStyle = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240"))

func (m *mainModel) setupTableView(cols []table.Column, rows []table.Row) {
	// Calculate how wide the table ACTUALLY needs to be to fit all columns
	totalWidth := 0
	for _, c := range cols {
		totalWidth += c.Width + 3 // Account for column padding and separators
	}
	totalWidth += 2 // Add space for the outer table borders

	// Ensure it's at least the width of the terminal so it doesn't look cut off
	if totalWidth < m.width {
		totalWidth = m.width
	}

	m.table.SetColumns(cols)
	m.table.SetRows(rows)
	m.table.SetWidth(totalWidth) // Tell the table to render fully without truncating

	m.viewport.SetXOffset(0) // Reset horizontal scroll for a new query
	m.viewport.SetContent(baseStyle.Render(m.table.View()))
}

func (m *mainModel) updateTableView(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case errorMsg:
		m.errorMsg = string(msg)
		return m, nil
	case fetchedDataMsg:
		m.setupTableView(msg.cols, msg.rows)
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Back):
			if !m.isCustomQuery {
				m.state = listView
				m.query = ""
				return m, m.loadTables
			}
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
		case key.Matches(msg, m.keys.Right):
			// Scroll viewport right by 5 characters
			m.viewport.ScrollRight(5)
			return m, nil
		case key.Matches(msg, m.keys.Left):
			// Scroll viewport left by 5 characters
			m.viewport.ScrollLeft(5)
			return m, nil
		}
	}

	// Route standard keys (Up/Down) to the table component
	m.table, cmd = m.table.Update(msg)

	// Sync the updated table cursor back into the viewport content
	m.viewport.SetContent(baseStyle.Render(m.table.View()))

	return m, cmd
}

func (m *mainModel) tableView() string {
	var b strings.Builder
	b.WriteString("Query: " + m.query + "\n")

	// Notice we render the viewport here instead of the table directly!
	b.WriteString(m.viewport.View() + "\n")

	b.WriteString(m.help.View(m))
	return b.String()
}

// Help key bindings for the table view
func (m mainModel) ShortHelp() []key.Binding {
	return []key.Binding{
		m.table.KeyMap.LineUp,
		m.table.KeyMap.LineDown,
		m.keys.Left, // Add Left/Right to help menu
		m.keys.Right,
		m.keys.Back,
		m.keys.Quit,
		m.keys.Help,
	}
}

func (m mainModel) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{m.table.KeyMap.LineUp, m.table.KeyMap.LineDown, m.keys.Left, m.keys.Right},
		{m.table.KeyMap.PageUp, m.table.KeyMap.PageDown, m.table.KeyMap.GotoTop, m.table.KeyMap.GotoBottom},
		{m.keys.Back, m.keys.Quit},
	}
}
