package ledger

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// KeyRate is a local annual percentage effective from Date, with its source.
type KeyRate struct {
	Date   string `json:"effective_from"`
	Rate   string `json:"rate_percent"`
	Source string `json:"source"`
}

// Validate checks one rate observation.
func (k KeyRate) Validate() error {
	if _, err := ParseDate(k.Date); err != nil {
		return err
	}
	if normalized, err := NormalizeRate(k.Rate); err != nil || normalized != k.Rate {
		return errors.New("неверное значение ключевой ставки")
	}
	if strings.TrimSpace(k.Source) == "" || strings.ContainsFunc(k.Source, func(r rune) bool {
		return r < 32 || r >= 127 && r <= 159
	}) {
		return errors.New("укажите источник ключевой ставки без управляющих символов")
	}
	return nil
}

// Coverage is the inclusive interval the user verified to have all rate changes.
type Coverage struct {
	From    string `json:"from"`
	Through string `json:"through"`
}

// Rates is the offline reference data. Entries must be sorted by date.
type Rates struct {
	Entries  []KeyRate `json:"entries"`
	Coverage *Coverage `json:"coverage"`
}

// Validate rejects duplicate entries and coverage without an initial rate.
func (r Rates) Validate() error {
	var prev string
	for _, entry := range r.Entries {
		if err := entry.Validate(); err != nil {
			return fmt.Errorf("ключевая ставка %s: %w", entry.Date, err)
		}
		if entry.Date <= prev {
			return fmt.Errorf("дублирующаяся дата ключевой ставки или нарушен порядок: %s", entry.Date)
		}
		prev = entry.Date
	}
	if c := r.Coverage; c != nil {
		if _, err := ParseDate(c.From); err != nil {
			return err
		}
		if _, err := ParseDate(c.Through); err != nil {
			return err
		}
		if c.Through < c.From {
			return errors.New("конец проверенного интервала раньше начала")
		}
		if len(r.Entries) == 0 || r.Entries[0].Date > c.From {
			return fmt.Errorf("нужно значение ключевой ставки, действующее на %s", c.From)
		}
	}
	return nil
}

// MissingRates identifies the first unsupported interval in a calculation.
type MissingRates struct {
	From    string
	Through string
}

func (m *MissingRates) Error() string {
	return fmt.Sprintf("нет проверенных данных ключевой ставки за %s — %s; обновите локальный справочник", m.From, m.Through)
}

// at returns the rate and bounds this segment by a coverage edge or a rate change.
func (r Rates) at(day, end time.Time) (string, time.Time, error) {
	date := day.Format(DateLayout)
	c := r.Coverage
	if c == nil || date > c.Through {
		return "", end, &MissingRates{date, end.AddDate(0, 0, -1).Format(DateLayout)}
	}
	if date < c.From {
		from, err := ParseDate(c.From)
		if err != nil {
			return "", end, err
		}
		last := earlier(end, from).AddDate(0, 0, -1)
		return "", end, &MissingRates{date, last.Format(DateLayout)}
	}
	i := sort.Search(len(r.Entries), func(i int) bool { return r.Entries[i].Date > date })
	if i == 0 {
		return "", end, &MissingRates{date, end.AddDate(0, 0, -1).Format(DateLayout)}
	}
	if i < len(r.Entries) {
		next, err := ParseDate(r.Entries[i].Date)
		if err != nil {
			return "", end, err
		}
		end = earlier(end, next)
	}
	through, err := ParseDate(c.Through)
	if err != nil {
		return "", end, err
	}
	return r.Entries[i-1].Rate, earlier(end, through.AddDate(0, 0, 1)), nil
}
