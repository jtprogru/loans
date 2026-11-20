package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jtprogru/loans/internal/ledger"
)

func TestEditPreservesLoanHistory(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "loans.db"))
	ctx := t.Context()
	id := addLoan(t, db, "100", "", "")
	pay(t, db, id, "2025-01-02", 5000)
	later := pay(t, db, id, "2025-01-03", 4000)
	before := get(t, db)
	d := before.Debtors[0]
	p := before.Payments[0]
	for _, tc := range []struct {
		name string
		edit func() error
		why  string
	}{
		{"issue date after all payments", func() error { after := d; after.Issued = "2025-01-04"; return db.UpdateDebtor(ctx, d, after) }, "платёж #1"},
		{"smaller loan", func() error { after := d; after.Principal = 8100; return db.UpdateDebtor(ctx, d, after) }, "платёж #2"},
		{"unknown repayment order", func() error { after := d; after.Repayment = "unknown"; return db.UpdateDebtor(ctx, d, after) }, "порядок"},
		{"different debtor ID", func() error { after := d; after.ID++; return db.UpdateDebtor(ctx, d, after) }, "ID"},
		{"missing debtor", func() error { old := d; old.ID++; return db.UpdateDebtor(ctx, old, old) }, "не найден"},
		{"old loan revision", func() error { old := d; old.Name = "старая версия"; return db.UpdateDebtor(ctx, old, d) }, "изменилась"},
		{"later overpayment", func() error { return db.UpdatePayment(ctx, p, p.Date, 7000, today) }, "платёж #2"},
		{"future date", func() error { return db.UpdatePayment(ctx, p, "2025-04-01", 1, today) }, "будущем"},
		{"stale payment", func() error { old := p; old.Amount--; return db.UpdatePayment(ctx, old, p.Date, 1, today) }, "изменён"},
		{"missing owner", func() error { old := p; old.DebtorID++; return db.UpdatePayment(ctx, old, p.Date, 1, today) }, "не найден"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.edit()
			if err == nil || !strings.Contains(err.Error(), tc.why) {
				t.Fatalf("got %v want %s", err, tc.why)
			}
			if !reflect.DeepEqual(before, get(t, db)) {
				t.Fatal("failed edit changed data")
			}
		})
	}
	after := d
	after.Name = "Новое имя"
	if err := db.UpdateDebtor(ctx, d, after); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdatePayment(ctx, p, "2025-01-03", 5500, today); err != nil {
		t.Fatal(err)
	}
	r := balanceAt(t, ctx, db, id, today)
	if r.Total != 500 || len(r.Allocations) != 2 || r.Allocations[0].ID != p.ID || r.Allocations[1].ID != later {
		t.Fatalf("updated report %+v", r)
	}
}

func TestStoreReportsPathAndValidationErrors(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-database")
	if err := os.WriteFile(file, []byte(strings.Repeat("not SQLite", 100)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, file, filepath.Join(file, "sub")} {
		if db, err := Open(ctx, path); err == nil {
			_ = db.Close()
			t.Fatalf("opened invalid database %s", path)
		}
	}
	db := openDB(t, filepath.Join(dir, "good"))
	if _, err := db.AddDebtor(ctx, ledger.Debtor{}); err == nil {
		t.Fatal("saved invalid loan")
	}
	if err := db.SetRate(ctx, ledger.KeyRate{}); err == nil {
		t.Fatal("accepted invalid rate")
	}
	if err := db.DeleteDebtor(ctx, 7); err == nil {
		t.Fatal("deleted nonexistent loan")
	}
	if err := db.DeletePayment(ctx, 7, 2); err == nil {
		t.Fatal("deleted payment of nonexistent debtor")
	}
	if _, err := db.AddPayment(ctx, ledger.Payment{DebtorID: 7}, today); err == nil {
		t.Fatal("added payment without debtor")
	}
	id := addLoan(t, db, "100", "", "")
	for _, p := range []ledger.Payment{{DebtorID: id, Date: today, Amount: 0}, {DebtorID: id, Date: "bad", Amount: 1}} {
		if _, err := db.AddPayment(ctx, p, today); err == nil {
			t.Fatal("saved invalid transfer")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	d := get(t, db).Debtors[0]
	if _, err := db.AddDebtor(cancelled, d); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
