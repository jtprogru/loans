package ledger

import (
	"strings"
	"testing"
)

func TestStoredTermsAreNotReinterpreted(t *testing.T) {
	d, err := NewDebtor("Анна", "100", "2025-01-01", "10", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Debtor)
	}{
		{"empty name", func(d *Debtor) { d.Name = " " }}, {"control in name", func(d *Debtor) { d.Name = "А\nБ" }}, {"amount", func(d *Debtor) { d.Principal = -1 }}, {"date", func(d *Debtor) { d.Issued = "2025-02-30" }}, {"implicit with fixed terms", func(d *Debtor) { d.Mode = ImplicitKey }}, {"decimal comma in storage", func(d *Debtor) { d.Rate = "1,5" }}, {"invalid period", func(d *Debtor) { d.Period = "0d" }}, {"unknown mode", func(d *Debtor) { d.Mode = "floating" }}, {"unknown priority", func(d *Debtor) { d.Repayment = "both" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := d
			tc.change(&v)
			if v.Validate() == nil {
				t.Fatal("accepted invalid terms")
			}
			if _, err := Replay(v, nil, Rates{}, "2025-01-01", false); err == nil {
				t.Fatal("invalid loan replayed")
			}
		})
	}
	for _, tc := range []struct{ amount, rate, description string }{{"100001", "", "сумма свыше"}, {"100", "key-rate", "размер не указан"}, {"100", "0", "явно беспроцентный"}, {"100", "12", "12% за год"}} {
		v, err := NewDebtor("Анна", tc.amount, d.Issued, tc.rate, "", PrincipalFirst)
		if err != nil || !strings.Contains(v.Terms(), tc.description) || !strings.Contains(v.Terms(), "сначала тело") {
			t.Fatalf("terms=%q err=%v", v.Terms(), err)
		}
	}
}
func TestRateBookRejectsInvalidData(t *testing.T) {
	day := "2025-01-01"
	entry := KeyRate{day, "10", "архив"}
	for _, entries := range [][]KeyRate{{{"bad", "10", "архив"}}, {{day, "1,2", "архив"}}, {{day, "0", "\n"}}, {{day, "0", "A\x7fB"}}, {entry, entry}} {
		if err := (Rates{Entries: entries}).Validate(); err == nil {
			t.Fatalf("accepted %+v", entries)
		}
	}
	for _, c := range []Coverage{{"bad", day}, {day, "bad"}, {"2025-02-01", day}, {"2024-12-31", day}} {
		if err := (Rates{[]KeyRate{entry}, &c}).Validate(); err == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
}
