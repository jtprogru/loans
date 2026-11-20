package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandLifecycle(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "loans.db")
	copyPath := filepath.Join(dir, "backup.json")
	for _, step := range []struct {
		args []string
		want string
	}{
		{[]string{"debtor", "add", "--name", "Иван", "--amount", "100", "--date", "2025-01-01", "--rate", "0"}, "явно беспроцентный"},
		{[]string{"debtor", "list"}, "итог 100.00"},
		{[]string{"payment", "list", "1"}, "Нет платежей"},
		{[]string{"payment", "add", "1", "--amount", "40", "--date", "2025-01-01"}, "Платёж #1 записан"},
		{[]string{"backup", "export", copyPath}, "Копия записана"},
		{[]string{"payment", "delete", "1", "--payment-id", "1", "--yes"}, "Платёж #1 удалён"},
		{[]string{"debtor", "delete", "1", "--yes"}, "Должник #1"},
		{[]string{"backup", "import", copyPath}, "База восстановлена"},
		{[]string{"debtor", "list"}, "итог 60.00"},
		{[]string{"key-rate", "list"}, "не задан"},
	} {
		text(t, mustRun(t, db, step.args...), step.want)
	}
}

func TestCommandValidation(t *testing.T) {
	db := filepath.Join(t.TempDir(), "loans.db")
	mustRun(t, db, "debtor", "add", "--name", "Ира", "--amount", "100001", "--date", "2025-01-01")
	for _, tc := range []struct {
		args string
		want string
	}{
		{"debtor list", "нет проверенных данных"}, {"debtor add", "required"}, {"debtor add --name Ира --amount 1 --date 2025-01-01 --period year", "только с числовой"}, {"debtor add --name Ира --amount 1 --date 2025-01-01 --rate=", "пустое значение"},
		{"debtor delete x --yes", "положительным"}, {"debtor delete 50 --yes", "не найден"}, {"balance 0", "положительным"}, {"balance 5", "не найден"},
		{"payment add -1 --date 2025-01-01 --amount 1", "unknown"}, {"payment add x --date 2025-01-01 --amount 1", "положительным"}, {"payment add 1 --date 2025-01-01 --amount abc", "сумма"}, {"payment add 1 --date вчера --amount 1", "дата"},
		{"payment list x", "положительным"}, {"payment list 55", "не найден"}, {"payment delete 1 --payment-id 1", "требует --yes"}, {"payment delete x --payment-id 1 --yes", "положительным"}, {"payment delete 1 --payment-id 0 --yes", "положительным"}, {"payment delete 1 --payment-id 5 --yes", "нет платежа"},
		{"key-rate set --date 2025-01-01 --rate foo", "ставка"}, {"key-rate set --date bad --rate 10", "дата"}, {"key-rate coverage --from 2025-01-01 --through 2025-01-02", "нужно значение"},
	} {
		t.Run(tc.args, func(t *testing.T) {
			_, err := command(t, db, strings.Fields(tc.args)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
	for _, args := range [][]string{{"backup", "export", db}, {"backup", "import", db}} {
		_, err := command(t, db, args...)
		if err == nil || !strings.Contains(err.Error(), "совпадает") {
			t.Fatalf("%v: %v", args, err)
		}
	}
}
