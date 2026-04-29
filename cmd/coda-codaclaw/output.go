package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type outputRequest struct {
	Op        string `json:"op"`
	SessionID string `json:"session_id"`
	Since     string `json:"since,omitempty"`
}

type hostMessage struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Body      string `json:"body"`
	Cursor    string `json:"cursor"`
}

type outputResponse struct {
	OK             bool          `json:"ok"`
	Messages       []hostMessage `json:"messages"`
	LastCodaSender *string       `json:"last_coda_sender"`
}

type textEnvelope struct {
	Text string `json:"text"`
}

type codaMessage struct {
	ID        string    `json:"ID"`
	From      string    `json:"From"`
	To        string    `json:"To"`
	Type      string    `json:"Type"`
	Body      []byte    `json:"Body"`
	CreatedAt time.Time `json:"CreatedAt"`
	Cursor    string    `json:"Cursor"`
}

func decodeOutputBody(raw string) []byte {
	var env textEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		fmt.Fprintf(os.Stderr, "output: body envelope parse failed, returning raw: %s\n", err)
		return []byte(raw)
	}
	if env.Text != "" {
		return []byte(env.Text)
	}
	return []byte(raw)
}

func handleOutput(args []string) int {
	if len(args) < 1 {
		return exitError("output: <sessionID> is required")
	}
	sessionID := args[0]

	var since string
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "--since=") {
			since = strings.TrimPrefix(arg, "--since=")
		}
	}

	client := defaultClient()
	req := outputRequest{Op: "output", SessionID: sessionID}
	if since != "" {
		req.Since = since
	}

	var resp outputResponse
	if err := client.Call(req, &resp); err != nil {
		return handleCallError(err, client.SocketPath)
	}

	// Card C (codaclaw#9 at 4ddb009) added last_coda_sender per-session
	// on the output response. It's the most recent inbound coda-side
	// sender; we use it for Message.To on every row in this response so
	// coda can route replies back to whoever originally sent the
	// triggering message. From stays empty for v0 — the host doesn't
	// surface the agent name on the output wire yet.
	var to string
	if resp.LastCodaSender != nil {
		to = *resp.LastCodaSender
	}

	messages := make([]codaMessage, 0, len(resp.Messages))
	for _, m := range resp.Messages {
		createdAt, err := time.Parse(time.RFC3339, m.Timestamp)
		if err != nil {
			createdAt, err = time.Parse(time.RFC3339Nano, m.Timestamp)
			if err != nil {
				return exitError(fmt.Sprintf("output: parse timestamp %q: %s", m.Timestamp, err))
			}
		}

		messages = append(messages, codaMessage{
			ID:        m.ID,
			From:      "",
			To:        to,
			Type:      "note",
			Body:      decodeOutputBody(m.Body),
			CreatedAt: createdAt,
			Cursor:    m.Cursor,
		})
	}

	data, err := json.Marshal(messages)
	if err != nil {
		return exitError(fmt.Sprintf("output: marshal output: %s", err))
	}
	fmt.Println(string(data))
	return 0
}
