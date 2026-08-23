// Command opsgraph is a free, offline-first incident-context CLI for on-call engineers.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// exitError lets subcommands control the process exit code while still
// returning a normal error up through cobra.
//
// Exit codes (contract):
//
//	0 = success
//	1 = verification failed / golden mismatch / service not found
//	2 = invalid usage / config error / unexpected failure
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// fail is a helper to build an exitError with a formatted message.
func fail(code int, format string, args ...any) error {
	return &exitError{code: code, err: fmt.Errorf(format, args...)}
}

func exitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	// Unknown errors (including cobra usage errors) map to 2.
	return 2
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := newRootCmd()
	// Cobra's Print/Printf/Println fall back to stderr when Command.out is
	// unset. Wire stdout explicitly so redirects and CI smoke captures work.
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.SetArgs(rewriteRootArgs(root, os.Args[1:]))
	err := root.ExecuteContext(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "opsgraph:", err)
	}
	os.Exit(exitCodeFor(err))
}
