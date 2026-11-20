package display

import (
	"github.com/jtprogru/loans/internal/ledger"
	"strings"
	"testing"
)

func TestLoanText(t *testing.T) {
	d := ledger.Debtor{ID: 7, Name: "Нина", Issued: "2025-05-12"}
	if s := Summary(d, ledger.Report{}); s != "#7 Нина | заём ещё не выдан" {
		t.Fatal(s)
	}
	if s := Balance(ledger.Report{Date: "2025-05-11"}); s != "2025-05-11: заём ещё не выдан" {
		t.Fatal(s)
	}
	r := ledger.Report{Issued: true, Date: d.Issued, Principal: 100, Interest: 3, Total: 103}
	if s := Summary(d, r); s != "#7 Нина | тело 1.00 ₽ | проценты 0.03 ₽ | итог 1.03 ₽ | открыт" {
		t.Fatal(s)
	}
	if s := Balance(r); !strings.Contains(s, "Итого к возврату: 1.03 ₽") || !strings.Contains(s, "Дата расчёта: "+r.Date) {
		t.Fatal(s)
	}
	a := ledger.Allocation{Payment: ledger.Payment{ID: 2, Date: "2025-06-01", Amount: 49}, Principal: 45, Interest: 4}
	if s := Payment(a); s != "2025-06-01  платёж #2: 0.49 ₽ (тело 0.45 ₽, проценты 0.04 ₽)" {
		t.Fatal(s)
	}
	if LastPayment(d, nil) != d.Issued || LastPayment(d, []ledger.Payment{a.Payment, {Date: d.Issued}}) != a.Date {
		t.Fatal("wrong cutoff")
	}
	if OneLine(" A\n\tБ ") != "A Б" {
		t.Fatal("diagnostic not flattened")
	}
}
func TestRateBookDescribesCoverage(t *testing.T) {
	r := ledger.Rates{}
	if RateBook(r)[0] != "Проверенный интервал не задан" {
		t.Fatal(RateBook(r))
	}
	r.Coverage = &ledger.Coverage{From: "2025-05-01", Through: "2025-05-31"}
	r.Entries = []ledger.KeyRate{{Date: "2025-05-01", Rate: "12", Source: "Архив"}}
	lines := RateBook(r)
	if len(lines) != 3 || lines[0] != "Проверенный интервал: 2025-05-01 — 2025-05-31" || lines[2] != "2025-05-01: 12%  Архив" {
		t.Fatal(lines)
	}
}
