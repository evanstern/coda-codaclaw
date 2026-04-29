// Package ipc provides a client for CodaClaw's Unix-socket IPC protocol.
// Each call opens a fresh socket connection, sends one JSON-delimited request,
// reads one JSON-delimited response, and closes. No pooling, no caching.
package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

// ErrHostUnreachable is returned when the Unix socket cannot be opened.
var ErrHostUnreachable = errors.New("host unreachable")

// Client talks to a CodaClaw host over a Unix socket.
type Client struct {
	SocketPath string
	Timeout    time.Duration
}

// envelope is used to check the ok/error fields of every response.
type envelope struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Call sends req as a single JSON line to the socket, reads one JSON line
// back, and unmarshals into resp. Returns ErrHostUnreachable if the socket
// can't be dialed.
func (c *Client) Call(req any, resp any) error {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	conn, err := net.DialTimeout("unix", c.SocketPath, timeout)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrHostUnreachable, c.SocketPath)
	}
	defer conn.Close()

	deadline := time.Now().Add(timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("write request: %w", err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var env envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return fmt.Errorf("unmarshal response envelope: %w", err)
	}
	if !env.OK {
		return fmt.Errorf("host error: %s", env.Error)
	}

	if err := json.Unmarshal(line, resp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	return nil
}
