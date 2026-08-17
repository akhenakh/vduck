package main

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func (m *mainModel) updateQueryEditView(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Esc):
			m.state = tableView
			m.textInput.Blur()
			return m, nil

		case key.Matches(msg, m.keys.Select):
			newQuery := strings.TrimSpace(m.textInput.Value())
			if newQuery == "" {
				return m, nil
			}

			m.query = newQuery
			m.isCustomQuery = true
			m.state = tableView
			m.loading = true
			m.textInput.Blur()
			return m, m.fetchData
		}
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m *mainModel) queryEditView() string {
	var b strings.Builder
	b.WriteString("Edit query:\n\n")
	b.WriteString(m.textInput.View())
	b.WriteString("\n\n")
	b.WriteString("Press enter to run, esc to cancel.")

	// Pad to fill the screen so the help/status area doesn't look odd.
	padding := m.height - 6
	if padding > 0 {
		b.WriteString(strings.Repeat("\n", padding))
	}
	return b.String()
}

func (m *mainModel) setupQueryEditView() tea.Cmd {
	m.textInput.SetValue(m.query)
	m.textInput.CursorEnd()
	return m.textInput.Focus()
}
