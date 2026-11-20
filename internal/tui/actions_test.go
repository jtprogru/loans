package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/jtprogru/loans/internal/ledger"
	"path/filepath"
	"strings"
	"testing"
)

func TestRateBookAndCoverage(t *testing.T) {
	m := workspace(t)
	press(m, 'k')
	see(t, m, "не задан")
	press(m, 's')
	press(m, tea.KeyTab)
	paste(m, "no")
	press(m, tea.KeyEnter)
	see(t, m, "Ошибка:", "ставка")
	replaceInput(m, "7,5")
	press(m, tea.KeyEnter)
	see(t, m, "7.5%", "Значение сохранено")
	press(m, 'r')
	press(m, 'c')
	paste(m, "2024-01-02")
	press(m, tea.KeyEnter)
	see(t, m, "нужно значение")
	replaceInput(m, testDay)
	press(m, tea.KeyEnter)
	see(t, m, "Проверенный интервал: "+testDay)
	press(m, tea.KeyPgDown)
	press(m, tea.KeyEsc)
	see(t, m, "Портфель")
}

func TestBackupRestoreAndSearch(t *testing.T) {
	m := workspace(t)
	path := filepath.Join(t.TempDir(), "копия.json")
	d, err := ledger.NewDebtor("Ирина", "1000", testDay, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.db.AddDebtor(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	press(m, 'r')
	press(m, '/')
	paste(m, "Никто")
	press(m, tea.KeyEnter)
	see(t, m, "Нет карточек по запросу")
	press(m, '/')
	replaceInput(m, "#1")
	press(m, tea.KeyEnter)
	see(t, m, "Ирина")
	press(m, 'e')
	replaceInput(m, "")
	press(m, tea.KeyEnter)
	see(t, m, "укажите путь")
	paste(m, path)
	press(m, tea.KeyEnter)
	see(t, m, "Копия записана")
	press(m, tea.KeyEnter)
	press(m, 'D')
	see(t, m, "Удалить должника #1")
	press(m, 'y')
	see(t, m, "Карточка и её платежи удалены")
	press(m, 'i')
	replaceInput(m, path)
	press(m, tea.KeyEnter)
	see(t, m, "будет полностью заменена")
	press(m, 'n')
	see(t, m, "Нет карточек")
	press(m, 'i')
	replaceInput(m, path)
	press(m, tea.KeyEnter)
	press(m, 'y')
	see(t, m, "Ирина", "Копия восстановлена")
	if m.query != "" {
		t.Fatal("stale filter")
	}
}

func TestFailedFormsCanBeCorrectedOrCancelled(t *testing.T) {
	m := workspace(t)
	press(m, 'n')
	press(m, tea.KeyEnter)
	see(t, m, "Ошибка:")
	paste(m, "Иван")
	press(m, tea.KeyTab)
	paste(m, "100")
	press(m, tea.KeyEnter)
	see(t, m, "Иван", "100.00")
	press(m, 'p')
	press(m, tea.KeyEnter)
	see(t, m, "сумма")
	paste(m, "101")
	press(m, tea.KeyEnter)
	see(t, m, "превышает долг")
	press(m, tea.KeyEsc)
	press(m, 'x')
	press(m, tea.KeyEnter)
	see(t, m, "положительный ID")
	paste(m, "9")
	press(m, tea.KeyEnter)
	see(t, m, "нет платежа #9")
	press(m, tea.KeyEsc)
	press(m, 'd')
	replaceInput(m, "bad")
	press(m, tea.KeyEnter)
	see(t, m, "неверная дата")
	press(m, tea.KeyEsc)
	press(m, 'r')
	see(t, m, "Итого к возврату: 100.00")
	press(m, 'q')
	press(m, 'e')
	replaceInput(m, t.TempDir())
	press(m, tea.KeyEnter)
	see(t, m, "Ошибка:")
	press(m, tea.KeyEsc)
	press(m, 'i')
	replaceInput(m, filepath.Join(t.TempDir(), "none"))
	press(m, tea.KeyEnter)
	press(m, 'y')
	see(t, m, "Ошибка:")
	if m.confirm != nil || len(m.rows) != 1 {
		t.Fatal("failed restore changed portfolio")
	}
}

func TestScrollKeepsSelectionAndHistoryVisible(t *testing.T) {
	m := workspace(t)
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 14})
	for i := range 40 {
		m.rows = append(m.rows, listRow{text: strings.Repeat("А", i+1)})
	}
	press(m, tea.KeyEnd)
	if m.selected != 39 {
		t.Fatal(m.selected)
	}
	press(m, tea.KeyUp)
	press(m, tea.KeyPgUp)
	if m.selected != 38-m.listPage() {
		t.Fatal(m.selected)
	}
	press(m, tea.KeyHome)
	press(m, tea.KeyDown)
	see(t, m, "› АА")
	m.screen = cardScreen
	m.history = []ledger.Event{{Date: testDay, Accrual: 99}, {Date: testDay, Allocation: &ledger.Allocation{Payment: ledger.Payment{ID: 7, Date: testDay}}}}
	m.wrapHistory()
	press(m, tea.KeyEnd)
	see(t, m, "платёж #7")
	press(m, tea.KeyHome)
	if m.offset != 0 {
		t.Fatal(m.offset)
	}
}
