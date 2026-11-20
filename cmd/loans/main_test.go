package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestExitStatusAndStreams(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		code int
		want string
	}{{"--version", 0, version}, {"unknown", 1, "Ошибка:"}} {
		var out, stderr bytes.Buffer
		code := run([]string{"--db", filepath.Join(t.TempDir(), "loans.db"), tc.cmd}, strings.NewReader(""), &out, &stderr)
		if code != tc.code {
			t.Fatalf("%s: exit %d", tc.cmd, code)
		}
		if code == 0 {
			if !strings.Contains(out.String(), tc.want) || stderr.Len() != 0 {
				t.Fatal(out.String(), stderr.String())
			}
		} else if out.Len() != 0 || !strings.Contains(stderr.String(), tc.want) {
			t.Fatal(out.String(), stderr.String())
		}
	}
}
