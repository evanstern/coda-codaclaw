package main

import (
	"encoding/json"
	"fmt"
)

type healthRequest struct {
	Op        string `json:"op"`
	SessionID string `json:"session_id"`
}

type healthResponse struct {
	OK      bool   `json:"ok"`
	State   string `json:"state"`
	Healthy bool   `json:"healthy"`
	Detail  string `json:"detail"`
}

type healthOutput struct {
	State   string `json:"State"`
	Healthy bool   `json:"Healthy"`
	Detail  string `json:"Detail"`
}

func handleHealth(args []string) int {
	if len(args) < 1 {
		return exitError("health: <sessionID> is required")
	}
	sessionID := args[0]

	client := defaultClient()
	req := healthRequest{Op: "health", SessionID: sessionID}
	var resp healthResponse
	if err := client.Call(req, &resp); err != nil {
		return handleCallError(err, client.SocketPath)
	}

	out := healthOutput{
		State:   resp.State,
		Healthy: resp.Healthy,
		Detail:  resp.Detail,
	}
	data, err := json.Marshal(out)
	if err != nil {
		return exitError(fmt.Sprintf("health: marshal output: %s", err))
	}
	fmt.Println(string(data))
	return 0
}
