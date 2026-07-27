package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (m *mainModel) setupDetailView(cols []table.Column, row table.Row) {
	m.refreshDetailView()
}

// refreshDetailView re-renders the detail viewport using the table's
// currently selected row. Used by setupDetailView and the n/p navigation keys.
func (m *mainModel) refreshDetailView() {
	cols := m.table.Columns()
	row := m.table.SelectedRow()

	var b strings.Builder
	maxColLen := 0
	for _, c := range cols {
		if len(c.Title) > maxColLen {
			maxColLen = len(c.Title)
		}
	}
	for i, c := range cols {
		b.WriteString(fmt.Sprintf("%-*s : %s\n", maxColLen, c.Title, row[i]))
	}
	m.selectedRowTxt = b.String()

	m.detailViewport.SetContent(baseStyle.Render(m.selectedRowTxt))
	m.detailViewport.GotoTop()
	m.detailViewport.SetXOffset(0)
}

func (m *mainModel) updateDetailView(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Back):
			m.state = tableView
			return m, nil
		case key.Matches(msg, m.keys.Copy):
			// Set the status message and trigger the OSC 52 clipboard command
			m.statusMsg = "Row copied to clipboard!"
			return m, tea.SetClipboard(m.selectedRowTxt)
		case key.Matches(msg, m.keys.Next):
			m.table.MoveDown(1)
			m.refreshDetailView()
			return m, nil
		case key.Matches(msg, m.keys.Prev):
			m.table.MoveUp(1)
			m.refreshDetailView()
			return m, nil
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
		}
	}

	// Bubble Tea's viewport natively handles Up/Down/Left/Right/PageUp/PageDown!
	m.detailViewport, cmd = m.detailViewport.Update(msg)
	return m, cmd
}

func (m *mainModel) detailView() string {
	var b strings.Builder
	b.WriteString("Row Details:\n")
	b.WriteString(m.detailViewport.View() + "\n")

	// Show a success message if a copy just occurred
	if m.statusMsg != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render(m.statusMsg) + "  ")
	}

	b.WriteString(m.help.View(detailHelp{m}))
	return b.String()
}

// Custom help interface wrapper for the detail view
type detailHelp struct {
	m *mainModel
}

func (d detailHelp) ShortHelp() []key.Binding {
	return []key.Binding{
		d.m.keys.Next,
		d.m.keys.Prev,
		d.m.keys.Copy,
		d.m.keys.Back,
		d.m.keys.Help,
		d.m.keys.Quit,
	}
}

func (d detailHelp) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{d.m.keys.Next, d.m.keys.Prev},
		{d.m.keys.Copy, d.m.keys.Back},
		{d.m.keys.Help, d.m.keys.Quit},
	}
}
