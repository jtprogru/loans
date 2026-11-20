package ledger

import (
	"errors"
	"strings"
	"testing"
)

func makeLoan(t *testing.T, amount, date, rate, period, repayment string) Debtor {
	t.Helper()
	d, err := NewDebtor("Анна", amount, date, rate, period, repayment)
	if err != nil {
		t.Fatal(err)
	}
	d.ID = 1
	return d
}

func makePayment(id int64, date string, cents int64) Payment {
	return Payment{ID: id, DebtorID: 1, Date: date, Amount: cents}
}

func TestAcceptanceTimeline(t *testing.T) {
	d := makeLoan(t, "10000", "2025-03-31", "30", "1m", PrincipalFirst)
	ps := []Payment{
		makePayment(3, "2025-05-01", 20_000),
		makePayment(1, "2025-04-10", 400_000),
		makePayment(2, "2025-04-30", 800_000),
	}
	for _, tc := range []struct {
		date                string
		principal, interest int64
		status              string
	}{
		{"2025-03-30", 0, 0, "заём ещё не выдан"},
		{"2025-03-31", 1_000_000, 0, "открыт"},
		{"2025-04-01", 1_000_000, 10_000, "открыт"},
		{"2025-04-10", 600_000, 100_000, "открыт"},
		{"2025-04-11", 600_000, 106_000, "открыт"},
		{"2025-04-29", 600_000, 214_000, "открыт"},
		{"2025-04-30", 0, 20_000, "открыт"},
		{"2025-05-01", 0, 0, "закрыт"},
		{"2025-06-01", 0, 0, "закрыт"},
	} {
		t.Run(tc.date, func(t *testing.T) {
			for _, history := range []bool{true, false} {
				r, err := Replay(d, ps, Rates{}, tc.date, history)
				if err != nil || r.Principal != tc.principal || r.Interest != tc.interest ||
					r.Total != tc.principal+tc.interest || r.Status() != tc.status {
					t.Fatalf("report %+v, err %v", r, err)
				}
				if tc.date == "2025-04-30" {
					if a := r.Allocations[1]; a.Principal != 600_000 || a.Interest != 200_000 {
						t.Fatalf("wrong boundary allocation: %+v", a)
					}
				}
			}
		})
	}
	for _, tc := range []struct {
		ps    []Payment
		date  string
		total int64
	}{
		{nil, "2025-04-30", 1_300_000},
		{[]Payment{makePayment(1, d.Issued, 400_000)}, "2025-04-01", 606_000},
	} {
		r, err := Replay(d, tc.ps, Rates{}, tc.date, false)
		if err != nil || r.Total != tc.total {
			t.Fatalf("report %+v, err %v", r, err)
		}
	}
}

func TestPeriodAndAnnualBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, amount, issue, rate, period, asOf string
		total                                   int64
	}{
		{"20 of 21 days", "5000", "2025-02-01", "10", "21d", "2025-02-21", 547619},
		{"21 of 21 days", "5000", "2025-02-01", "10", "21d", "2025-02-22", 550000},
		{"28-day calendar month", "10000", "2025-01-31", "10", "1m", "2025-02-28", 1100000},
		{"restored 31-day anchor", "10000", "2025-01-31", "10", "1m", "2025-03-31", 1200000},
		{"after shortened month", "10000", "2025-01-31", "10", "1m", "2025-03-01", 1103226},
		{"29-day calendar month", "10000", "2024-01-31", "10", "1m", "2024-02-29", 1100000},
		{"two calendar months", "10000", "2024-12-31", "10", "2m", "2025-02-28", 1100000},
		{"leap February actual year", "366000", "2024-02-28", "10", "", "2024-03-01", 36620000},
		{"new calendar year denominator", "133590", "2023-12-30", "100", "", "2024-01-01", 13432100},
		{"far apart dates without duration overflow", "10", "0001-01-01", "1", "1d", "9999-12-31", 36521580},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := makeLoan(t, tc.amount, tc.issue, tc.rate, tc.period, "")
			r, err := Replay(d, nil, Rates{}, tc.asOf, false)
			if err != nil || r.Total != tc.total {
				t.Fatalf("got total %s, want %s, err %v", Money(r.Total), Money(tc.total), err)
			}
		})
	}
}

func TestRateRulesAndCoverage(t *testing.T) {
	for _, tc := range []struct {
		amount, rate, mode string
		wantMissing        bool
	}{
		{"100000", "", ImplicitFree, false},
		{"100000.01", "", ImplicitKey, true},
		{"100000.01", "0", Fixed, false},
		{"70000", ExplicitKey, ExplicitKey, true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			d := makeLoan(t, tc.amount, "2025-01-01", tc.rate, "", "")
			if d.Mode != tc.mode {
				t.Fatalf("mode %s, want %s", d.Mode, tc.mode)
			}
			r, err := Replay(d, nil, Rates{}, "2025-01-02", false)
			if tc.wantMissing {
				var missing *MissingRates
				if !errors.As(err, &missing) || missing.From != "2025-01-02" || missing.Through != missing.From {
					t.Fatalf("invalid coverage error %v", err)
				}
			} else if err != nil || r.Total != d.Principal {
				t.Fatalf("report %+v, err %v", r, err)
			}
		})
	}

	d := makeLoan(t, "365000", "2025-01-01", "", "", "")
	rates := Rates{
		Entries:  []KeyRate{{"2025-01-01", "10", "тест"}, {"2025-01-03", "12", "тест"}},
		Coverage: &Coverage{"2025-01-02", "2025-01-03"},
	}
	ps := []Payment{makePayment(1, "2025-01-03", 100000)}
	r, err := Replay(d, ps, rates, "2025-01-03", true)
	if err != nil || r.Total != 36422000 || len(r.Events) != 3 || r.Events[0].Accrual != 10000 || r.Events[1].Accrual != 12000 {
		t.Fatalf("report %+v, err %v", r, err)
	}
	if a := r.Allocations[0]; a.Interest != 22000 || a.Principal != 78000 {
		t.Fatalf("allocation %+v", a)
	}
	r, err = Replay(d, ps, rates, "2025-01-02", false)
	if err != nil || r.Total != 36510000 {
		t.Fatalf("historic report %+v, err %v", r, err)
	}
	_, err = Replay(d, ps, rates, "2025-01-06", false)
	var missing *MissingRates
	if !errors.As(err, &missing) || missing.From != "2025-01-04" || missing.Through != "2025-01-06" {
		t.Fatalf("incomplete coverage error %v", err)
	}

	small := makeLoan(t, "100000.01", "2025-01-01", "", "", "")
	r, err = Replay(small, []Payment{makePayment(1, small.Issued, 1)}, rates, "2025-01-02", false)
	if err != nil || r.Principal != 10000000 || r.Interest == 0 {
		t.Fatalf("crossing threshold changed rule: %+v, err %v", r, err)
	}
}

func TestAccumulatedRoundingAndPaymentOrder(t *testing.T) {
	d := makeLoan(t, "1", "2025-01-01", "1", "4d", "")
	ps := []Payment{makePayment(1, "2025-01-03", 1)}
	r, err := Replay(d, ps, Rates{}, "2025-01-07", true)
	if err != nil || r.Total != 101 || len(r.Events) != 3 || r.Events[0].Date != "2025-01-03" ||
		r.Events[0].Accrual != 1 || r.Events[1].Allocation == nil || r.Events[1].Allocation.Interest != 1 ||
		r.Events[2].Date != "2025-01-07" || r.Events[2].Accrual != 1 {
		t.Fatalf("report %+v, err %v", r, err)
	}
	ps = append(ps, makePayment(2, "2025-01-07", 101))
	r, err = Replay(d, ps, Rates{}, "2025-01-08", false)
	if err != nil || r.Total != 0 {
		t.Fatalf("cannot close by displayed rounded total: %+v, %v", r, err)
	}

	over := []Payment{makePayment(9, d.Issued, 70), makePayment(2, d.Issued, 40)}
	_, err = Replay(d, over, Rates{}, d.Issued, false)
	if err == nil || !strings.Contains(err.Error(), "#9") {
		t.Fatalf("expected payment #9 to be rejected, got %v", err)
	}
}

func TestInputValidation(t *testing.T) {
	for _, s := range []string{"", "0", "-1", "0.001", "NaN", "1e2", "92233720368547758.08"} {
		if _, err := ParseMoney(s); err == nil {
			t.Errorf("invalid amount accepted: %q", s)
		}
	}
	if n, err := ParseMoney("92233720368547758,07"); err != nil || Money(n) != "92233720368547758.07" {
		t.Fatalf("max amount: %d %v", n, err)
	}
	for _, s := range []string{"", "2025-2-01", "2025-02-29", "0000-01-01", "2025-01-01T00:00:00Z"} {
		if _, err := ParseDate(s); err == nil {
			t.Errorf("invalid date accepted: %q", s)
		}
	}
	for _, tc := range []struct{ rate, period string }{
		{"key-rate", "1m"}, {"", "1m"}, {"10", "0d"}, {"10", "-1m"}, {"-1", ""}, {"1e2", ""},
	} {
		if _, err := NewDebtor("Анна", "100", "2025-01-01", tc.rate, tc.period, ""); err == nil {
			t.Errorf("invalid terms accepted: %+v", tc)
		}
	}
}
