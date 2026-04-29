package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type deliverMessage struct {
	ID   string `json:"id"`
	From string `json:"from"`
	Body string `json:"body"`
}

type deliverRequest struct {
	Op        string         `json:"op"`
	SessionID string         `json:"session_id"`
	Message   deliverMessage `json:"message"`
}

type deliverResponse struct {
	OK        bool `json:"ok"`
	Delivered bool `json:"delivered"`
}

// inboundMessage is the shape coda sends on stdin for deliver.
// Body is []byte which JSON-encodes as base64.
type inboundMessage struct {
	ID   string `json:"ID"`
	From string `json:"From"`
	Body []byte `json:"Body"`
}

func handleDeliver(args []string) int {
	if len(args) < 1 {
		return exitError("deliver: <sessionID> is required")
	}
	sessionID := args[0]

	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		return exitError(fmt.Sprintf("deliver: reading stdin: %s", err))
	}

	var msg inboundMessage
	if err := json.Unmarshal(stdinData, &msg); err != nil {
		return exitError(fmt.Sprintf("deliver: parsing message: %s", err))
	}

	// Body arrives as base64-decoded []byte from JSON unmarshal.
	// Convert to utf-8 string for the host wire.
	bodyStr := string(msg.Body)

	client := defaultClient()
	req := deliverRequest{
		Op:        "deliver",
		SessionID: sessionID,
		Message: deliverMessage{
			ID:   msg.ID,
			From: msg.From,
			Body: bodyStr,
		},
	}

	var resp deliverResponse
	if err := client.Call(req, &resp); err != nil {
		return handleCallError(err, client.SocketPath)
	}

	fmt.Println(`{"delivered":true}`)
	return 0
}
