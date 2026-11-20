package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/jtprogru/loans/internal/ledger"
)

const backupVersion = 1

type backup struct {
	Version int `json:"version"`
	Snapshot
}

// Export writes a consistent JSON snapshot, replacing the target only on success.
func (s *Store) Export(ctx context.Context, path string) (err error) {
	if err := s.checkBackupPath(path); err != nil {
		return err
	}
	data, err := s.Snapshot(ctx)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".loans-backup-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(f.Name()))
		}
	}()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(backup{backupVersion, data}); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Sync(); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Import atomically restores a validated backup, never merging data.
func (s *Store) Import(ctx context.Context, path, today string, replace bool) error {
	if err := s.checkBackupPath(path); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	data, err := readBackup(f, today)
	if err := errors.Join(err, f.Close()); err != nil {
		return fmt.Errorf("копия %q: %w", path, err)
	}
	return s.tx(ctx, false, func(tx *sql.Tx) error {
		if !replace {
			var exists bool
			err := tx.QueryRowContext(ctx, `SELECT EXISTS (
			 SELECT 1 FROM debtors UNION ALL SELECT 1 FROM key_rates UNION ALL SELECT 1 FROM rate_coverage
			)`).Scan(&exists)
			if err != nil {
				return err
			}
			if exists {
				return errors.New("база не пуста: для полной замены требуется --replace")
			}
		}
		return restore(ctx, tx, data)
	})
}

func (s *Store) checkBackupPath(path string) error {
	target, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(target)); err == nil {
		target = filepath.Join(dir, filepath.Base(target))
	}
	info, err := os.Stat(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if info != nil && !info.Mode().IsRegular() {
		return fmt.Errorf("путь копии %q не является обычным файлом", path)
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal", ".log"} {
		protected := s.path + suffix
		if target == protected {
			return errors.New("путь копии совпадает с файлом базы или журнала")
		}
		if info != nil {
			other, err := os.Stat(protected)
			if err == nil && os.SameFile(info, other) {
				return errors.New("путь копии ссылается на файл базы или журнала")
			}
		}
	}
	return nil
}

func readBackup(r io.Reader, today string) (Snapshot, error) {
	var b struct {
		Version  int              `json:"version"`
		Debtors  []ledger.Debtor  `json:"debtors"`
		Payments []ledger.Payment `json:"payments"`
		Rates    *struct {
			Entries  []ledger.KeyRate `json:"entries"`
			Coverage json.RawMessage  `json:"coverage"`
		} `json:"key_rates"`
	}
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return Snapshot{}, fmt.Errorf("неверный JSON: %w", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Snapshot{}, errors.New("после копии обнаружены лишние данные")
	}
	if b.Version != backupVersion {
		return Snapshot{}, fmt.Errorf("неподдерживаемая версия копии: %d", b.Version)
	}
	if b.Debtors == nil || b.Payments == nil || b.Rates == nil || b.Rates.Entries == nil || b.Rates.Coverage == nil {
		return Snapshot{}, errors.New("неполная структура копии")
	}
	data := Snapshot{Debtors: b.Debtors, Payments: b.Payments, Rates: ledger.Rates{Entries: b.Rates.Entries}}
	coverage := json.NewDecoder(bytes.NewReader(b.Rates.Coverage))
	coverage.DisallowUnknownFields()
	if err := coverage.Decode(&data.Rates.Coverage); err != nil {
		return Snapshot{}, fmt.Errorf("неверное покрытие ключевой ставки: %w", err)
	}
	return data, validateSnapshot(data, today)
}

func validateSnapshot(data Snapshot, today string) error {
	if err := data.Rates.Validate(); err != nil {
		return err
	}
	debtors := make(map[int64]ledger.Debtor, len(data.Debtors))
	for _, d := range data.Debtors {
		if d.ID <= 0 || d.ID == math.MaxInt64 || debtors[d.ID].ID != 0 {
			return fmt.Errorf("некорректный или повторный ID должника: %d", d.ID)
		}
		if err := d.Validate(); err != nil {
			return fmt.Errorf("должник #%d: %w", d.ID, err)
		}
		debtors[d.ID] = d
	}
	ids := make(map[int64]bool, len(data.Payments))
	for _, p := range data.Payments {
		if p.ID <= 0 || p.ID == math.MaxInt64 || ids[p.ID] {
			return fmt.Errorf("некорректный или повторный ID платежа: %d", p.ID)
		}
		ids[p.ID] = true
		d, ok := debtors[p.DebtorID]
		if !ok {
			return fmt.Errorf("платёж #%d: должник #%d отсутствует", p.ID, p.DebtorID)
		}
		if err := validatePayment(d, p, today); err != nil {
			return fmt.Errorf("платёж #%d: %w", p.ID, err)
		}
	}
	byDebtor := data.PaymentsByDebtor()
	for _, d := range data.Debtors {
		if err := replayPayments(d, byDebtor[d.ID], data.Rates); err != nil {
			return fmt.Errorf("должник #%d: %w", d.ID, err)
		}
	}
	return nil
}

func restore(ctx context.Context, tx *sql.Tx, data Snapshot) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM debtors; DELETE FROM key_rates; DELETE FROM rate_coverage;"); err != nil {
		return err
	}
	for _, d := range data.Debtors {
		if _, err := tx.ExecContext(ctx, "INSERT INTO debtors ("+debtorColumns+") VALUES (?,?,?,?,?,?,?,?)",
			d.ID, d.Name, d.Principal, d.Issued, d.Mode, d.Rate, d.Period, d.Repayment); err != nil {
			return err
		}
	}
	for _, p := range data.Payments {
		if _, err := tx.ExecContext(ctx, "INSERT INTO payments ("+paymentColumns+") VALUES (?,?,?,?)",
			p.ID, p.DebtorID, p.Date, p.Amount); err != nil {
			return err
		}
	}
	for _, k := range data.Rates.Entries {
		if _, err := tx.ExecContext(ctx, "INSERT INTO key_rates (date,rate,source) VALUES (?,?,?)", k.Date, k.Rate, k.Source); err != nil {
			return err
		}
	}
	if c := data.Rates.Coverage; c != nil {
		if _, err := tx.ExecContext(ctx, "INSERT INTO rate_coverage VALUES (1,?,?)", c.From, c.Through); err != nil {
			return err
		}
	}
	return nil
}
