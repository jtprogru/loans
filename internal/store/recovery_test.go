package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jtprogru/loans/internal/ledger"
)

func TestBackupRejectsMalformedRecordsBeforeReplace(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	db := openDB(t, filepath.Join(dir, "loans.db"))
	id := addLoan(t, db, "50", "", "")
	pay(t, db, id, today, 500)
	before := get(t, db)
	p := filepath.Join(dir, "copy.json")
	if err := db.Export(ctx, p); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ field, invalid string }{
		{`"entries": []`, `"entries": null`}, {`"coverage": null`, `"coverage": {"unknown":1}`},
		{`"coverage": null`, `"coverage":{"from":"bad","through":"bad"}`}, {`"principal_kopecks": 5000`, `"principal_kopecks": 0`}, {`"id": 1`, `"id": 0`},
		{`"amount_kopecks": 500`, `"amount_kopecks": 0`}, {`"id": 1,
      "debtor_id"`, `"id": -1,
      "debtor_id"`},
	} {
		invalid := strings.Replace(string(b), tc.field, tc.invalid, 1)
		if invalid == string(b) {
			t.Fatal("invalid fixture", tc.field)
		}
		if err := os.WriteFile(p, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if err := db.Import(ctx, p, today, true); err == nil {
			t.Fatalf("accepted %s", tc.invalid)
		}
		if !reflect.DeepEqual(before, get(t, db)) {
			t.Fatalf("replacement modified database: %s", tc.field)
		}
	}
}

func TestRestoreRollsBackMidTransaction(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	db := openDB(t, filepath.Join(dir, "loans.db"))
	id := addLoan(t, db, "50", "", "")
	pay(t, db, id, today, 500)
	if err := db.SetRate(ctx, ledger.KeyRate{Date: today, Rate: "10", Source: "тест"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetCoverage(ctx, ledger.Coverage{From: today, Through: today}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "copy.json")
	if err := db.Export(ctx, p); err != nil {
		t.Fatal(err)
	}
	addLoan(t, db, "60", "", "")
	before := get(t, db)
	for _, table := range []string{"debtors", "payments", "key_rates", "rate_coverage"} {
		t.Run(table, func(t *testing.T) {
			if _, err := db.db.ExecContext(ctx, "CREATE TRIGGER reject BEFORE INSERT ON "+table+" BEGIN SELECT RAISE(ABORT,'test abort'); END;"); err != nil {
				t.Fatal(err)
			}
			err := db.Import(ctx, p, today, true)
			if err == nil || !strings.Contains(err.Error(), "test abort") {
				t.Fatalf("wanted write failure, got %v", err)
			}
			if !reflect.DeepEqual(before, get(t, db)) {
				t.Fatal("partially applied backup")
			}
			if _, err := db.db.ExecContext(ctx, "DROP TRIGGER reject"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRateCorrectionCannotInvalidateExistingPayment(t *testing.T) {
	ctx := t.Context()
	db := openDB(t, filepath.Join(t.TempDir(), "loans.db"))
	id := addLoan(t, db, "36500", ledger.ExplicitKey, "")
	rate := ledger.KeyRate{Date: "2025-01-02", Rate: "10", Source: "тест"}
	if err := db.SetRate(ctx, rate); err != nil {
		t.Fatal(err)
	}
	if err := db.SetCoverage(ctx, ledger.Coverage{From: rate.Date, Through: rate.Date}); err != nil {
		t.Fatal(err)
	}
	pay(t, db, id, rate.Date, 3651000)
	before := get(t, db)
	rate.Rate = "0"
	if err := db.SetRate(ctx, rate); err == nil || !strings.Contains(err.Error(), "платёж #1") {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, get(t, db)) {
		t.Fatal("invalid rate persisted")
	}
}
