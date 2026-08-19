package main

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/akhenakh/tiletea"
)

var mapHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

// kittyDeleteAll is the Kitty graphics protocol command that deletes all
// currently placed images. It is emitted when leaving the map view so the last
// rendered map doesn't linger behind the table/detail views.
const kittyDeleteAll = "\x1b_Ga=d\x1b\\"

type mapReadyMsg struct {
	gv  *tiletea.GeomView
	err error
}

// openMapView returns a command that builds the map component for the geometry
// of the currently selected row. Building happens off the update loop because
// it fetches the map style/tile source over the network on first use.
func (m *mainModel) openMapView() tea.Cmd {
	geoIdx := m.geoCols[0]
	row := m.table.SelectedRow()
	geomText := row[geoIdx]
	asJSON := m.geoAsJSON

	return func() tea.Msg {
		var gv *tiletea.GeomView
		var err error
		if asJSON {
			gv, err = tiletea.NewGeomViewFromGeoJSON([]byte(geomText), tiletea.WithAltScreen(false))
		} else {
			gv, err = tiletea.NewGeomViewFromWKT(geomText, tiletea.WithAltScreen(false))
		}
		return mapReadyMsg{gv: gv, err: err}
	}
}

// enterMapView validates that the selected row has a mappable geometry and
// switches to the map view, kicking off the asynchronous map construction.
func (m *mainModel) enterMapView() (tea.Model, tea.Cmd) {
	if len(m.geoCols) == 0 {
		return m, nil
	}
	row := m.table.SelectedRow()
	geoIdx := m.geoCols[0]
	if len(row) == 0 || geoIdx >= len(row) {
		return m, nil
	}
	geomText := row[geoIdx]
	if geomText == "" || geomText == "NULL" {
		m.statusMsg = "Selected row has no geometry"
		return m, nil
	}

	m.prevState = m.state
	m.state = mapView
	m.geomView = nil
	m.mapLoading = true
	return m, m.openMapView()
}

// mapAreaHeight returns the height to give the map component, reserving one row
// for the hint line rendered above it.
func mapAreaHeight(h int) int {
	if h < 2 {
		return 1
	}
	return h - 1
}

func (m *mainModel) updateMapView(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case mapReadyMsg:
		if msg.err != nil {
			m.mapLoading = false
			m.geomView = nil
			m.statusMsg = fmt.Sprintf("Cannot display geometry: %s", msg.err)
			m.state = m.prevState
			return m, nil
		}
		m.geomView = msg.gv
		m.mapLoading = false
		// Give the map its size right away so it renders on the first frame.
		mv, c := m.geomView.Update(tea.WindowSizeMsg{Width: m.width, Height: mapAreaHeight(m.height)})
		if gv, ok := mv.(*tiletea.GeomView); ok {
			m.geomView = gv
		}
		return m, c

	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyPressMsg:
		if key.Matches(msg, m.keys.Esc) {
			m.geomView = nil
			m.mapLoading = false
			m.state = m.prevState
			// Clear the last map image so it doesn't show through the view we
			// return to. Emitted raw so it bypasses the renderer, which would
			// otherwise drop the zero-width APC sequence.
			return m, tea.Raw(kittyDeleteAll)
		}

	case tea.WindowSizeMsg:
		if m.geomView != nil {
			mv, c := m.geomView.Update(tea.WindowSizeMsg{Width: msg.Width, Height: mapAreaHeight(msg.Height)})
			if gv, ok := mv.(*tiletea.GeomView); ok {
				m.geomView = gv
			}
			return m, c
		}
		return m, nil
	}

	// Forward everything else (pan/zoom keys, render results) to the map.
	if m.geomView != nil {
		mv, c := m.geomView.Update(msg)
		if gv, ok := mv.(*tiletea.GeomView); ok {
			m.geomView = gv
		}
		return m, c
	}
	return m, cmd
}

func (m *mainModel) mapViewScreen() string {
	if m.mapLoading || m.geomView == nil {
		return fmt.Sprintf("\n  %s Loading map...\n", m.spinner.View())
	}
	// The hint must come before the map's content: the renderer drops any text
	// that follows the map's zero-width Kitty graphics APC sequence.
	return mapHintStyle.Render("esc close | arrows/hjkl pan | +/- zoom | q quit") + "\n" + m.geomView.View().Content
}
