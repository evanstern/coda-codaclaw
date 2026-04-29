package main

type stopRequest struct {
	Op        string `json:"op"`
	SessionID string `json:"session_id"`
}

type stopResponse struct {
	OK bool `json:"ok"`
}

func handleStop(args []string) int {
	if len(args) < 1 {
		return exitError("stop: <sessionID> is required")
	}
	sessionID := args[0]

	client := defaultClient()
	req := stopRequest{Op: "stop", SessionID: sessionID}
	var resp stopResponse
	if err := client.Call(req, &resp); err != nil {
		return handleCallError(err, client.SocketPath)
	}
	return 0
}
