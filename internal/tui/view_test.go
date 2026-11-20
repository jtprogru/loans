package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFullWindowLayout(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Model)
	}{
		{"portfolio", func(m *Model) { m.rows = []listRow{{text: "Мария · 界"}} }},
		{"card", func(m *Model) { m.screen = cardScreen }},
		{"rates", func(m *Model) { m.screen = rateScreen }},
		{"form", func(m *Model) { m.debtorForm(nil) }},
		{"confirmation", func(m *Model) { m.confirm = &confirmation{title: "Удалить карточку?"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(t.Context(), nil, nil, func() string { return "2025-01-01" })
			tc.setup(m)
			for _, size := range []tea.WindowSizeMsg{{Width: 120, Height: 32}, {Width: 30, Height: 12}, {Width: 86, Height: 20}} {
				m.Update(size)
				v := m.View()
				lines := strings.Split(v.Content, "\n")
				if !v.AltScreen || len(lines) != size.Height {
					t.Fatalf("%+v: got %d rows, AltScreen=%v", size, len(lines), v.AltScreen)
				}
				for i, line := range lines {
					if width := ansi.StringWidth(line); width != size.Width {
						t.Fatalf("line %d: width %d, want %d", i, width, size.Width)
					}
				}
				if strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
					t.Fatal("missing footer at bottom")
				}
			}
		})
	}
}

func TestSmallWindowProtectsConfirmation(t *testing.T) {
	var deleted bool
	m := &Model{confirm: &confirmation{run: func() error { deleted = true; return nil }}}
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 5})
	if !strings.Contains(m.View().Content, "Увеличьте окно") {
		t.Fatal("missing small window hint")
	}
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if deleted || m.confirm == nil {
		t.Fatal("hidden confirmation was accepted")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Ctrl+C must still quit")
	}
}
