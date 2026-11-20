package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jtprogru/loans/internal/display"
	"github.com/jtprogru/loans/internal/ledger"
)

var (
	bannerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F0FDFA")).Background(lipgloss.Color("#115E59"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0")).Background(lipgloss.Color("#334155"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ECFEFF")).Background(lipgloss.Color("#155E75"))
	titleStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	warningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
)

func (m *Model) tooSmall() bool  { return m.width < 30 || m.height < 12 }
func (m *Model) innerWidth() int { return max(1, m.width-2) }
func (m *Model) inputWidth() int { return max(1, m.innerWidth()-4) }

// View draws into the alternate screen, preserving the shell on exit.
func (m *Model) View() tea.View {
	var content string
	switch {
	case m.tooSmall():
		lines := strings.Split(ansi.Hardwrap("Увеличьте окно до 30×12. Ctrl+C — выход", max(1, m.width), false), "\n")
		content = strings.Join(lines[:min(len(lines), max(0, m.height))], "\n")
	case m.confirm != nil:
		lines, footer, _, last := m.confirmViewport()
		content = m.frame("Подтверждение", nil, lines[min(last, m.confirm.offset):], footer)
	case m.form != nil:
		content = m.formView()
	case m.screen == cardScreen:
		content = m.cardView()
	case m.screen == rateScreen:
		content = m.rateView()
	default:
		content = m.listView()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m *Model) confirmViewport() (lines, footer []string, page, last int) {
	lines = m.wrap(m.confirm.title)
	footer = m.footer("y — подтвердить · n/Esc — отменить")
	if len(lines) > m.page(nil, footer) {
		footer = m.footer("↑↓ PgUp/PgDn — прокрутка · y в конце — подтвердить · n/Esc — отменить")
	}
	page = m.page(nil, footer)
	return lines, footer, page, max(0, len(lines)-page)
}

func (m *Model) listSections() (header, footer []string) {
	title := fmt.Sprintf("Должники: %d  ·  %s", len(m.rows), m.today)
	if m.query != "" {
		title += fmt.Sprintf("  ·  поиск %q", m.query)
	}
	header = []string{titleStyle.Render(ansi.Truncate(title, m.innerWidth(), "…"))}
	footer = m.footer("↑↓ PgUp/PgDn — список · Enter — карточка · / — поиск · n — создать\ne — копия · i — восстановить · k — ставки · r — обновить · q — выход")
	return header, footer
}

func (m *Model) listPage() int {
	header, footer := m.listSections()
	return m.page(header, footer)
}

func (m *Model) listView() string {
	header, footer := m.listSections()
	page := m.page(header, footer)
	var body []string
	if len(m.rows) == 0 {
		if m.query == "" {
			body = m.wrap("Пока нет займов. Нажмите n, чтобы создать первую карточку.")
		} else {
			body = m.wrap("Нет карточек по запросу")
		}
	}
	start := min(max(0, m.selected-page+1), max(0, len(m.rows)-page))
	for i := start; i < min(start+page, len(m.rows)); i++ {
		prefix := "  "
		if i == m.selected {
			prefix = "› "
		}
		line := ansi.Truncate(prefix+display.OneLine(ansi.Strip(m.rows[i].text)), m.innerWidth(), "…")
		if i == m.selected {
			line = selectedStyle.Width(m.innerWidth()).Render(line)
		}
		body = append(body, line)
	}
	return m.frame("Портфель", header, body, footer)
}

func (m *Model) cardViewport() (header, footer, prefix []string, page int) {
	header = m.wrap(fmt.Sprintf("#%d %s\n%s\nВыдано: %s ₽ · %s\n%s\nИстория (сначала новые дни):",
		m.d.ID, m.d.Name, m.summary, ledger.Money(m.d.Principal), m.d.Issued, m.d.Terms()))
	for i, s := range header {
		switch {
		case i == 0, i == len(header)-1, strings.HasPrefix(s, "Итого к возврату:"):
			header[i] = titleStyle.Render(s)
		case strings.HasPrefix(s, "Итог не рассчитан"):
			header[i] = warningStyle.Render(s)
		}
	}
	help := "↑↓ PgUp/PgDn — история · d — дата · t — сегодня · r — обновить\ne — править заём · p — платёж · P — править платёж · x — удалить платёж · D — удалить карточку · Esc — список"
	if m.width < 55 {
		help = "↑↓ история · d дата · t сегодня · r обновить\ne править · D удалить заём\nПлатёж: p новый · P править · x удалить\nEsc список"
	}
	footer = m.footer(help)
	page = m.page(header, footer)
	if page < 2 {
		// On small terminals scroll the whole card, not just the history.
		prefix, header = header, nil
		page = m.page(nil, footer)
	}
	return header, footer, prefix, page
}

func (m *Model) cardView() string {
	header, footer, prefix, page := m.cardViewport()
	n := len(prefix) + len(m.historyLines)
	start := min(m.offset, max(0, n-page))
	body := make([]string, 0, page)
	for i := start; i < min(start+page, n); i++ {
		if i < len(prefix) {
			body = append(body, prefix[i])
		} else {
			body = append(body, m.historyLines[i-len(prefix)])
		}
	}
	return m.frame("Карточка займа", header, body, footer)
}

func (m *Model) wrapHistory() {
	m.historyLines = m.historyLines[:0]
	if len(m.history) == 0 {
		m.historyLines = m.wrap("Нет начислений и платежей на выбранную дату")
		return
	}
	// Display newest dates first, preserving the order within each date.
	for end := len(m.history); end > 0; {
		start := end - 1
		for start > 0 && m.history[start-1].Date == m.history[end-1].Date {
			start--
		}
		for _, e := range m.history[start:end] {
			m.historyLines = append(m.historyLines, m.wrap(historyLine(e))...)
		}
		end = start
	}
}

func historyLine(e ledger.Event) string {
	if e.Allocation != nil {
		return display.Payment(*e.Allocation)
	}
	return fmt.Sprintf("%s  начислено %s ₽", e.Date, ledger.Money(e.Accrual))
}

func (m *Model) rateViewport() (header, footer, lines []string, page int) {
	header = []string{titleStyle.Render("Ключевая ставка ЦБ РФ")}
	footer = m.footer("↑↓ PgUp/PgDn — история · s — значение · c — покрытие · r — обновить · Esc — список")
	lines = m.wrap(strings.Join(m.rates, "\n"))
	return header, footer, lines, m.page(header, footer)
}

func (m *Model) rateView() string {
	header, footer, lines, page := m.rateViewport()
	start := min(m.offset, max(0, len(lines)-page))
	return m.frame("Справочник", header, lines[start:min(start+page, len(lines))], footer)
}

func (m *Model) formView() string {
	f := m.form
	header := m.wrap(f.title)
	footer := m.footer("Tab/Shift+Tab — поле · Enter — применить · Esc — отменить")
	visible := max(1, m.page(header, footer)/2)
	fields := f.activeFields()
	pos := slices.Index(fields, f.focused)
	start := min(max(0, pos-visible+1), max(0, len(fields)-visible))
	var body []string
	for _, i := range fields[start:min(start+visible, len(fields))] {
		input := f.inputs[i]
		input.SetWidth(m.inputWidth())
		label := f.fields[i].label
		if i == f.focused {
			label = titleStyle.Render("› " + label)
		} else {
			label = "  " + label
		}
		body = append(body, label, input.View())
	}
	return m.frame(fmt.Sprintf("Форма · %d/%d", pos+1, len(fields)), header, body, footer)
}

func (m *Model) wrap(text string) []string {
	return strings.Split(ansi.Wrap(ansi.Strip(text), m.innerWidth(), ""), "\n")
}

func (m *Model) footer(help string) []string {
	var out []string
	if m.notice != "" {
		lines := m.wrap(m.notice)
		if len(lines) > 3 {
			lines = lines[:3]
			lines[2] = ansi.Truncate(lines[2], m.innerWidth()-1, "") + "…"
		}
		style := successStyle
		if strings.HasPrefix(m.notice, "Ошибка:") {
			style = warningStyle
		}
		for _, line := range lines {
			out = append(out, style.Render(line))
		}
	}
	for _, line := range m.wrap(help) {
		out = append(out, helpStyle.Width(m.innerWidth()).Render(line))
	}
	return out
}

// Reserve one row for the section banner; help stays at the bottom even on empty screens.
func (m *Model) page(header, footer []string) int { return max(1, m.height-1-len(header)-len(footer)) }

func (m *Model) frame(section string, header, body, footer []string) string {
	bar := bannerStyle
	if m.confirm != nil {
		bar = bar.Background(lipgloss.Color("#9F1239"))
	}
	lines := make([]string, 0, m.height)
	lines = append(lines, bar.Width(m.width).Render(ansi.Truncate(" ЗАЙМЫ  /  "+section, m.width, "…")))
	add := func(s string) {
		s = ansi.Truncate(s, m.innerWidth(), "…")
		lines = append(lines, " "+s+strings.Repeat(" ", m.width-1-ansi.StringWidth(s)))
	}
	limit := max(1, m.height-len(footer))
	for _, group := range [][]string{header, body} {
		for _, line := range group {
			if len(lines) >= limit {
				break
			}
			add(line)
		}
	}
	blank := strings.Repeat(" ", m.width)
	for len(lines) < limit {
		lines = append(lines, blank)
	}
	for _, line := range footer {
		if len(lines) >= m.height {
			break
		}
		add(line)
	}
	return strings.Join(lines, "\n")
}
