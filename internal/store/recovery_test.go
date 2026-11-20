package store

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jtprogru/loans/internal/ledger"
)

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
