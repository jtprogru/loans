package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/jtprogru/loans/internal/ledger"
	"testing"
)

func TestEditLoanAndPayment(t *testing.T) {
	m := workspace(t)
	press(m, 'n')
	paste(m, "Анна")
	press(m, tea.KeyTab)
	paste(m, "100")
	press(m, tea.KeyEnter)
	press(m, 'p')
	paste(m, "80")
	press(m, tea.KeyEnter)
	see(t, m, "Итого к возврату: 20.00")
	press(m, 'e')
	see(t, m, "Изменить карточку #1", "Анна")
	replaceInput(m, "Мария")
	press(m, tea.KeyTab)
	replaceInput(m, "70")
	press(m, tea.KeyEnter)
	see(t, m, "превышает долг")
	press(m, tea.KeyEsc)
	see(t, m, "Анна", "20.00")
	press(m, 'e')
	replaceInput(m, "Мария")
	press(m, tea.KeyTab)
	replaceInput(m, "120")
	press(m, tea.KeyEnter)
	see(t, m, "#1 Мария", "Итого к возврату: 40.00")
	press(m, 'P')
	paste(m, "1")
	press(m, tea.KeyEnter)
	see(t, m, "Изменить платёж #1", "80.00")
	replaceInput(m, "90")
	press(m, tea.KeyEnter)
	see(t, m, "платёж #1", "Итого к возврату: 30.00")
	data, err := m.db.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Debtors) != 1 || len(data.Payments) != 1 || data.Payments[0].Amount != 9000 || data.Debtors[0].Name != "Мария" {
		t.Fatalf("unexpected records %+v", data)
	}
}
func TestEditingPreservesRateRule(t *testing.T) {
	for _, tc := range []struct{ amount, rate, period string }{{"100", "", ""}, {"100001", "", ""}, {"100", "key-rate", ""}, {"100", "0", "21d"}, {"100", "2.50", "1m"}} {
		m := workspace(t)
		d, err := ledger.NewDebtor("Иван", tc.amount, testDay, tc.rate, tc.period, ledger.PrincipalFirst)
		if err != nil {
			t.Fatal(err)
		}
		d.ID, err = m.db.AddDebtor(t.Context(), d)
		if err != nil {
			t.Fatal(err)
		}
		m.loadCard(d.ID, testDay)
		press(m, 'e')
		see(t, m, "Изменить карточку #1")
		press(m, tea.KeyEnter)
		if m.form != nil || m.d != d {
			t.Fatalf("changed terms: got %+v, want %+v", m.d, d)
		}
	}
}
