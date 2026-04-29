package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func startFakeHost(t *testing.T, handler func(req json.RawMessage) json.RawMessage) string {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 64*1024)
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				resp := handler(json.RawMessage(buf[:n]))
				data, _ := json.Marshal(resp)
				data = append(data, '\n')
				c.Write(data)
			}(conn)
		}
	}()
	return sockPath
}

func TestCall_HappyPath(t *testing.T) {
	type testReq struct {
		Op   string `json:"op"`
		Name string `json:"name"`
	}
	type testResp struct {
		OK    bool   `json:"ok"`
		Value string `json:"value"`
	}

	sockPath := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		return json.RawMessage(`{"ok":true,"value":"hello"}`)
	})

	client := &Client{SocketPath: sockPath, Timeout: 5 * time.Second}
	var resp testResp
	err := client.Call(testReq{Op: "test", Name: "world"}, &resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Value != "hello" {
		t.Errorf("got value %q, want %q", resp.Value, "hello")
	}
}

func TestCall_ErrorResponse(t *testing.T) {
	sockPath := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		return json.RawMessage(`{"ok":false,"error":"session not found"}`)
	})

	client := &Client{SocketPath: sockPath, Timeout: 5 * time.Second}
	var resp struct{ OK bool }
	err := client.Call(map[string]string{"op": "stop"}, &resp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := "host error: session not found"
	if err.Error() != want {
		t.Errorf("got error %q, want %q", err.Error(), want)
	}
}

func TestCall_HostUnreachable(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "nonexistent.sock")
	client := &Client{SocketPath: sockPath, Timeout: 5 * time.Second}
	var resp struct{ OK bool }
	err := client.Call(map[string]string{"op": "test"}, &resp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !isHostUnreachable(err) {
		t.Errorf("expected ErrHostUnreachable, got: %v", err)
	}
}

func isHostUnreachable(err error) bool {
	return err != nil && fmt.Sprintf("%v", err) != "" &&
		os.IsNotExist(err) == false &&
		err.Error()[:len("host unreachable")] == "host unreachable"
}

func TestCall_RequestFormat(t *testing.T) {
	var captured json.RawMessage
	sockPath := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		captured = make(json.RawMessage, len(req))
		copy(captured, req)
		return json.RawMessage(`{"ok":true}`)
	})

	type req struct {
		Op    string `json:"op"`
		Agent string `json:"agent"`
	}
	client := &Client{SocketPath: sockPath, Timeout: 5 * time.Second}
	var resp struct{ OK bool }
	err := client.Call(req{Op: "start", Agent: "kit"}, &resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(captured, &parsed); err != nil {
		t.Fatalf("parse captured request: %v", err)
	}
	if parsed["op"] != "start" {
		t.Errorf("got op %q, want %q", parsed["op"], "start")
	}
	if parsed["agent"] != "kit" {
		t.Errorf("got agent %q, want %q", parsed["agent"], "kit")
	}
}
