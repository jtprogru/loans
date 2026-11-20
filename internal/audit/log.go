// Package audit records local action outcomes without logging loan or backup data.
package audit

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// Open opens the private journal next to the selected database.
func Open(dbPath string) (*os.File, *slog.Logger, error) {
	path, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, nil, err
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	logPath := path + ".log"
	if info, err := os.Lstat(logPath); err == nil {
		if !info.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("журнал %q не является обычным файлом", logPath)
		}
		for _, suffix := range []string{"", "-journal", "-shm", "-wal"} {
			if dbInfo, err := os.Stat(path + suffix); err == nil && os.SameFile(dbInfo, info) {
				return nil, nil, fmt.Errorf("журнал %q ссылается на файлы SQLite", logPath)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, nil, err
	}
	return f, slog.New(slog.NewJSONHandler(f, nil)), nil
}

// Action records the operation and whether it succeeded; error details stay in the UI.
func Action(log *slog.Logger, action string, err error) {
	if err != nil {
		log.Error("операция не выполнена", "action", action)
	} else {
		log.Info("операция выполнена", "action", action)
	}
}
