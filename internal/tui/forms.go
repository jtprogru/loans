package tui

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/jtprogru/loans/internal/ledger"
)

type field struct{ label, value string }

type form struct {
	title, action string
	fields        []field
	inputs        []textinput.Model
	visible       func(int) bool // nil when all fields apply
	focused       int
	submit        func([]string) error
}

func (f *form) activeFields() []int {
	var indices []int
	for i := range f.inputs {
		if f.visible == nil || f.visible(i) {
			indices = append(indices, i)
		}
	}
	return indices
}

type confirmation struct {
	offset              int
	title, action, done string
	run                 func() error
	onSuccess           func()
}

func (m *Model) openForm(title, action string, fields []field, submit func([]string) error) {
	m.notice = ""
	f := &form{title: title, action: action, fields: fields, submit: submit}
	for _, field := range fields {
		in := textinput.New()
		in.SetVirtualCursor(true)
		styles := in.Styles()
		styles.Focused.Prompt = titleStyle
		styles.Cursor.Blink = false
		in.SetStyles(styles)
		in.SetValue(field.value)
		f.inputs = append(f.inputs, in)
	}
	f.inputs[0].Focus()
	m.form = f
}

func (m *Model) updateForm(msg tea.Msg) tea.Cmd {
	f := m.form
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			m.form, m.notice = nil, ""
			return nil
		case "tab", "shift+tab":
			f.inputs[f.focused].Blur()
			step := 1
			if key.String() == "shift+tab" {
				step = -1
			}
			fields := f.activeFields()
			pos := slices.Index(fields, f.focused)
			f.focused = fields[(pos+step+len(fields))%len(fields)]
			return f.inputs[f.focused].Focus()
		case "enter":
			values := make([]string, len(f.inputs))
			for _, i := range f.activeFields() {
				values[i] = strings.TrimSpace(f.inputs[i].Value())
			}
			m.notice = ""
			err := f.submit(values)
			m.record(f.action, err)
			if err == nil && m.form == f {
				m.form = nil
			}
			return nil
		}
	}
	var cmd tea.Cmd
	f.inputs[f.focused].SetWidth(m.inputWidth())
	f.inputs[f.focused], cmd = f.inputs[f.focused].Update(msg)
	return cmd
}

func (m *Model) handleConfirmation(key tea.KeyPressMsg) {
	switch key.String() {
	case "n", "esc":
		m.confirm, m.notice = nil, ""
	case "up", "down", "pgup", "pgdown", "home", "end":
		_, _, page, last := m.confirmViewport()
		m.confirm.offset = move(min(m.confirm.offset, last), last+1, page, key.String())
	case "y":
		_, _, _, last := m.confirmViewport()
		if m.confirm.offset < last {
			return
		}
		c := m.confirm
		m.confirm = nil
		err := c.run()
		m.record(c.action, err)
		if err == nil {
			m.notice = c.done
			c.onSuccess()
		}
	}
}

func (m *Model) debtorForm(original *ledger.Debtor) {
	title, action := "Новый должник", "debtor add"
	name, amount, date, rate, period, order := "", "", m.now(), "", "", ledger.InterestFirst
	if original != nil {
		title, action = fmt.Sprintf("Изменить карточку #%d", original.ID), "debtor edit"
		name, amount, date, period, order = original.Name, ledger.Money(original.Principal), original.Issued, original.Period, original.Repayment
		switch original.Mode {
		case ledger.Fixed:
			rate = original.Rate
		case ledger.ExplicitKey:
			rate = ledger.ExplicitKey
		}
	}
	m.openForm(title, action, []field{
		{"Имя", name}, {"Сумма выдачи, ₽", amount}, {"Дата выдачи YYYY-MM-DD", date},
		{"Ставка, %: число, key-rate или пусто (по сумме)", rate}, {"Период: пусто — год; примеры: year, 1m, 21d", period},
		{"Очередность: interest-first или principal-first", order},
	}, func(v []string) error {
		d, err := ledger.NewDebtor(v[0], v[1], v[2], v[3], v[4], v[5])
		if err != nil {
			return err
		}
		if original != nil {
			d.ID = original.ID
			if err := m.db.UpdateDebtor(m.ctx, *original, d); err != nil {
				return err
			}
			m.notice = fmt.Sprintf("Карточка #%d обновлена", d.ID)
			m.loadCard(d.ID, m.date)
			return nil
		}
		id, err := m.db.AddDebtor(m.ctx, d)
		if err != nil {
			return err
		}
		m.notice = fmt.Sprintf("Должник #%d создан", id)
		m.loadCard(id, m.now())
		return nil
	})
	const rateField, periodField = 3, 4
	f := m.form
	f.visible = func(i int) bool {
		if i != periodField {
			return true
		}
		_, err := ledger.NormalizeRate(strings.TrimSpace(f.inputs[rateField].Value()))
		return err == nil
	}
}

func (m *Model) paymentForm(original *ledger.Payment) {
	title, action, amount, date := "Новый платёж", "payment add", "", m.now()
	if original != nil {
		title, action = fmt.Sprintf("Изменить платёж #%d", original.ID), "payment edit"
		amount, date = ledger.Money(original.Amount), original.Date
	}
	m.openForm(title, action, []field{{"Сумма, ₽", amount}, {"Дата YYYY-MM-DD", date}}, func(v []string) error {
		amount, err := ledger.ParseMoney(v[0])
		if err != nil {
			return err
		}
		if original != nil {
			if err := m.db.UpdatePayment(m.ctx, *original, v[1], amount, m.now()); err != nil {
				return err
			}
			m.notice = fmt.Sprintf("Платёж #%d обновлён", original.ID)
			m.loadCard(m.id, m.date)
			return nil
		}
		id, err := m.db.AddPayment(m.ctx, ledger.Payment{DebtorID: m.id, Date: v[1], Amount: amount}, m.now())
		if err != nil {
			return err
		}
		m.notice = fmt.Sprintf("Платёж #%d записан", id)
		m.loadCard(m.id, m.now())
		return nil
	})
}

func (m *Model) selectPaymentForm(edit bool) {
	title := "Удалить платёж"
	if edit {
		title = "Изменить платёж"
	}
	m.openForm(title, "payment select", []field{{"ID платежа", ""}}, func(v []string) error {
		id, err := strconv.ParseInt(v[0], 10, 64)
		if err != nil || id <= 0 {
			return errors.New("введите положительный ID платежа")
		}
		for _, p := range m.payments {
			if p.ID == id {
				if edit {
					m.paymentForm(&p)
					return nil
				}
				debtor := m.id
				m.confirm = &confirmation{
					title: fmt.Sprintf("Удалить платёж #%d от %s (%s ₽) должника #%d? Последующие платежи будут пересчитаны.",
						id, p.Date, ledger.Money(p.Amount), debtor),
					action: "payment delete", done: "Платёж удалён",
					run:       func() error { return m.db.DeletePayment(m.ctx, debtor, id) },
					onSuccess: func() { m.loadCard(debtor, m.now()) },
				}
				return nil
			}
		}
		return fmt.Errorf("у должника #%d нет платежа #%d", m.id, id)
	})
}

func (m *Model) backupForm(restore bool) {
	title, action := "Экспорт копии", "backup export"
	if restore {
		title, action = "Восстановление копии", "backup select"
	}
	m.openForm(title, action, []field{{"Путь к JSON", "loans-backup.json"}}, func(v []string) error {
		path := v[0]
		if path == "" {
			return errors.New("укажите путь к копии")
		}
		if !restore {
			if err := m.db.Export(m.ctx, path); err != nil {
				return err
			}
			m.notice = fmt.Sprintf("Копия записана в %q", path)
			return nil
		}
		m.confirm = &confirmation{
			title: fmt.Sprintf("Восстановить %q? Текущая база будет полностью заменена: карточки (%d), платежи (%d), справочник (%d) и его проверенный интервал.",
				path, m.listSize[0], m.listSize[1], m.listSize[2]),
			action: "backup import", done: "Копия восстановлена",
			run:       func() error { return m.db.Import(m.ctx, path, m.now(), true) },
			onSuccess: func() { m.query, m.selected = "", 0; m.loadList() },
		}
		return nil
	})
}

func (m *Model) rateForm() {
	m.openForm("Значение ключевой ставки", "key-rate set", []field{
		{"Действует с YYYY-MM-DD", m.now()}, {"Годовая ставка, %", ""}, {"Источник", ledger.KeyRateSource},
	}, func(v []string) error {
		rate, err := ledger.NormalizeRate(v[1])
		if err != nil {
			return err
		}
		if err := m.db.SetRate(m.ctx, ledger.KeyRate{Date: v[0], Rate: rate, Source: v[2]}); err != nil {
			return err
		}
		m.notice = "Значение сохранено"
		m.loadRates()
		return nil
	})
}

func (m *Model) coverageForm() {
	m.openForm("Подтвердить полноту истории ключевой ставки", "key-rate coverage", []field{
		{"Проверено с YYYY-MM-DD", ""}, {"Проверено по YYYY-MM-DD включительно", m.now()},
	}, func(v []string) error {
		if err := m.db.SetCoverage(m.ctx, ledger.Coverage{From: v[0], Through: v[1]}); err != nil {
			return err
		}
		m.notice = "Проверенный интервал сохранён"
		m.loadRates()
		return nil
	})
}
