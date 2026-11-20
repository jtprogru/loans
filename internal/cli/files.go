package cli

import (
	"github.com/jtprogru/loans/internal/display"
	"github.com/jtprogru/loans/internal/ledger"
	"github.com/jtprogru/loans/internal/store"
	"github.com/spf13/cobra"
)

func (s *session) backup() *cobra.Command {
	group := &cobra.Command{Use: "backup", Short: "Полные резервные копии JSON"}
	export := s.command("export PATH", "Экспортировать полную копию", 1, func(cmd *cobra.Command, db *store.Store, args []string) error {
		if err := db.Export(cmd.Context(), args[0]); err != nil {
			return err
		}
		return write(cmd, "Копия записана в %q\n", args[0])
	})
	var replace bool
	restore := s.command("import PATH", "Восстановить копию без слияния данных", 1, func(cmd *cobra.Command, db *store.Store, args []string) error {
		if err := db.Import(cmd.Context(), args[0], s.today(), replace); err != nil {
			return err
		}
		return write(cmd, "База восстановлена из %q\n", args[0])
	})
	restore.Flags().BoolVar(&replace, "replace", false, "подтвердить полную замену карточек, платежей и справочника")
	group.AddCommand(export, restore)
	return group
}

func (s *session) keyRate() *cobra.Command {
	group := &cobra.Command{Use: "key-rate", Short: "Локальный справочник ключевой ставки ЦБ РФ"}
	var date, rate, source string
	set := s.command("set", "Записать значение с даты его действия", 0, func(cmd *cobra.Command, db *store.Store, _ []string) error {
		value, err := ledger.NormalizeRate(rate)
		if err != nil {
			return err
		}
		if err := db.SetRate(cmd.Context(), ledger.KeyRate{Date: date, Rate: value, Source: source}); err != nil {
			return err
		}
		return write(cmd, "Ключевая ставка с %s: %s%%\n", date, value)
	})
	set.Flags().StringVar(&date, "date", "", "дата вступления ставки в силу YYYY-MM-DD")
	set.Flags().StringVar(&rate, "rate", "", "годовая ставка в процентах")
	set.Flags().StringVar(&source, "source", ledger.KeyRateSource, "источник значения")
	required(set, "date", "rate")

	var from, through string
	coverage := s.command("coverage", "Подтвердить полноту данных за проверенный интервал", 0, func(cmd *cobra.Command, db *store.Store, _ []string) error {
		if err := db.SetCoverage(cmd.Context(), ledger.Coverage{From: from, Through: through}); err != nil {
			return err
		}
		return write(cmd, "Проверенный интервал: %s — %s\n", from, through)
	})
	coverage.Flags().StringVar(&from, "from", "", "начало интервала YYYY-MM-DD включительно")
	coverage.Flags().StringVar(&through, "through", "", "конец интервала YYYY-MM-DD включительно")
	required(coverage, "from", "through")
	list := s.command("list", "Показать значения, источники и покрытие", 0, func(cmd *cobra.Command, db *store.Store, _ []string) error {
		data, err := db.Snapshot(cmd.Context())
		if err != nil {
			return err
		}
		for _, line := range display.RateBook(data.Rates) {
			if err := write(cmd, "%s\n", line); err != nil {
				return err
			}
		}
		return nil
	})
	group.AddCommand(set, coverage, list)
	return group
}
