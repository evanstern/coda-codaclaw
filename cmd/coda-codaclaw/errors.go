package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/evanstern/coda-codaclaw/internal/ipc"
)

func exitError(msg string) int {
	fmt.Fprintln(os.Stderr, msg)
	return 1
}

func handleCallError(err error, socketPath string) int {
	if errors.Is(err, ipc.ErrHostUnreachable) {
		return exitError(fmt.Sprintf("host unreachable: %s", socketPath))
	}
	return exitError(err.Error())
}
