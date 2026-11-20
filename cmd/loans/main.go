package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/jtprogru/loans/internal/cli"
)

var version = "dev"

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, in io.Reader, out, stderr io.Writer) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err := cli.Execute(ctx, args, in, out, stderr, version)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "Ошибка:", err) // Best effort: the command already failed.
		return 1
	}
	return 0
}
