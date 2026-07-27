package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

func (m *mainModel) setupListView(tables []string) {
	items := make([]list.Item, len(tables))
	for i, t := range tables {
		items[i] = listItem(t)
	}
	m.list.SetItems(items)
}

func (m *mainModel) updateListView(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case fetchedTablesMsg:
		m.setupListView(msg)

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Select):
			if i, ok := m.list.SelectedItem().(listItem); ok {
				selected := string(i)

				// Intercept our quack workaround placeholder
				if strings.Contains(selected, "<Hidden Remote Tables>") {
					// Use the built-in Bubble Tea list status message!
					return m, m.list.NewStatusMessage("Cannot browse hidden tables directly. Use --query flag.")
				}

				m.query = fmt.Sprintf("SELECT * FROM %s LIMIT 1000;", selected)
				m.state = tableView
				return m, m.fetchData
			}
		}
	}

	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *mainModel) listView() string {
	return m.list.View()
}

type listItem string

func (i listItem) FilterValue() string { return string(i) }
func (i listItem) Title() string       { return string(i) }
func (i listItem) Description() string { return "Press Enter to view data" }
