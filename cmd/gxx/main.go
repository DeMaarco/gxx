package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"gxx/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	code := app.Run(
		ctx,
		os.Args[1:],
		os.Stdin,
		os.Stdout,
		os.Stderr,
		isTerminal(os.Stdin) && isTerminal(os.Stderr),
	)
	stop()
	os.Exit(code)
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
