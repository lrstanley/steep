// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command docs-demos is the tea shop, the small Bubble Tea program the
// documentation at https://getsteep.dev demonstrates steep and reel with:
// a menu of teas to move through with the keys or the mouse, a filter, and
// a brew that reports its progress to the terminal and ends with a desktop
// notification.
//
// Its tests write every recording, snapshot, and failure the site shows, when
// exporting is turned on (see main_test.go).
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// License is a fake secret the shop shows, for the redaction demos.
const License = "tea-8f3a91c2d7"

const (
	brewStep  = 60 * time.Millisecond
	brewSteps = 20
)

type menuItem struct{ name, kind, steep string }

var menu = []menuItem{
	{"Sencha", "green", "1m"},
	{"Matcha", "green", "0m"},
	{"Gyokuro", "green", "2m"},
	{"Oolong", "oolong", "3m"},
	{"Assam", "black", "4m"},
	{"Darjeeling", "black", "3m"},
	{"Pu-erh", "dark", "5m"},
}

var (
	matcha = lipgloss.Color("#a6d86f")
	deep   = lipgloss.Color("#65a04c")
	muted  = lipgloss.Color("#7d957a")
	cream  = lipgloss.Color("#d6e6cb")
	amber  = lipgloss.Color("#e3b86b")

	titleStyle    = lipgloss.NewStyle().Foreground(matcha).Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#0b150f")).Background(matcha).Bold(true)
	rowStyle      = lipgloss.NewStyle().Foreground(cream)
	mutedStyle    = lipgloss.NewStyle().Foreground(muted)
	amberStyle    = lipgloss.NewStyle().Foreground(amber)
	boxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(deep).Padding(0, 1)
)

type brewTick struct{}

func tick() tea.Cmd { return tea.Tick(brewStep, func(time.Time) tea.Msg { return brewTick{} }) }

// Model is the tea shop.
type Model struct {
	width     int
	cursor    int
	filter    string
	filtering bool
	brewing   int    // Steps of the brew so far, 0 when idle.
	brew      string // The tea being brewed, chosen when it started.
	Served    []string
}

// NewModel returns the shop, with its first tea selected.
func NewModel() *Model { return &Model{width: 72} }

func (m *Model) visible() []menuItem {
	var out []menuItem
	for _, t := range menu {
		if strings.Contains(strings.ToLower(t.name+" "+t.kind), strings.ToLower(m.filter)) {
			out = append(out, t)
		}
	}
	return out
}

// Init implements [tea.Model].
func (m *Model) Init() tea.Cmd { return nil }

// Update implements [tea.Model].
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case brewTick:
		return m, m.stepBrew()
	case tea.MouseClickMsg:
		// The menu's first row is the fourth of the screen, under the box's
		// border, the title, and a blank row.
		if row := msg.Y - 3; row >= 0 && row < len(m.visible()) {
			m.cursor = row
		}
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelDown {
			m.move(1)
		} else {
			m.move(-1)
		}
	case tea.KeyPressMsg:
		return m, m.key(msg)
	}
	return m, nil
}

func (m *Model) move(by int) {
	m.cursor = max(0, min(m.cursor+by, len(m.visible())-1))
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	if m.filtering {
		switch msg.String() {
		case "esc":
			m.filter, m.filtering = "", false
		case "enter":
			m.filtering = false
		case "backspace":
			m.filter = strings.TrimSuffix(m.filter, m.filter[max(0, len(m.filter)-1):])
		default:
			if msg.Text != "" {
				m.filter += msg.Text
			}
		}
		m.cursor = 0
		return nil
	}
	switch msg.String() {
	case "q", "ctrl+c":
		return tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "/":
		m.filtering = true
	case "enter":
		if m.brewing == 0 && len(m.visible()) > 0 {
			m.brewing = 1
			m.brew = m.visible()[m.cursor].name
			return tick()
		}
	}
	return nil
}

func (m *Model) stepBrew() tea.Cmd {
	m.brewing++
	if m.brewing <= brewSteps {
		return tick()
	}
	m.brewing = 0
	m.Served = append(m.Served, m.brew)
	return tea.Raw(ansi.Notify(m.brew + " is ready"))
}

// View implements [tea.Model].
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "tea shop"
	if m.brewing > 0 {
		v.ProgressBar = tea.NewProgressBar(tea.ProgressBarDefault, m.brewing*100/brewSteps)
	}
	return v
}

func (m *Model) render() string {
	inner := max(m.width-4, 20)
	rows := []string{titleStyle.Render("The Tea Shop"), ""}
	teas := m.visible()
	for i, t := range teas {
		row := fmt.Sprintf(" %-*s %-8s %s ", max(inner-14, 8), t.name, t.kind, t.steep)
		if i == m.cursor {
			rows = append(rows, selectedStyle.Render(row))
		} else {
			rows = append(rows, rowStyle.Render(row))
		}
	}
	if len(teas) == 0 {
		rows = append(rows, mutedStyle.Render(" No tea matches."))
	}
	rows = append(rows, "", m.status(inner), mutedStyle.Render("license "+License))
	help := "↑/↓ choose  / filter  enter brew  q quit"
	return boxStyle.Width(m.width).Render(strings.Join(rows, "\n")) + "\n " + mutedStyle.Render(help)
}

func (m *Model) status(width int) string {
	switch {
	case m.brewing > 0:
		bar := max(width-8, 4)
		done := bar * m.brewing / brewSteps
		return amberStyle.Render("Brewing ") + titleStyle.Render(strings.Repeat("█", done)) + mutedStyle.Render(strings.Repeat("░", bar-done))
	case m.filtering || m.filter != "":
		return amberStyle.Render("Filter: ") + rowStyle.Render(m.filter)
	case len(m.Served) > 0:
		return titleStyle.Render(m.Served[len(m.Served)-1] + " is ready")
	}
	return mutedStyle.Render("Pick a tea.")
}

func main() {
	if _, err := tea.NewProgram(NewModel()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
