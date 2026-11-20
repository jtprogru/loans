package ledger

import (
	"fmt"
	"math"
	"math/big"
	"slices"
	"strings"
	"time"
)

// Allocation shows how one payment was split.
type Allocation struct {
	Payment
	Principal int64
	Interest  int64
}

// Event is either a day's rounded accrual or a payment.
type Event struct {
	Date       string
	Accrual    int64
	Allocation *Allocation
}

// Report is the end-of-day balance and the optional history behind it.
type Report struct {
	Date        string
	Issued      bool
	Principal   int64
	Interest    int64
	Total       int64
	Allocations []Allocation
	Events      []Event
}

// Status describes the state on the selected date.
func (r Report) Status() string {
	switch {
	case !r.Issued:
		return "заём ещё не выдан"
	case r.Total == 0:
		return "закрыт"
	default:
		return "открыт"
	}
}

// Replay calculates the balance up to and including asOf. With history enabled,
// Events includes nonzero daily accruals computed from adjacent rounded totals.
func Replay(d Debtor, payments []Payment, rates Rates, asOf string, history bool) (Report, error) {
	report := Report{Date: asOf}
	end, err := ParseDate(asOf)
	if err != nil {
		return report, err
	}
	if err := d.Validate(); err != nil {
		return report, err
	}
	if asOf < d.Issued {
		return report, nil
	}
	issue, err := ParseDate(d.Issued)
	if err != nil {
		return report, err
	}
	report.Issued, report.Principal = true, d.Principal
	order := slices.Clone(payments)
	slices.SortFunc(order, func(a, b Payment) int {
		if cmp := strings.Compare(a.Date, b.Date); cmp != 0 {
			return cmp
		}
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})

	state := replayState{loan: d, issue: issue, day: issue.AddDate(0, 0, 1), rates: rates, report: &report, history: history}
	if d.Mode == Fixed {
		state.fixed = rateFraction(d.Rate)
		state.period, err = parsePeriod(d.Period)
		if err != nil {
			return Report{}, err
		}
	}
	for _, p := range order {
		if p.Date > asOf {
			break
		}
		pday, err := ParseDate(p.Date)
		if err != nil {
			return Report{}, fmt.Errorf("платёж #%d: %w", p.ID, err)
		}
		if pday.Before(issue) || p.Amount <= 0 || p.DebtorID != d.ID {
			return Report{}, fmt.Errorf("платёж #%d: неверный заём, дата или сумма", p.ID)
		}
		if err := state.accrue(pday.AddDate(0, 0, 1)); err != nil {
			return Report{}, err
		}
		if err := state.pay(p); err != nil {
			return Report{}, err
		}
	}
	if err := state.accrue(end.AddDate(0, 0, 1)); err != nil {
		return Report{}, err
	}
	interest := state.outstanding()
	if !interest.IsInt64() || interest.Int64() > math.MaxInt64-report.Principal {
		return Report{}, fmt.Errorf("итог на %s превышает диапазон денежных сумм", asOf)
	}
	report.Interest = interest.Int64()
	report.Total = report.Principal + report.Interest
	return report, nil
}

type replayState struct {
	loan    Debtor
	issue   time.Time
	day     time.Time
	rates   Rates
	fixed   *big.Rat
	period  period
	accrued big.Rat
	paid    big.Int
	report  *Report
	history bool
}

func (s *replayState) pay(p Payment) error {
	interest := s.outstanding()
	debt := new(big.Int).Add(interest, big.NewInt(s.report.Principal))
	if debt.Cmp(big.NewInt(p.Amount)) < 0 {
		return fmt.Errorf("платёж #%d от %s превышает долг в момент платежа", p.ID, p.Date)
	}
	a := Allocation{Payment: p}
	if s.loan.Repayment == PrincipalFirst {
		a.Principal = min(p.Amount, s.report.Principal)
		a.Interest = p.Amount - a.Principal
	} else {
		a.Interest = p.Amount
		if interest.IsInt64() {
			a.Interest = min(p.Amount, interest.Int64())
		}
		a.Principal = p.Amount - a.Interest
	}
	s.report.Principal -= a.Principal
	s.paid.Add(&s.paid, big.NewInt(a.Interest))
	s.report.Allocations = append(s.report.Allocations, a)
	if s.history {
		s.report.Events = append(s.report.Events, Event{Date: p.Date, Allocation: &a})
	}
	return nil
}

func (s *replayState) outstanding() *big.Int {
	total := rounded(&s.accrued)
	return total.Sub(total, &s.paid)
}

func (s *replayState) accrue(end time.Time) error {
	if s.report.Principal == 0 || s.loan.Mode == ImplicitFree || s.fixed != nil && s.fixed.Sign() == 0 {
		if s.day.Before(end) {
			s.day = end
		}
		return nil
	}
	for s.day.Before(end) {
		stop := end
		rate := s.fixed
		if rate == nil {
			value, boundary, err := s.rates.at(s.day, end)
			if err != nil {
				return err
			}
			stop, rate = boundary, rateFraction(value)
		}
		denominator, stop := s.denominator(stop)
		daily := new(big.Rat).Mul(rate, big.NewRat(s.report.Principal, denominator))
		if !s.history || daily.Sign() == 0 {
			s.accrued.Add(&s.accrued, daily.Mul(daily, big.NewRat(daysBetween(s.day, stop), 1)))
			s.day = stop
			continue
		}
		before := rounded(&s.accrued)
		for s.day.Before(stop) {
			s.accrued.Add(&s.accrued, daily)
			after := rounded(&s.accrued)
			delta := before.Sub(after, before)
			if !delta.IsInt64() {
				return fmt.Errorf("начисление на %s превышает диапазон денежных сумм", s.day.Format(DateLayout))
			}
			if delta.Sign() != 0 {
				s.report.Events = append(s.report.Events, Event{Date: s.day.Format(DateLayout), Accrual: delta.Int64()})
			}
			before = after
			s.day = s.day.AddDate(0, 0, 1)
		}
	}
	return nil
}

func (s *replayState) denominator(stop time.Time) (int64, time.Time) {
	switch s.period.unit {
	case 'd':
		return int64(s.period.size), stop
	case 'm':
		// day-1 handles exactly the anchored month boundary itself.
		prev := s.day.AddDate(0, 0, -1)
		delta := (prev.Year()-s.issue.Year())*12 + int(prev.Month()-s.issue.Month())
		leftMonths := delta / s.period.size * s.period.size
		left := anchored(s.issue, leftMonths)
		if left.After(prev) {
			leftMonths -= s.period.size
			left = anchored(s.issue, leftMonths)
		}
		right := anchored(s.issue, leftMonths+s.period.size)
		return daysBetween(left, right), earlier(stop, right.AddDate(0, 0, 1))
	default:
		start := time.Date(s.day.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		next := start.AddDate(1, 0, 0)
		return daysBetween(start, next), earlier(stop, next)
	}
}

func anchored(issue time.Time, months int) time.Time {
	year, month, day := issue.Date()
	year += months / 12
	month += time.Month(months % 12)
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC)
	return time.Date(last.Year(), last.Month(), min(day, last.Day()), 0, 0, 0, 0, time.UTC)
}

func daysBetween(a, b time.Time) int64 { return (b.Unix() - a.Unix()) / 86400 }

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func rounded(n *big.Rat) *big.Int {
	var remainder big.Int
	q, _ := new(big.Int).QuoRem(n.Num(), n.Denom(), &remainder)
	if remainder.Lsh(&remainder, 1).Cmp(n.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	return q
}
