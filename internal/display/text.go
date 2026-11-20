// Package display keeps balance and history labels consistent across interfaces.
package display

import (
	"fmt"
	"strings"

	"github.com/jtprogru/loans/internal/ledger"
)

// Balance renders the report or the not-yet-issued state.
func Balance(r ledger.Report) string {
	if !r.Issued {
		return fmt.Sprintf("%s: %s", r.Date, r.Status())
	}
	return fmt.Sprintf("Дата расчёта: %s\nОстаток тела: %s ₽\nНеоплаченные проценты: %s ₽\nИтого к возврату: %s ₽\nСтатус: %s",
		r.Date, ledger.Money(r.Principal), ledger.Money(r.Interest), ledger.Money(r.Total), r.Status())
}

// Payment renders the amounts allocated from one payment.
func Payment(a ledger.Allocation) string {
	return fmt.Sprintf("%s  платёж #%d: %s ₽ (тело %s ₽, проценты %s ₽)",
		a.Date, a.ID, ledger.Money(a.Amount), ledger.Money(a.Principal), ledger.Money(a.Interest))
}

// RateBook shows the coverage the user confirmed and all recorded entries.
func RateBook(r ledger.Rates) []string {
	lines := []string{"Проверенный интервал не задан"}
	if r.Coverage != nil {
		lines[0] = fmt.Sprintf("Проверенный интервал: %s — %s", r.Coverage.From, r.Coverage.Through)
	}
	lines = append(lines, "История: "+ledger.KeyRateSource)
	for _, k := range r.Entries {
		lines = append(lines, fmt.Sprintf("%s: %s%%  %s", k.Date, k.Rate, k.Source))
	}
	return lines
}

// LastPayment returns the newest payment date, or the issue date without payments.
func LastPayment(d ledger.Debtor, ps []ledger.Payment) string {
	date := d.Issued
	for _, p := range ps {
		date = max(date, p.Date)
	}
	return date
}

// Summary is the list row for an issued loan with a confirmed balance.
func Summary(d ledger.Debtor, r ledger.Report) string {
	who := fmt.Sprintf("#%d %s", d.ID, d.Name)
	if !r.Issued {
		return who + " | " + r.Status()
	}
	return fmt.Sprintf("%s | тело %s ₽ | проценты %s ₽ | итог %s ₽ | %s", who,
		ledger.Money(r.Principal), ledger.Money(r.Interest), ledger.Money(r.Total), r.Status())
}

// OneLine flattens multi-line diagnostics for display in one TUI row.
func OneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
