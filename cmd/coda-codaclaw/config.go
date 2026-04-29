package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/evanstern/coda-codaclaw/internal/ipc"
)

const defaultSocketPath = "~/.codaclaw/host.sock"

func expandHome(path string) string {
	if len(path) < 2 || path[:2] != "~/" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

func resolveSocketPath(config map[string]string) string {
	if v := os.Getenv("CODACLAW_HOST_SOCKET"); v != "" {
		return expandHome(v)
	}
	if config != nil {
		if v, ok := config["host_endpoint"]; ok && v != "" {
			return expandHome(v)
		}
	}
	return expandHome(defaultSocketPath)
}

func defaultClient() *ipc.Client {
	return &ipc.Client{
		SocketPath: resolveSocketPath(nil),
		Timeout:    30 * time.Second,
	}
}
