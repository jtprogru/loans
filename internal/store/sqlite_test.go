package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jtprogru/loans/internal/ledger"
)

const today = "2025-03-31"

func openDB(t *testing.T, path string) *Store {
	t.Helper()
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func addLoan(t *testing.T, db *Store, amount, rate, repayment string) int64 {
	t.Helper()
	d, err := ledger.NewDebtor("Тёзка", amount, "2025-01-01", rate, "", repayment)
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.AddDebtor(t.Context(), d)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func pay(t *testing.T, db *Store, id int64, date string, amount int64) int64 {
	t.Helper()
	newID, err := db.AddPayment(t.Context(), ledger.Payment{DebtorID: id, Date: date, Amount: amount}, today)
	if err != nil {
		t.Fatal(err)
	}
	return newID
}

func get(t *testing.T, db *Store) Snapshot {
	t.Helper()
	data, err := db.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBackdatedPaymentRollsBack(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "loans.sqlite3"))
	id := addLoan(t, db, "100", "", "")
	other := addLoan(t, db, "100", "", "")
	last := pay(t, db, id, "2025-01-05", 9000)
	before := get(t, db)
	for _, p := range []ledger.Payment{
		{DebtorID: id, Date: "2025-01-02", Amount: 1100},
		{DebtorID: id, Date: "2024-12-31", Amount: 100},
		{DebtorID: id, Date: "2025-04-01", Amount: 100},
	} {
		if _, err := db.AddPayment(t.Context(), p, today); err == nil {
			t.Fatalf("unexpected accepted payment %+v", p)
		} else if p.Amount == 1100 && !strings.Contains(err.Error(), "платёж #1") {
			t.Fatalf("should identify the subsequent invalid payment, got %v", err)
		}
		if after := get(t, db); !reflect.DeepEqual(before, after) {
			t.Fatalf("rejected transaction changed data: %+v", after)
		}
	}
	if err := db.DeletePayment(t.Context(), other, last); err == nil {
		t.Fatal("deleted another debtor's payment")
	}
	if err := db.DeleteDebtor(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if data := get(t, db); len(data.Payments) != 0 || len(data.Debtors) != 1 || data.Debtors[0].ID != other {
		t.Fatalf("invalid cascade: %+v", data)
	}
}

func TestDeleteIsRolledBackIfItNeedsUnverifiedRates(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "loans.sqlite3"))
	id := addLoan(t, db, "365000", ledger.ExplicitKey, ledger.PrincipalFirst)
	if err := db.SetRate(t.Context(), ledger.KeyRate{Date: "2025-01-01", Rate: "10", Source: "условный тест"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetCoverage(t.Context(), ledger.Coverage{From: "2025-01-01", Through: "2025-01-02"}); err != nil {
		t.Fatal(err)
	}
	closePrincipal := pay(t, db, id, "2025-01-02", 36_500_000)
	pay(t, db, id, "2025-01-05", 10_000)
	before := get(t, db)
	err := db.DeletePayment(t.Context(), id, closePrincipal)
	var missing *ledger.MissingRates
	if !errors.As(err, &missing) || missing.From != "2025-01-03" || !reflect.DeepEqual(before, get(t, db)) {
		t.Fatalf("delete should not change data with unverified rates: %v", err)
	}
}

func TestConcurrentPaymentValidation(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "loans.sqlite3")
	first, second := openDB(t, path), openDB(t, path)
	id := addLoan(t, first, "100", "", "")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, db := range []*Store{first, second} {
		wg.Go(func() {
			_, err := db.AddPayment(ctx, ledger.Payment{DebtorID: id, Date: "2025-02-01", Amount: 7500}, today)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	ok := 0
	for err := range results {
		if err == nil {
			ok++
		} else if !strings.Contains(err.Error(), "превышает долг") {
			t.Fatalf("unexpected transaction error: %v", err)
		}
	}
	if ok != 1 || len(get(t, first).Payments) != 1 {
		t.Fatalf("expected exactly one accepted payment, got %d", ok)
	}
}

func TestBackupRoundTripAndRejectInvalidCopies(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "loans-backup.json")
	src := openDB(t, filepath.Join(dir, "source ?.sqlite3"))
	id := addLoan(t, src, "365000", ledger.ExplicitKey, "")
	for _, k := range []ledger.KeyRate{
		{Date: "2025-01-02", Rate: "10", Source: "условный тест"},
		{Date: "2025-01-03", Rate: "12", Source: "условный тест"},
	} {
		if err := src.SetRate(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	if err := src.SetCoverage(ctx, ledger.Coverage{From: "2025-01-02", Through: "2025-01-03"}); err != nil {
		t.Fatal(err)
	}
	paymentID := pay(t, src, id, "2025-01-03", 100000)
	if err := src.Export(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err := src.Export(ctx, src.path); err == nil {
		t.Fatal("export can overwrite the current database")
	}
	dst := openDB(t, filepath.Join(dir, "target.sqlite3"))
	if err := dst.Import(ctx, path, today, false); err != nil {
		t.Fatal(err)
	}
	want := get(t, src)
	if after := get(t, dst); !reflect.DeepEqual(want, after) {
		t.Fatalf("restored %+v, want %+v", after, want)
	}
	for _, date := range []string{"2025-01-01", "2025-01-02", "2025-01-03"} {
		want := balanceAt(t, ctx, src, id, date)
		got := balanceAt(t, ctx, dst, id, date)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: restored %+v, want %+v", date, got, want)
		}
	}
	if err := dst.Import(ctx, path, today, false); err == nil {
		t.Fatal("nonempty database replaced without flag")
	}
	valid, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := src.Export(cancelled, path); err == nil {
		t.Fatal("export ignored the cancelled context")
	}
	stillValid, err := os.ReadFile(path)
	if err != nil || string(stillValid) != string(valid) {
		t.Fatalf("failed export modified the existing backup: %v", err)
	}
	for _, input := range []string{
		"{", string(valid) + "{}", strings.Replace(string(valid), `"version": 1`, `"version": 99`, 1),
		strings.Replace(string(valid), `"debtor_id": 1`, `"debtor_id": 99`, 1),
		strings.Replace(string(valid), `"amount_kopecks": 100000`, `"amount_kopecks": 999999999`, 1),
		strings.Replace(string(valid), `"through": "2025-01-03"`, `"through": "2025-01-02"`, 1),
	} {
		bad := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(bad, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		if err := dst.Import(ctx, bad, today, true); err == nil {
			t.Fatalf("invalid backup accepted: %q", input)
		}
		if after := get(t, dst); !reflect.DeepEqual(want, after) {
			t.Fatalf("invalid import changed DB: %+v", after)
		}
	}
	newID := addLoan(t, dst, "1000", "0", "")
	if newID <= id || pay(t, dst, newID, "2025-01-03", 1000) <= paymentID {
		t.Fatal("new IDs conflict with restored IDs")
	}
	if err := dst.Import(ctx, path, today, true); err != nil {
		t.Fatal(err)
	}
	if after := get(t, dst); !reflect.DeepEqual(want, after) {
		t.Fatalf("replace did not restore exactly: %+v", after)
	}
}

func balanceAt(t *testing.T, ctx context.Context, db *Store, id int64, date string) ledger.Report {
	t.Helper()
	d, ps, rates, err := db.Loan(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	r, err := ledger.Replay(d, ps, rates, date, true)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
