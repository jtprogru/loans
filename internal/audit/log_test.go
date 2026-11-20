package audit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJournalStoresOutcomeNotPrivateDetails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "loans.db")
	for _, err := range []error{nil, errors.New("private details")} {
		f, log, e := Open(path)
		if e != nil {
			t.Fatal(e)
		}
		Action(log, "debtor edit", err)
		if e := f.Close(); e != nil {
			t.Fatal(e)
		}
	}
	b, err := os.ReadFile(path + ".log")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Count(s, "debtor edit") != 2 || strings.Contains(s, "private details") || !strings.Contains(s, "ERROR") || !strings.Contains(s, "INFO") {
		t.Fatal(s)
	}
}
func TestJournalRejectsInvalidPaths(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := Open(filepath.Join(dir, "bad\x00path")); err == nil {
		t.Fatal("invalid journal path accepted")
	}
	path := filepath.Join(dir, "db")
	if err := os.WriteFile(path, []byte("database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, path+".log"); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if _, _, err := Open(path); err == nil {
		t.Fatal("accepted database as its own journal")
	}
	if _, _, err := Open(filepath.Join(path, "sub")); err == nil {
		t.Fatal("accepted a regular file as a directory")
	}
	other := filepath.Join(dir, "other")
	if err := os.Mkdir(other+".log", 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(other); err == nil {
		t.Fatal("opened directory as journal")
	}
}
