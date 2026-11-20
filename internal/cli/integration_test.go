package cli_test

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/jtprogru/loans/internal/cli"
	"github.com/jtprogru/loans/internal/store"
	"github.com/jtprogru/loans/internal/tui"
)

func command(t *testing.T, path string, args ...string) (string, error) {
	t.Helper()
	var out, stderr bytes.Buffer
	err := cli.Execute(t.Context(), append([]string{"--db", path}, args...),
		strings.NewReader(""), &out, &stderr, "test")
	if stderr.Len() != 0 {
		t.Fatalf("unexpected command diagnostic: %s", &stderr)
	}
	return out.String(), err
}

func mustRun(t *testing.T, path string, args ...string) string {
	t.Helper()
	out, err := command(t, path, args...)
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return out
}

func text(t *testing.T, out string, words ...string) {
	t.Helper()
	for _, word := range words {
		if !strings.Contains(out, word) {
			t.Fatalf("missing %q in:\n%s", word, out)
		}
	}
}

func TestDefaultDatabaseInHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	want := filepath.Join(home, ".config", "loans", "loans.sqlite3")
	var out bytes.Buffer
	for _, args := range [][]string{{"--help"}, {"debtor", "list"}} {
		if err := cli.Execute(t.Context(), args, strings.NewReader(""), &out, io.Discard, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(out.String(), strconv.Quote(want)) {
		t.Fatalf("default not found in help: %s", out.String())
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}
}

func TestSharedLedgerBetweenCLIAndTUI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loans.sqlite3")
	text(t, mustRun(t, path, "debtor", "add", "--name", "Анна", "--amount", "10000", "--date", "2025-01-01"),
		"#1", "беспроцентный", "сначала проценты")
	if _, err := os.Stat(path + ".log"); err != nil {
		t.Fatal(err)
	}
	text(t, mustRun(t, path, "balance", "1", "--date", "2024-12-31"), "заём ещё не выдан")

	m, closeFirst := startTUI(t, path, "2025-01-10")
	text(t, m.View().Content, "Анна", "итог 10000.00")
	key(m, tea.KeyEnter)
	runes(m, "p")
	text(t, m.View().Content, "Новый платёж")
	runes(m, "4000")
	key(m, tea.KeyEnter)
	text(t, m.View().Content, "Итого к возврату: 6000.00", "платёж #1", "тело 4000.00", "проценты 0.00")
	text(t, mustRun(t, path, "balance", "1"), "Остаток тела: 6000.00", "Итого к возврату: 6000.00")
	text(t, mustRun(t, path, "payment", "list", "1"), "платёж #1", "тело 4000.00")

	runes(m, "d")
	m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	runes(m, "2025-01-09")
	key(m, tea.KeyEnter)
	text(t, m.View().Content, "Дата расчёта: 2025-01-09", "Итого к возврату: 10000.00")
	runes(m, "t")
	text(t, m.View().Content, "Итого к возврату: 6000.00")

	runes(m, "x")
	runes(m, "1")
	key(m, tea.KeyEnter)
	text(t, m.View().Content, "Удалить платёж #1", "4000.00")
	runes(m, "n")
	text(t, m.View().Content, "Итого к возврату: 6000.00")
	runes(m, "x")
	runes(m, "1")
	key(m, tea.KeyEnter)
	runes(m, "y")
	text(t, m.View().Content, "Итого к возврату: 10000.00")
	closeFirst()

	m, closeAgain := startTUI(t, path, "2025-01-10")
	defer closeAgain()
	text(t, m.View().Content, "Анна", "итог 10000.00")
	text(t, mustRun(t, path, "balance", "1"), "Итого к возврату: 10000.00")
	if _, err := command(t, path, "debtor", "delete", "1"); err == nil {
		t.Fatal("deleted without --yes")
	}
	journal, err := os.ReadFile(path + ".log")
	if err != nil {
		t.Fatal(err)
	}
	text(t, string(journal), "loans debtor add", "loans debtor delete", `"level":"ERROR"`)
	if strings.Contains(string(journal), "Анна") || strings.Contains(string(journal), "4000.00") {
		t.Fatal("journal contains personal data or amounts")
	}
}

func TestMissingKeyRateInBothInterfaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loans.sqlite3")
	const (
		today   = "2025-01-10"
		missing = "2025-01-04 — 2025-01-10"
	)
	mustRun(t, path, "key-rate", "set", "--date", "2025-01-02", "--rate", "10", "--source", "тест")
	mustRun(t, path, "key-rate", "coverage", "--from", "2025-01-02", "--through", "2025-01-03")
	mustRun(t, path, "debtor", "add", "--name", "Ирина", "--amount", "70000", "--date", "2025-01-01", "--rate", "key-rate")
	out, err := command(t, path, "balance", "1", "--date", today)
	if out != "" || err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("balance without complete coverage: output %q, error %v", out, err)
	}
	m, closeDB := startTUI(t, path, today)
	defer closeDB()
	text(t, m.View().Content, "Ирина", "итог не рассчитан")
	key(m, tea.KeyEnter)
	text(t, m.View().Content, "Итог не рассчитан", missing)
	if strings.Contains(m.View().Content, "Итого к возврату") {
		t.Fatal("TUI displayed unconfirmed total")
	}
	text(t, mustRun(t, path, "balance", "1", "--date", "2025-01-03"), "Итого к возврату:", "70038.36")
}

func startTUI(t *testing.T, path, today string) (*tui.Model, func()) {
	t.Helper()
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	m := tui.New(t.Context(), db, slog.New(slog.NewTextHandler(io.Discard, nil)), func() string { return today })
	m.Update(m.Init()())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	return m, func() {
		t.Helper()
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func key(m *tui.Model, k rune) { m.Update(tea.KeyPressMsg{Code: k}) }

func runes(m *tui.Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}
