package tui

import (
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/jtprogru/loans/internal/ledger"
	"github.com/jtprogru/loans/internal/store"
)

const testDay = "2025-02-01"

func workspace(t *testing.T) *Model {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "loans.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	m := New(t.Context(), db, slog.New(slog.DiscardHandler), func() string { return testDay })
	m.Update(m.Init()())
	return m
}
func press(m *Model, code rune, mods ...tea.KeyMod) {
	k := tea.KeyPressMsg{Code: code}
	if len(mods) > 0 {
		k.Mod = mods[0]
	} else if code >= 32 && code <= 126 {
		k.Text = string(code)
	}
	m.Update(k)
}
func paste(m *Model, value string)        { m.Update(tea.PasteMsg{Content: value}) }
func replaceInput(m *Model, value string) { press(m, 'u', tea.ModCtrl); paste(m, value) }
func see(t *testing.T, m *Model, words ...string) {
	t.Helper()
	s := ansi.Strip(m.View().Content)
	for _, word := range words {
		if !strings.Contains(s, word) {
			t.Fatalf("missing %q:\n%s", word, s)
		}
	}
}

func TestDebtorPeriodFollowsRate(t *testing.T) {
	for _, tc := range []struct{ rate, period, mode, stored string }{{"", "", ledger.ImplicitFree, ""}, {"key-rate", "", ledger.ExplicitKey, ""}, {"0", "", ledger.Fixed, "year"}, {"12,5", "21d", ledger.Fixed, "21d"}} {
		t.Run(tc.mode+tc.rate, func(t *testing.T) {
			m := workspace(t)
			press(m, 'n')
			paste(m, "Анна")
			press(m, tea.KeyTab)
			paste(m, "1000")
			press(m, tea.KeyTab)
			press(m, tea.KeyTab)
			// Enter a period first, then change the rate like a user correcting the form.
			paste(m, "10")
			press(m, tea.KeyTab)
			paste(m, "1m")
			press(m, tea.KeyTab, tea.ModShift)
			replaceInput(m, tc.rate)
			if got := strings.Contains(m.View().Content, "Период"); got != (tc.mode == ledger.Fixed) {
				t.Fatalf("rate %q: period visible = %t", tc.rate, got)
			}
			press(m, tea.KeyTab)
			if tc.mode == ledger.Fixed {
				replaceInput(m, tc.period)
				press(m, tea.KeyTab)
			}
			see(t, m, "› Очередность")
			press(m, tea.KeyEnter)
			if m.form != nil || m.screen != cardScreen || m.d.Mode != tc.mode || m.d.Period != tc.stored {
				t.Fatalf("loan=%+v notice=%s", m.d, m.notice)
			}
		})
	}
}

func TestLongConfirmationCannotBeAcceptedBlind(t *testing.T) {
	m := workspace(t)
	m.Update(tea.WindowSizeMsg{Width: 30, Height: 12})
	called := false
	m.confirm = &confirmation{title: strings.Repeat("Подробности ", 40) + "Удалить все платежи?", run: func() error { called = true; return nil }, onSuccess: func() {}}
	press(m, 'y')
	if called {
		t.Fatal("accepted before consequences were visible")
	}
	press(m, tea.KeyEnd)
	see(t, m, "Удалить все платежи?")
	press(m, 'y')
	if !called {
		t.Fatal("confirmation did not execute")
	}
}
