// Package ledger validates loan terms and replays the history of a loan.
package ledger

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	DateLayout = "2006-01-02"

	ImplicitFree = "implicit-free"
	ImplicitKey  = "implicit-key-rate"
	ExplicitKey  = "key-rate"
	Fixed        = "fixed"
	Annual       = "year"

	InterestFirst  = "interest-first"
	PrincipalFirst = "principal-first"

	KeyRateSource = "https://www.cbr.ru/hd_base/KeyRate/"
	freeLimit     = 100_000_00
)

// Debtor is one issued loan with explicitly stored terms.
type Debtor struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Principal int64  `json:"principal_kopecks"`
	Issued    string `json:"issued"`
	Mode      string `json:"rate_mode"`
	Rate      string `json:"rate_percent"`
	Period    string `json:"period"`
	Repayment string `json:"repayment"`
}

// Payment is a recorded transfer; its allocation is derived, not stored.
type Payment struct {
	ID       int64  `json:"id"`
	DebtorID int64  `json:"debtor_id"`
	Date     string `json:"date"`
	Amount   int64  `json:"amount_kopecks"`
}

var (
	moneyPattern   = regexp.MustCompile(`^[0-9]+([.,][0-9]{1,2})?$`)
	percentPattern = regexp.MustCompile(`^[0-9]+([.,][0-9]+)?$`)
	periodPattern  = regexp.MustCompile(`^([0-9]+)([dm])$`)
)

// ParseDate reads a calendar date, using UTC only as a zone-free representation.
func ParseDate(s string) (time.Time, error) {
	d, err := time.Parse(DateLayout, s)
	if err != nil || d.Year() < 1 || d.Format(DateLayout) != s {
		return time.Time{}, fmt.Errorf("неверная дата %q: ожидается YYYY-MM-DD", s)
	}
	return d, nil
}

// Today returns the local calendar date.
func Today() string { return time.Now().Format(DateLayout) }

// ParseMoney converts a positive ruble amount to kopecks.
func ParseMoney(s string) (int64, error) {
	if !moneyPattern.MatchString(s) {
		return 0, errors.New("сумма должна быть положительной, в рублях, до двух знаков после разделителя")
	}
	rubles, fraction, _ := strings.Cut(strings.ReplaceAll(s, ",", "."), ".")
	kopecks, err := strconv.ParseInt(rubles+(fraction + "00")[:2], 10, 64)
	if err != nil || kopecks <= 0 {
		return 0, errors.New("сумма должна быть положительной и не превышать " + Money(math.MaxInt64) + " ₽")
	}
	return kopecks, nil
}

// Money formats a non-negative kopeck amount as rubles.
func Money(n int64) string { return fmt.Sprintf("%d.%02d", n/100, n%100) }

// NormalizeRate validates a decimal percentage and uses a dot in storage.
func NormalizeRate(s string) (string, error) {
	if !percentPattern.MatchString(s) {
		return "", errors.New("ставка должна быть неотрицательным десятичным числом")
	}
	return strings.ReplaceAll(s, ",", "."), nil
}

func rateFraction(s string) *big.Rat {
	r, _ := new(big.Rat).SetString(s) // Called only with validated percentages.
	return r.Quo(r, big.NewRat(100, 1))
}

type period struct {
	size int
	unit byte
}

func parsePeriod(s string) (period, error) {
	if s == Annual {
		return period{}, nil
	}
	match := periodPattern.FindStringSubmatch(s)
	if match != nil {
		n, err := strconv.ParseInt(match[1], 10, 32)
		if err == nil && n > 0 {
			return period{int(n), match[2][0]}, nil
		}
	}
	return period{}, fmt.Errorf("неверный период %q: ожидается year, число месяцев (1m) или дней (21d)", s)
}

// NewDebtor resolves omitted terms at creation time.
func NewDebtor(name, amount, date, rate, interval, repayment string) (Debtor, error) {
	n, err := ParseMoney(amount)
	if err != nil {
		return Debtor{}, err
	}
	d := Debtor{Name: strings.TrimSpace(name), Principal: n, Issued: date, Repayment: repayment}
	if d.Repayment == "" {
		d.Repayment = InterestFirst
	}
	if rate == "" || rate == ExplicitKey {
		if interval != "" {
			return Debtor{}, errors.New("период допустим только с числовой ставкой")
		}
		d.Mode = ExplicitKey
		if rate == "" {
			d.Mode = ImplicitFree
			if n > freeLimit {
				d.Mode = ImplicitKey
			}
		}
	} else {
		d.Mode, d.Period = Fixed, interval
		if d.Period == "" {
			d.Period = Annual
		}
		d.Rate, err = NormalizeRate(rate)
		if err != nil {
			return Debtor{}, err
		}
	}
	return d, d.Validate()
}

// Validate checks the persisted terms without applying defaults again.
func (d Debtor) Validate() error {
	if strings.TrimSpace(d.Name) == "" || strings.ContainsFunc(d.Name, func(r rune) bool {
		return r < 32 || r >= 127 && r <= 159
	}) {
		return errors.New("укажите имя без управляющих символов")
	}
	if d.Principal <= 0 {
		return errors.New("сумма выдачи должна быть положительной")
	}
	if _, err := ParseDate(d.Issued); err != nil {
		return err
	}
	switch d.Mode {
	case ImplicitFree, ImplicitKey, ExplicitKey:
		if d.Rate != "" || d.Period != "" {
			return errors.New("числовая ставка и период не соответствуют режиму займа")
		}
	case Fixed:
		if normalized, err := NormalizeRate(d.Rate); err != nil || normalized != d.Rate {
			return errors.New("неверная договорная ставка")
		}
		if _, err := parsePeriod(d.Period); err != nil {
			return err
		}
	default:
		return fmt.Errorf("неизвестный режим ставки %q", d.Mode)
	}
	switch d.Repayment {
	case PrincipalFirst, InterestFirst:
		return nil
	default:
		return fmt.Errorf("неизвестный порядок погашения %q: используйте interest-first или principal-first", d.Repayment)
	}
}

// Terms describes the actual rate rule and payment priority.
func (d Debtor) Terms() string {
	var rule string
	switch d.Mode {
	case ImplicitFree:
		rule = "беспроцентный: договор молчит, сумма до 100 000 ₽ включительно"
	case ImplicitKey:
		rule = "ключевая ставка ЦБ РФ (годовая): договор молчит, сумма свыше 100 000 ₽"
	case ExplicitKey:
		rule = "ключевая ставка ЦБ РФ (годовая): проценты предусмотрены, размер не указан"
	case Fixed:
		interval := d.Period
		if interval == Annual {
			interval = "год"
		}
		rule = fmt.Sprintf("договорная ставка %s%% за %s", d.Rate, interval)
		if rateFraction(d.Rate).Sign() == 0 {
			rule += " (явно беспроцентный)"
		}
	}
	priority := "сначала проценты, затем тело"
	if d.Repayment == PrincipalFirst {
		priority = "сначала тело, затем проценты"
	}
	return rule + "; " + priority
}
