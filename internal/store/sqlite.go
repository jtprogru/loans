// Package store implements the transactional SQLite ledger and portable backups.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jtprogru/loans/internal/ledger"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS debtors (
 id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL,
 principal INTEGER NOT NULL CHECK (principal > 0), issued TEXT NOT NULL,
 mode TEXT NOT NULL, rate TEXT NOT NULL, period TEXT NOT NULL, repayment TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS payments (
 id INTEGER PRIMARY KEY AUTOINCREMENT, debtor_id INTEGER NOT NULL REFERENCES debtors(id) ON DELETE CASCADE,
 date TEXT NOT NULL, amount INTEGER NOT NULL CHECK (amount > 0)
);
CREATE INDEX IF NOT EXISTS payment_order ON payments (debtor_id,date,id);
CREATE TABLE IF NOT EXISTS key_rates (
 date TEXT PRIMARY KEY NOT NULL, rate TEXT NOT NULL, source TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS rate_coverage (
 id INTEGER PRIMARY KEY CHECK (id = 1), start TEXT NOT NULL, through TEXT NOT NULL
);
`

// Store owns a SQLite database.
type Store struct {
	db   *sql.DB
	path string
}

// Open initializes and opens the database. All connections enforce foreign keys.
func Open(ctx context.Context, path string) (*Store, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	switch {
	case err == nil:
		if err := f.Close(); err != nil {
			return nil, err
		}
	case !errors.Is(err, os.ErrExist):
		return nil, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("путь базы %q не является обычным файлом", path)
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if !strings.HasPrefix(u.Path, "/") { // Windows drive letter.
		u.Path = "/" + u.Path
	}
	db, err := sql.Open("sqlite", u.String()+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, path: path}
	if err := s.tx(ctx, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, schema)
		return err
	}); err != nil {
		return nil, errors.Join(fmt.Errorf("инициализация SQLite %s: %w", path, err), db.Close())
	}
	return s, nil
}

// Close releases the database handles.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) tx(ctx context.Context, readOnly bool, fn func(*sql.Tx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(); !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, rollbackErr)
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Snapshot is a consistent copy of the raw ledger, without derived balances.
type Snapshot struct {
	Debtors  []ledger.Debtor  `json:"debtors"`
	Payments []ledger.Payment `json:"payments"`
	Rates    ledger.Rates     `json:"key_rates"`
}

// Snapshot reads the entire ledger in one transaction.
func (s *Store) Snapshot(ctx context.Context) (out Snapshot, err error) {
	err = s.tx(ctx, true, func(tx *sql.Tx) error {
		out, err = snapshot(ctx, tx)
		return err
	})
	return out, err
}

func snapshot(ctx context.Context, tx *sql.Tx) (Snapshot, error) {
	debtors, err := query(ctx, tx, scanDebtor, "SELECT "+debtorColumns+" FROM debtors ORDER BY id")
	if err != nil {
		return Snapshot{}, err
	}
	payments, err := query(ctx, tx, scanPayment, "SELECT "+paymentColumns+" FROM payments ORDER BY debtor_id,date,id")
	if err != nil {
		return Snapshot{}, err
	}
	rates, err := loadRates(ctx, tx)
	return Snapshot{Debtors: debtors, Payments: payments, Rates: rates}, err
}

const (
	debtorColumns  = "id,name,principal,issued,mode,rate,period,repayment"
	paymentColumns = "id,debtor_id,date,amount"
)

type row interface{ Scan(...any) error }

func scanDebtor(r row) (d ledger.Debtor, err error) {
	err = r.Scan(&d.ID, &d.Name, &d.Principal, &d.Issued, &d.Mode, &d.Rate, &d.Period, &d.Repayment)
	return d, err
}

func scanPayment(r row) (p ledger.Payment, err error) {
	err = r.Scan(&p.ID, &p.DebtorID, &p.Date, &p.Amount)
	return p, err
}

func query[T any](ctx context.Context, tx *sql.Tx, scan func(row) (T, error), sql string, args ...any) (out []T, err error) {
	rows, err := tx.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	out = []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func loadDebtor(ctx context.Context, tx *sql.Tx, id int64) (ledger.Debtor, error) {
	d, err := scanDebtor(tx.QueryRowContext(ctx, "SELECT "+debtorColumns+" FROM debtors WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		err = fmt.Errorf("должник #%d не найден", id)
	}
	return d, err
}

func loadPayments(ctx context.Context, tx *sql.Tx, id int64) ([]ledger.Payment, error) {
	return query(ctx, tx, scanPayment, "SELECT "+paymentColumns+" FROM payments WHERE debtor_id=? ORDER BY date,id", id)
}

func loadRates(ctx context.Context, tx *sql.Tx) (ledger.Rates, error) {
	entries, err := query(ctx, tx, func(r row) (k ledger.KeyRate, err error) {
		err = r.Scan(&k.Date, &k.Rate, &k.Source)
		return k, err
	}, "SELECT date,rate,source FROM key_rates ORDER BY date")
	if err != nil {
		return ledger.Rates{}, err
	}
	rates := ledger.Rates{Entries: entries}
	var c ledger.Coverage
	err = tx.QueryRowContext(ctx, "SELECT start,through FROM rate_coverage WHERE id=1").Scan(&c.From, &c.Through)
	if errors.Is(err, sql.ErrNoRows) {
		return rates, nil
	}
	if err == nil {
		rates.Coverage = &c
	}
	return rates, err
}

// Loan reads a debtor, their payments and the rates from the same snapshot.
func (s *Store) Loan(ctx context.Context, id int64) (d ledger.Debtor, ps []ledger.Payment, rates ledger.Rates, err error) {
	err = s.tx(ctx, true, func(tx *sql.Tx) error {
		d, err = loadDebtor(ctx, tx, id)
		if err != nil {
			return err
		}
		ps, err = loadPayments(ctx, tx, id)
		if err != nil {
			return err
		}
		rates, err = loadRates(ctx, tx)
		return err
	})
	return d, ps, rates, err
}

// AddDebtor saves validated, already-resolved terms and returns the new ID.
func (s *Store) AddDebtor(ctx context.Context, d ledger.Debtor) (id int64, err error) {
	if err := d.Validate(); err != nil {
		return 0, err
	}
	err = s.tx(ctx, false, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "INSERT INTO debtors (name,principal,issued,mode,rate,period,repayment) VALUES (?,?,?,?,?,?,?)",
			d.Name, d.Principal, d.Issued, d.Mode, d.Rate, d.Period, d.Repayment)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// UpdateDebtor corrects a loan in place, refusing stale edits or invalid payment history.
func (s *Store) UpdateDebtor(ctx context.Context, before, after ledger.Debtor) error {
	if before.ID != after.ID {
		return errors.New("нельзя изменить ID должника")
	}
	if err := after.Validate(); err != nil {
		return err
	}
	return s.tx(ctx, false, func(tx *sql.Tx) error {
		current, err := loadDebtor(ctx, tx, before.ID)
		if err != nil {
			return err
		}
		if current != before {
			return errors.New("карточка изменилась; обновите её перед правкой")
		}
		_, err = tx.ExecContext(ctx, "UPDATE debtors SET name=?,principal=?,issued=?,mode=?,rate=?,period=?,repayment=? WHERE id=?", after.Name, after.Principal, after.Issued, after.Mode, after.Rate, after.Period, after.Repayment, after.ID)
		if err != nil {
			return err
		}
		return validateLoan(ctx, tx, after)
	})
}

// DeleteDebtor removes a debtor and their payments together.
func (s *Store) DeleteDebtor(ctx context.Context, id int64) error {
	return s.tx(ctx, false, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "DELETE FROM debtors WHERE id=?", id)
		return checkAffected(res, err, fmt.Sprintf("должник #%d не найден", id))
	})
}

func checkAffected(res sql.Result, err error, missing string) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return errors.New(missing)
	}
	return err
}

// AddPayment inserts and checks the resulting history in one transaction.
func (s *Store) AddPayment(ctx context.Context, p ledger.Payment, today string) (id int64, err error) {
	err = s.tx(ctx, false, func(tx *sql.Tx) error {
		d, err := loadDebtor(ctx, tx, p.DebtorID)
		if err != nil {
			return err
		}
		if err := validatePayment(d, p, today); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "INSERT INTO payments (debtor_id,date,amount) VALUES (?,?,?)", p.DebtorID, p.Date, p.Amount)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return err
		}
		return validateLoan(ctx, tx, d)
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// UpdatePayment corrects a transfer without changing its owner, ID or same-day ordering.
func (s *Store) UpdatePayment(ctx context.Context, before ledger.Payment, date string, amount int64, today string) error {
	return s.tx(ctx, false, func(tx *sql.Tx) error {
		d, err := loadDebtor(ctx, tx, before.DebtorID)
		if err != nil {
			return err
		}
		after := before
		after.Date, after.Amount = date, amount
		if err := validatePayment(d, after, today); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "UPDATE payments SET date=?,amount=? WHERE id=? AND debtor_id=? AND date=? AND amount=?", date, amount, before.ID, before.DebtorID, before.Date, before.Amount)
		if err := checkAffected(res, err, "платёж изменён или удалён; обновите карточку"); err != nil {
			return err
		}
		return validateLoan(ctx, tx, d)
	})
}

// DeletePayment removes a payment only if all remaining payments are still valid.
func (s *Store) DeletePayment(ctx context.Context, debtor, id int64) error {
	return s.tx(ctx, false, func(tx *sql.Tx) error {
		d, err := loadDebtor(ctx, tx, debtor)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "DELETE FROM payments WHERE debtor_id=? AND id=?", debtor, id)
		if err := checkAffected(res, err, fmt.Sprintf("у должника #%d нет платежа #%d", debtor, id)); err != nil {
			return err
		}
		return validateLoan(ctx, tx, d)
	})
}

func validatePayment(d ledger.Debtor, p ledger.Payment, today string) error {
	if _, err := ledger.ParseDate(p.Date); err != nil {
		return err
	}
	switch {
	case p.Date < d.Issued:
		return errors.New("платёж до даты выдачи")
	case p.Date > today:
		return errors.New("платёж в будущем")
	case p.Amount <= 0:
		return errors.New("сумма платежа должна быть положительной")
	}
	return nil
}

func validateLoan(ctx context.Context, tx *sql.Tx, d ledger.Debtor) error {
	ps, err := loadPayments(ctx, tx, d.ID)
	if err != nil || len(ps) == 0 {
		return err
	}
	if ps[0].Date < d.Issued {
		return fmt.Errorf("платёж #%d до даты выдачи", ps[0].ID)
	}
	rates, err := loadRates(ctx, tx)
	if err != nil {
		return err
	}
	return replayPayments(d, ps, rates)
}

func replayPayments(d ledger.Debtor, ps []ledger.Payment, rates ledger.Rates) error {
	if len(ps) == 0 {
		return nil
	}
	last := ""
	for _, p := range ps {
		last = max(last, p.Date)
	}
	_, err := ledger.Replay(d, ps, rates, last, false)
	return err
}

// SetRate inserts or corrects an observation by effective date.
func (s *Store) SetRate(ctx context.Context, k ledger.KeyRate) error {
	if err := k.Validate(); err != nil {
		return err
	}
	return s.tx(ctx, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO key_rates (date,rate,source) VALUES (?,?,?) ON CONFLICT(date) DO UPDATE SET rate=excluded.rate,source=excluded.source", k.Date, k.Rate, k.Source)
		if err != nil {
			return err
		}
		return validateRateChange(ctx, tx)
	})
}

// SetCoverage changes the inclusive checked interval without dropping observations.
func (s *Store) SetCoverage(ctx context.Context, c ledger.Coverage) error {
	return s.tx(ctx, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO rate_coverage VALUES (1,?,?) ON CONFLICT(id) DO UPDATE SET start=excluded.start,through=excluded.through", c.From, c.Through)
		if err != nil {
			return err
		}
		return validateRateChange(ctx, tx)
	})
}

func validateRateChange(ctx context.Context, tx *sql.Tx) error {
	data, err := snapshot(ctx, tx)
	if err != nil {
		return err
	}
	if err := data.Rates.Validate(); err != nil {
		return err
	}
	byDebtor := data.PaymentsByDebtor()
	for _, d := range data.Debtors {
		if d.Mode == ledger.ImplicitKey || d.Mode == ledger.ExplicitKey {
			if err := replayPayments(d, byDebtor[d.ID], data.Rates); err != nil {
				return fmt.Errorf("должник #%d: %w", d.ID, err)
			}
		}
	}
	return nil
}

// PaymentsByDebtor groups payments without copying or deriving allocations.
func (data Snapshot) PaymentsByDebtor() map[int64][]ledger.Payment {
	out := make(map[int64][]ledger.Payment)
	for _, p := range data.Payments {
		out[p.DebtorID] = append(out[p.DebtorID], p)
	}
	return out
}
