// Package cli implements the non-interactive command tree and launches the TUI.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/jtprogru/loans/internal/audit"
	"github.com/jtprogru/loans/internal/ledger"
	"github.com/jtprogru/loans/internal/store"
	"github.com/jtprogru/loans/internal/tui"
	"github.com/spf13/cobra"
)

type session struct {
	path   string
	today  func() string
	logged bool
}

// Execute runs one CLI command with explicit input and output streams.
func Execute(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer, version string) error {
	s := session{today: ledger.Today}
	root := s.root(version)
	root.SetArgs(append([]string{}, args...))
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(stderr)
	cmd, err := root.ExecuteContextC(ctx)
	if err == nil || s.logged || s.path == "" {
		return err
	}
	// Parser and argument failures do not initialize SQLite.
	f, log, logErr := audit.Open(s.path)
	if logErr != nil {
		return errors.Join(err, fmt.Errorf("открытие журнала: %w", logErr))
	}
	audit.Action(log, cmd.CommandPath(), err)
	return errors.Join(err, f.Close())
}

func (s *session) root(version string) *cobra.Command {
	root := &cobra.Command{
		Use: "loans", Short: "Личный учёт займов", Version: version,
		Long:         "Личный учёт рублёвых займов. Без команды открывается TUI. Даты: YYYY-MM-DD, суммы в рублях (до двух знаков после разделителя).",
		SilenceUsage: true, SilenceErrors: true, Args: cobra.NoArgs,
	}
	// Leave the default empty when there is no home; an explicit --db still works.
	var defaultDB string
	if home, err := os.UserHomeDir(); err == nil {
		defaultDB = filepath.Join(home, ".config", "loans", "loans.sqlite3")
	}
	root.PersistentFlags().StringVar(&s.path, "db", defaultDB, "путь к базе SQLite")
	root.RunE = s.run(func(cmd *cobra.Command, db *store.Store, log *slog.Logger, _ []string) error {
		return tui.Run(cmd.Context(), db, log, cmd.InOrStdin(), cmd.OutOrStdout())
	})
	root.AddCommand(s.debtor(), s.balance(), s.payment(), s.backup(), s.keyRate())
	return root
}

func (s *session) run(fn func(*cobra.Command, *store.Store, *slog.Logger, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) (err error) {
		s.logged = true
		if s.path == "" {
			return errors.New("укажите непустой путь --db")
		}
		f, log, err := audit.Open(s.path)
		if err != nil {
			return fmt.Errorf("открытие журнала: %w", err)
		}
		defer func() { err = errors.Join(err, f.Close()) }()
		defer func() { audit.Action(log, cmd.CommandPath(), err) }()
		db, err := store.Open(cmd.Context(), s.path)
		if err != nil {
			return fmt.Errorf("открытие базы: %w", err)
		}
		defer func() { err = errors.Join(err, db.Close()) }()
		return fn(cmd, db, log, args)
	}
}

func (s *session) command(use, desc string, argc int, fn func(*cobra.Command, *store.Store, []string) error) *cobra.Command {
	return &cobra.Command{
		Use: use, Short: desc, Args: cobra.ExactArgs(argc),
		RunE: s.run(func(cmd *cobra.Command, db *store.Store, _ *slog.Logger, args []string) error {
			return fn(cmd, db, args)
		}),
	}
}

func positiveID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("ID %q должен быть положительным целым числом", s)
	}
	return id, nil
}

func write(cmd *cobra.Command, format string, args ...any) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(), format, args...)
	return err
}

func required(cmd *cobra.Command, names ...string) {
	for _, name := range names {
		// All names are registered on this command immediately before this call.
		_ = cmd.MarkFlagRequired(name)
	}
}
