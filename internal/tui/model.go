// Package tui implements the full-screen keyboard interface.
package tui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jtprogru/loans/internal/audit"
	"github.com/jtprogru/loans/internal/display"
	"github.com/jtprogru/loans/internal/ledger"
	"github.com/jtprogru/loans/internal/store"
)

type screen uint8

const (
	listScreen screen = iota
	cardScreen
	rateScreen
)

type listRow struct {
	d    ledger.Debtor
	text string
}

// Model is the terminal application's state machine.
type Model struct {
	ctx context.Context
	db  *store.Store
	log *slog.Logger
	now func() string

	screen        screen
	width, height int
	notice        string
	today         string

	rows     []listRow
	query    string
	selected int
	listSize [3]int
	rates    []string

	id           int64
	date         string
	d            ledger.Debtor
	payments     []ledger.Payment
	summary      string
	history      []ledger.Event
	historyLines []string
	offset       int

	form    *form
	confirm *confirmation
}

// Run opens the full-screen interface and restores the previous screen on exit.
func Run(ctx context.Context, db *store.Store, log *slog.Logger, in io.Reader, out io.Writer) error {
	m := New(ctx, db, log, ledger.Today)
	_, err := tea.NewProgram(m, tea.WithContext(ctx),
		tea.WithoutSignalHandler(), tea.WithInput(in), tea.WithOutput(out)).Run()
	return err
}

// New constructs the model with an explicit calendar clock.
func New(ctx context.Context, db *store.Store, log *slog.Logger, now func() string) *Model {
	return &Model{ctx: ctx, db: db, log: log, now: now, width: 100, height: 28}
}

type loadList struct{}

// Init schedules the initial list load after the terminal is prepared.
func (m *Model) Init() tea.Cmd { return func() tea.Msg { return loadList{} } }

// Update handles keyboard input and terminal resize.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case loadList:
		m.loadList()
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.wrapHistory()
	}
	key, isKey := msg.(tea.KeyPressMsg)
	if isKey && key.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.tooSmall() {
		return m, nil
	}
	if m.confirm != nil {
		if isKey {
			m.handleConfirmation(key)
		}
		return m, nil
	}
	if m.form != nil {
		return m, m.updateForm(msg)
	}
	if !isKey {
		return m, nil
	}
	if key.String() == "esc" || key.String() == "q" {
		if m.screen == listScreen {
			return m, tea.Quit
		}
		m.notice = ""
		m.loadList()
		return m, nil
	}
	switch m.screen {
	case listScreen:
		m.updateList(key.String())
	case cardScreen:
		m.updateCard(key.String())
	case rateScreen:
		m.updateRates(key.String())
	}
	return m, nil
}

func (m *Model) record(action string, err error) {
	audit.Action(m.log, "tui "+action, err)
	if err != nil {
		m.notice = "Ошибка: " + err.Error()
	}
}

func (m *Model) loadList() {
	m.screen = listScreen
	m.today = m.now()
	m.rows = nil
	data, err := m.db.Snapshot(m.ctx)
	m.record("debtor list", err)
	if err != nil {
		m.listSize = [3]int{}
		m.selected = 0
		return
	}
	m.listSize = [3]int{len(data.Debtors), len(data.Payments), len(data.Rates.Entries)}
	grouped := data.PaymentsByDebtor()
	search := strings.ToLower(m.query)
	for _, d := range data.Debtors {
		id := strconv.FormatInt(d.ID, 10)
		if !strings.Contains(strings.ToLower(d.Name), search) && id != strings.TrimPrefix(search, "#") {
			continue
		}
		r, err := ledger.Replay(d, grouped[d.ID], data.Rates, m.today, false)
		line := display.Summary(d, r)
		if err != nil {
			line = fmt.Sprintf("#%d %s | итог не рассчитан | %s", d.ID, d.Name, err)
			audit.Action(m.log, "tui balance", err)
		}
		m.rows = append(m.rows, listRow{d, line})
	}
	m.selected = max(0, min(m.selected, len(m.rows)-1))
}

func (m *Model) loadCard(id int64, date string) {
	d, ps, rates, err := m.db.Loan(m.ctx, id)
	if err != nil {
		m.record("balance", err)
		return
	}
	m.screen, m.id, m.date, m.d, m.payments, m.offset = cardScreen, id, date, d, ps, 0
	r, err := ledger.Replay(d, ps, rates, date, true)
	audit.Action(m.log, "tui balance", err)
	m.history = r.Events
	m.summary = display.Balance(r)
	if err != nil {
		m.summary = fmt.Sprintf("Дата расчёта: %s\nИтог не рассчитан: %s", date, err)
		// Historic allocations can still be shown when later accrual is unknown.
		if len(ps) != 0 {
			cutoff := min(m.date, display.LastPayment(d, ps))
			paid, err := ledger.Replay(d, ps, rates, cutoff, false)
			if err == nil {
				for i := range paid.Allocations {
					a := &paid.Allocations[i]
					m.history = append(m.history, ledger.Event{Date: a.Date, Allocation: a})
				}
			}
		}
	}
	m.wrapHistory()
}

func (m *Model) loadRates() {
	data, err := m.db.Snapshot(m.ctx)
	m.record("key-rate list", err)
	if err != nil {
		return
	}
	m.screen, m.offset, m.rates = rateScreen, 0, display.RateBook(data.Rates)
}

func (m *Model) updateList(key string) {
	switch key {
	case "up", "down", "pgup", "pgdown", "home", "end":
		m.selected = move(m.selected, len(m.rows), m.listPage(), key)
	case "enter":
		if len(m.rows) != 0 {
			m.notice = ""
			m.loadCard(m.rows[m.selected].d.ID, m.now())
		}
	case "/":
		m.openForm("Поиск по имени или ID", "search", []field{{"Запрос (пусто — все)", m.query}}, func(v []string) error {
			m.query, m.selected = v[0], 0
			m.loadList()
			return nil
		})
	case "n":
		m.debtorForm(nil)
	case "e", "i":
		m.backupForm(key == "i")
	case "k":
		m.notice = ""
		m.loadRates()
	case "r":
		m.notice = ""
		m.loadList()
	}
}

func (m *Model) updateCard(key string) {
	switch key {
	case "up", "down", "pgup", "pgdown", "home", "end":
		_, _, prefix, page := m.cardViewport()
		m.offset = move(m.offset, max(1, len(prefix)+len(m.historyLines)-page+1), page, key)
	case "d":
		m.openForm("Отчёт на дату", "report date", []field{{"Дата YYYY-MM-DD", m.date}}, func(v []string) error {
			if _, err := ledger.ParseDate(v[0]); err != nil {
				return err
			}
			m.loadCard(m.id, v[0])
			return nil
		})
	case "t":
		m.notice = ""
		m.loadCard(m.id, m.now())
	case "r":
		m.notice = ""
		m.loadCard(m.id, m.date)
	case "e":
		m.debtorForm(&m.d)
	case "p":
		m.paymentForm(nil)
	case "x", "P":
		m.selectPaymentForm(key == "P")
	case "D":
		id := m.id
		m.confirm = &confirmation{
			title:  fmt.Sprintf("Удалить должника #%d %s и все его платежи (%d)?", id, m.d.Name, len(m.payments)),
			action: "debtor delete", done: "Карточка и её платежи удалены",
			run:       func() error { return m.db.DeleteDebtor(m.ctx, id) },
			onSuccess: m.loadList,
		}
	}
}

func (m *Model) updateRates(key string) {
	switch key {
	case "up", "down", "pgup", "pgdown", "home", "end":
		_, _, lines, page := m.rateViewport()
		m.offset = move(m.offset, max(1, len(lines)-page+1), page, key)
	case "s":
		m.rateForm()
	case "c":
		m.coverageForm()
	case "r":
		m.notice = ""
		m.loadRates()
	}
}

func move(pos, count, page int, key string) int {
	switch key {
	case "up":
		pos--
	case "down":
		pos++
	case "pgup":
		pos -= page
	case "pgdown":
		pos += page
	case "home":
		pos = 0
	case "end":
		pos = count - 1
	}
	return max(0, min(count-1, pos))
}
