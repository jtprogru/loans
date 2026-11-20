package cli

import (
	"errors"
	"fmt"

	"github.com/jtprogru/loans/internal/display"
	"github.com/jtprogru/loans/internal/ledger"
	"github.com/jtprogru/loans/internal/store"
	"github.com/spf13/cobra"
)

func (s *session) debtor() *cobra.Command {
	group := &cobra.Command{Use: "debtor", Short: "Карточки должников"}
	var name, amount, date, rate, period, repayment string
	add := s.command("add", "Создать карточку должника", 0, func(cmd *cobra.Command, db *store.Store, _ []string) error {
		for _, flag := range []struct{ name, value string }{{"rate", rate}, {"period", period}, {"repayment", repayment}} {
			if cmd.Flags().Changed(flag.name) && flag.value == "" {
				return fmt.Errorf("--%s: пустое значение", flag.name)
			}
		}
		d, err := ledger.NewDebtor(name, amount, date, rate, period, repayment)
		if err != nil {
			return err
		}
		id, err := db.AddDebtor(cmd.Context(), d)
		if err != nil {
			return err
		}
		return write(cmd, "Должник #%d\nУсловия: %s\n", id, d.Terms())
	})
	flags := add.Flags()
	flags.StringVar(&name, "name", "", "имя должника")
	flags.StringVar(&amount, "amount", "", "выданная сумма в рублях")
	flags.StringVar(&date, "date", "", "дата выдачи YYYY-MM-DD")
	flags.StringVar(&rate, "rate", "", "процент (0 — беспроцентный) или key-rate; без флага — правило по сумме")
	flags.StringVar(&period, "period", "", "период числовой ставки: year (по умолчанию), 1m, 21d")
	flags.StringVar(&repayment, "repayment", "", "очередность: interest-first (по умолчанию) или principal-first")
	required(add, "name", "amount", "date")

	list := s.command("list", "Показать долги на сегодня", 0, s.list)
	var yes bool
	del := s.command("delete ID", "Удалить карточку и все её платежи", 1, func(cmd *cobra.Command, db *store.Store, args []string) error {
		if !yes {
			return errors.New("удаление карточки и всех её платежей требует --yes")
		}
		id, err := positiveID(args[0])
		if err != nil {
			return err
		}
		if err := db.DeleteDebtor(cmd.Context(), id); err != nil {
			return err
		}
		return write(cmd, "Должник #%d и его платежи удалены\n", id)
	})
	del.Flags().BoolVar(&yes, "yes", false, "подтвердить удаление")
	group.AddCommand(add, list, del)
	return group
}

func (s *session) list(cmd *cobra.Command, db *store.Store, _ []string) error {
	data, err := db.Snapshot(cmd.Context())
	if err != nil {
		return err
	}
	today := s.today()
	if err := write(cmd, "Дата расчёта: %s\n", today); err != nil {
		return err
	}
	if len(data.Debtors) == 0 {
		return write(cmd, "Нет должников\n")
	}
	grouped := data.PaymentsByDebtor()
	var failures []error
	for _, d := range data.Debtors {
		r, err := ledger.Replay(d, grouped[d.ID], data.Rates, today, false)
		if err != nil {
			failures = append(failures, fmt.Errorf("должник #%d: %w", d.ID, err))
			if err := write(cmd, "#%d %s | итог не рассчитан\n", d.ID, d.Name); err != nil {
				return err
			}
			continue
		}
		if err := write(cmd, "%s\n", display.Summary(d, r)); err != nil {
			return err
		}
	}
	return errors.Join(failures...)
}

func (s *session) balance() *cobra.Command {
	var date string
	c := s.command("balance ID", "Рассчитать остаток на дату", 1, func(cmd *cobra.Command, db *store.Store, args []string) error {
		id, err := positiveID(args[0])
		if err != nil {
			return err
		}
		asOf := date
		if !cmd.Flags().Changed("date") {
			asOf = s.today()
		}
		d, ps, rates, err := db.Loan(cmd.Context(), id)
		if err != nil {
			return err
		}
		r, err := ledger.Replay(d, ps, rates, asOf, false)
		if err != nil {
			return err
		}
		return write(cmd, "#%d %s\n%s\nУсловия: %s\n", d.ID, d.Name, display.Balance(r), d.Terms())
	})
	c.Flags().StringVar(&date, "date", "", "дата расчёта YYYY-MM-DD (по умолчанию сегодня)")
	return c
}

func (s *session) payment() *cobra.Command {
	group := &cobra.Command{Use: "payment", Short: "Записи о платежах"}
	var amount, date string
	add := s.command("add ID", "Записать платёж должника", 1, func(cmd *cobra.Command, db *store.Store, args []string) error {
		id, err := positiveID(args[0])
		if err != nil {
			return err
		}
		n, err := ledger.ParseMoney(amount)
		if err != nil {
			return err
		}
		p, err := db.AddPayment(cmd.Context(), ledger.Payment{DebtorID: id, Amount: n, Date: date}, s.today())
		if err != nil {
			return err
		}
		return write(cmd, "Платёж #%d записан\n", p)
	})
	add.Flags().StringVar(&amount, "amount", "", "сумма платежа в рублях")
	add.Flags().StringVar(&date, "date", "", "дата платежа YYYY-MM-DD")
	required(add, "amount", "date")

	list := s.command("list ID", "Показать платежи и их распределение", 1, func(cmd *cobra.Command, db *store.Store, args []string) error {
		id, err := positiveID(args[0])
		if err != nil {
			return err
		}
		d, ps, rates, err := db.Loan(cmd.Context(), id)
		if err != nil {
			return err
		}
		if len(ps) == 0 {
			return write(cmd, "Нет платежей\n")
		}
		r, err := ledger.Replay(d, ps, rates, display.LastPayment(d, ps), false)
		if err != nil {
			return err
		}
		for _, a := range r.Allocations {
			if err := write(cmd, "%s\n", display.Payment(a)); err != nil {
				return err
			}
		}
		return nil
	})

	var paymentID int64
	var yes bool
	del := s.command("delete ID", "Удалить платёж и пересчитать долг", 1, func(cmd *cobra.Command, db *store.Store, args []string) error {
		if !yes {
			return errors.New("удаление платежа требует --yes")
		}
		id, err := positiveID(args[0])
		if err != nil {
			return err
		}
		if paymentID <= 0 {
			return errors.New("--payment-id должен быть положительным")
		}
		if err := db.DeletePayment(cmd.Context(), id, paymentID); err != nil {
			return err
		}
		return write(cmd, "Платёж #%d удалён\n", paymentID)
	})
	del.Flags().BoolVar(&yes, "yes", false, "подтвердить удаление")
	del.Flags().Int64Var(&paymentID, "payment-id", 0, "ID платежа")
	required(del, "payment-id")
	group.AddCommand(add, list, del)
	return group
}
