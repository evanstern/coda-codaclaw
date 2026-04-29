package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/evanstern/coda-codaclaw/internal/ipc"
)

type startRequest struct {
	Op     string            `json:"op"`
	Agent  string            `json:"agent"`
	Config map[string]string `json:"config,omitempty"`
}

type startResponse struct {
	OK        bool   `json:"ok"`
	SessionID string `json:"session_id"`
}

func handleStart(args []string) int {
	var agent string
	for _, arg := range args {
		if strings.HasPrefix(arg, "--agent=") {
			agent = strings.TrimPrefix(arg, "--agent=")
		}
	}
	if agent == "" {
		return exitError("start: --agent=<name> is required")
	}

	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		return exitError(fmt.Sprintf("start: reading stdin: %s", err))
	}

	config := make(map[string]string)
	if len(stdinData) > 0 {
		if err := json.Unmarshal(stdinData, &config); err != nil {
			return exitError(fmt.Sprintf("start: parsing ProviderConfig: %s", err))
		}
	}

	socketPath := resolveSocketPath(config)
	client := &ipc.Client{
		SocketPath: socketPath,
		Timeout:    30 * time.Second,
	}

	req := startRequest{
		Op:     "start",
		Agent:  agent,
		Config: config,
	}

	var resp startResponse
	if err := client.Call(req, &resp); err != nil {
		return handleCallError(err, socketPath)
	}

	fmt.Println(resp.SessionID)
	return 0
}
