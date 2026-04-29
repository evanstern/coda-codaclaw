package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var testBinary string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "coda-codaclaw-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mktemp: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	testBinary = filepath.Join(tmp, "coda-codaclaw")
	cmd := exec.Command("go", "build", "-o", testBinary, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build: %s\n%v\n", out, err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

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

func runPlugin(t *testing.T, socketPath string, args []string, stdin string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(testBinary, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Env = append(os.Environ(), "CODACLAW_HOST_SOCKET="+socketPath)

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("run: %v", err)
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// --- start tests ---

func TestStart_HappyPath(t *testing.T) {
	var captured map[string]interface{}
	sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		json.Unmarshal(req, &captured)
		return json.RawMessage(`{"ok":true,"session_id":"sess-123"}`)
	})

	stdout, _, code := runPlugin(t, sock, []string{"start", "--agent=kit"}, `{"host_endpoint":"/ignored"}`)
	if code != 0 {
		t.Fatalf("exit code %d, want 0", code)
	}
	if got := strings.TrimSpace(stdout); got != "sess-123" {
		t.Errorf("stdout = %q, want %q", got, "sess-123")
	}
	if captured["op"] != "start" {
		t.Errorf("op = %v, want start", captured["op"])
	}
	if captured["agent"] != "kit" {
		t.Errorf("agent = %v, want kit", captured["agent"])
	}
}

func TestStart_ErrorResponse(t *testing.T) {
	sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		return json.RawMessage(`{"ok":false,"error":"agent not found"}`)
	})

	_, stderr, code := runPlugin(t, sock, []string{"start", "--agent=bad"}, "")
	if code == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr, "agent not found") {
		t.Errorf("stderr = %q, want to contain 'agent not found'", stderr)
	}
}

func TestStart_HostUnreachable(t *testing.T) {
	badPath := filepath.Join(t.TempDir(), "no.sock")
	_, stderr, code := runPlugin(t, badPath, []string{"start", "--agent=kit"}, "")
	if code == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr, "host unreachable:") {
		t.Errorf("stderr = %q, want to contain 'host unreachable:'", stderr)
	}
}

func TestStart_MissingAgent(t *testing.T) {
	_, stderr, code := runPlugin(t, "/dev/null", []string{"start"}, "")
	if code == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr, "--agent=") {
		t.Errorf("stderr = %q, want to contain '--agent='", stderr)
	}
}

// --- stop tests ---

func TestStop_HappyPath(t *testing.T) {
	var captured map[string]interface{}
	sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		json.Unmarshal(req, &captured)
		return json.RawMessage(`{"ok":true}`)
	})

	stdout, _, code := runPlugin(t, sock, []string{"stop", "sess-123"}, "")
	if code != 0 {
		t.Fatalf("exit code %d, want 0", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if captured["op"] != "stop" {
		t.Errorf("op = %v, want stop", captured["op"])
	}
	if captured["session_id"] != "sess-123" {
		t.Errorf("session_id = %v, want sess-123", captured["session_id"])
	}
}

func TestStop_ErrorResponse(t *testing.T) {
	sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		return json.RawMessage(`{"ok":false,"error":"session not found"}`)
	})

	_, stderr, code := runPlugin(t, sock, []string{"stop", "bad-id"}, "")
	if code == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr, "session not found") {
		t.Errorf("stderr = %q, want to contain 'session not found'", stderr)
	}
}

func TestStop_HostUnreachable(t *testing.T) {
	badPath := filepath.Join(t.TempDir(), "no.sock")
	_, stderr, code := runPlugin(t, badPath, []string{"stop", "sess-123"}, "")
	if code == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr, "host unreachable:") {
		t.Errorf("stderr = %q, want to contain 'host unreachable:'", stderr)
	}
}

// --- deliver tests ---

func TestDeliver_HappyPath(t *testing.T) {
	var captured map[string]interface{}
	sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		json.Unmarshal(req, &captured)
		return json.RawMessage(`{"ok":true,"delivered":true}`)
	})

	// "Hello, world!" base64 = "SGVsbG8sIHdvcmxkIQ=="
	stdinMsg := `{"ID":"msg-1","From":"ash","Body":"SGVsbG8sIHdvcmxkIQ=="}`
	stdout, _, code := runPlugin(t, sock, []string{"deliver", "sess-123"}, stdinMsg)
	if code != 0 {
		t.Fatalf("exit code %d, want 0", code)
	}
	if !strings.Contains(stdout, `"delivered":true`) {
		t.Errorf("stdout = %q, want to contain delivered:true", stdout)
	}
	if captured["op"] != "deliver" {
		t.Errorf("op = %v, want deliver", captured["op"])
	}

	msg, ok := captured["message"].(map[string]interface{})
	if !ok {
		t.Fatalf("message not a map: %T", captured["message"])
	}
	if msg["body"] != "Hello, world!" {
		t.Errorf("body = %v, want 'Hello, world!' (utf-8, not base64)", msg["body"])
	}
}

func TestDeliver_BodyEncoding_RoundTrip(t *testing.T) {
	testCases := []string{
		"Hello, world!",
		"日本語テスト",
		"emoji: 🎉🚀",
		"newline\nand\ttab",
		"",
	}

	for _, text := range testCases {
		t.Run(text, func(t *testing.T) {
			var capturedBody string
			sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
				var parsed struct {
					Message struct {
						Body string `json:"body"`
					} `json:"message"`
				}
				json.Unmarshal(req, &parsed)
				capturedBody = parsed.Message.Body
				return json.RawMessage(`{"ok":true,"delivered":true}`)
			})

			// Build the coda-side message: Body is []byte which JSON-encodes as base64
			type codaMsg struct {
				ID   string `json:"ID"`
				From string `json:"From"`
				Body []byte `json:"Body"`
			}
			msg := codaMsg{ID: "msg-1", From: "ash", Body: []byte(text)}
			stdinData, _ := json.Marshal(msg)

			_, _, code := runPlugin(t, sock, []string{"deliver", "sess-1"}, string(stdinData))
			if code != 0 {
				t.Fatalf("exit code %d, want 0", code)
			}

			if capturedBody != text {
				t.Errorf("host received body %q, want %q (utf-8, not base64)", capturedBody, text)
			}
		})
	}
}

func TestDeliver_ErrorResponse(t *testing.T) {
	sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		return json.RawMessage(`{"ok":false,"error":"session not found"}`)
	})

	stdinMsg := `{"ID":"msg-1","From":"ash","Body":"dGVzdA=="}`
	_, stderr, code := runPlugin(t, sock, []string{"deliver", "sess-bad"}, stdinMsg)
	if code == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr, "session not found") {
		t.Errorf("stderr = %q, want to contain 'session not found'", stderr)
	}
}

func TestDeliver_HostUnreachable(t *testing.T) {
	badPath := filepath.Join(t.TempDir(), "no.sock")
	stdinMsg := `{"ID":"msg-1","From":"ash","Body":"dGVzdA=="}`
	_, stderr, code := runPlugin(t, badPath, []string{"deliver", "sess-123"}, stdinMsg)
	if code == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr, "host unreachable:") {
		t.Errorf("stderr = %q, want to contain 'host unreachable:'", stderr)
	}
}

// --- health tests ---

func TestHealth_HappyPath(t *testing.T) {
	sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		return json.RawMessage(`{"ok":true,"state":"running","healthy":true,"detail":""}`)
	})

	stdout, _, code := runPlugin(t, sock, []string{"health", "sess-123"}, "")
	if code != 0 {
		t.Fatalf("exit code %d, want 0", code)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &out); err != nil {
		t.Fatalf("parse stdout: %v", err)
	}
	if out["State"] != "running" {
		t.Errorf("State = %v, want running", out["State"])
	}
	if out["Healthy"] != true {
		t.Errorf("Healthy = %v, want true", out["Healthy"])
	}
}

func TestHealth_ErrorResponse(t *testing.T) {
	sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		return json.RawMessage(`{"ok":false,"error":"session not found"}`)
	})

	_, stderr, code := runPlugin(t, sock, []string{"health", "sess-bad"}, "")
	if code == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr, "session not found") {
		t.Errorf("stderr = %q, want to contain 'session not found'", stderr)
	}
}

func TestHealth_HostUnreachable(t *testing.T) {
	badPath := filepath.Join(t.TempDir(), "no.sock")
	_, stderr, code := runPlugin(t, badPath, []string{"health", "sess-123"}, "")
	if code == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr, "host unreachable:") {
		t.Errorf("stderr = %q, want to contain 'host unreachable:'", stderr)
	}
}

// --- output tests ---

func TestOutput_HappyPath(t *testing.T) {
	var captured map[string]interface{}
	sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		json.Unmarshal(req, &captured)
		return json.RawMessage(`{"ok":true,"messages":[{"id":"out-1","timestamp":"2024-01-15T10:30:00Z","type":"text","body":"{\"text\":\"PONG\"}","cursor":"seq-5"}],"last_coda_sender":"ash"}`)
	})

	stdout, _, code := runPlugin(t, sock, []string{"output", "sess-123", "--since=seq-3"}, "")
	if code != 0 {
		t.Fatalf("exit code %d, want 0", code)
	}

	if captured["op"] != "output" {
		t.Errorf("op = %v, want output", captured["op"])
	}
	if captured["since"] != "seq-3" {
		t.Errorf("since = %v, want seq-3", captured["since"])
	}

	var msgs []map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &msgs); err != nil {
		t.Fatalf("parse stdout: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}

	msg := msgs[0]
	if msg["ID"] != "out-1" {
		t.Errorf("ID = %v, want out-1", msg["ID"])
	}
	if msg["Type"] != "note" {
		t.Errorf("Type = %v, want note", msg["Type"])
	}
	if msg["Cursor"] != "seq-5" {
		t.Errorf("Cursor = %v, want seq-5", msg["Cursor"])
	}
	if msg["To"] != "ash" {
		t.Errorf("To = %v, want ash (from last_coda_sender)", msg["To"])
	}

	bodyB64, ok := msg["Body"].(string)
	if !ok {
		t.Fatalf("Body is not string: %T", msg["Body"])
	}
	if bodyB64 != "UE9ORw==" {
		t.Errorf("Body = %q, want %q (base64 of 'PONG')", bodyB64, "UE9ORw==")
	}
}

func TestOutput_LastCodaSender_BroadcastToAllRows(t *testing.T) {
	sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		return json.RawMessage(`{"ok":true,"messages":[` +
			`{"id":"o1","timestamp":"2024-01-15T10:30:00Z","type":"text","body":"{\"text\":\"hi\"}","cursor":"seq-1"},` +
			`{"id":"o2","timestamp":"2024-01-15T10:30:01Z","type":"text","body":"{\"text\":\"there\"}","cursor":"seq-2"}` +
			`],"last_coda_sender":"zach"}`)
	})

	stdout, _, code := runPlugin(t, sock, []string{"output", "sess-1"}, "")
	if code != 0 {
		t.Fatalf("exit code %d, want 0", code)
	}

	var msgs []map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &msgs); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	for i, m := range msgs {
		if m["To"] != "zach" {
			t.Errorf("msg[%d].To = %v, want zach", i, m["To"])
		}
	}
}

func TestOutput_LastCodaSender_NullOrAbsent(t *testing.T) {
	cases := map[string]string{
		"null":   `{"ok":true,"messages":[{"id":"o1","timestamp":"2024-01-15T10:30:00Z","type":"text","body":"{\"text\":\"hi\"}","cursor":"seq-1"}],"last_coda_sender":null}`,
		"absent": `{"ok":true,"messages":[{"id":"o1","timestamp":"2024-01-15T10:30:00Z","type":"text","body":"{\"text\":\"hi\"}","cursor":"seq-1"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
				return json.RawMessage(body)
			})
			stdout, _, code := runPlugin(t, sock, []string{"output", "sess-1"}, "")
			if code != 0 {
				t.Fatalf("exit code %d, want 0", code)
			}
			var msgs []map[string]interface{}
			if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &msgs); err != nil {
				t.Fatalf("parse: %v", err)
			}
			if msgs[0]["To"] != "" {
				t.Errorf("To = %v, want empty string", msgs[0]["To"])
			}
		})
	}
}

func TestOutput_BodyDecoding_RoundTrip(t *testing.T) {
	sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		return json.RawMessage(`{"ok":true,"messages":[{"id":"out-1","timestamp":"2024-01-15T10:30:00Z","type":"text","body":"{\"text\":\"PONG\"}","cursor":"seq-1"}]}`)
	})

	stdout, _, code := runPlugin(t, sock, []string{"output", "sess-123"}, "")
	if code != 0 {
		t.Fatalf("exit code %d, want 0", code)
	}

	var msgs []struct {
		Body []byte `json:"Body"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &msgs); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if string(msgs[0].Body) != "PONG" {
		t.Errorf("decoded Body = %q, want %q", string(msgs[0].Body), "PONG")
	}
}

func TestOutput_BodyDecoding_FallbackRaw(t *testing.T) {
	sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		return json.RawMessage(`{"ok":true,"messages":[{"id":"out-1","timestamp":"2024-01-15T10:30:00Z","type":"text","body":"not json at all","cursor":"seq-1"}]}`)
	})

	stdout, stderr, code := runPlugin(t, sock, []string{"output", "sess-123"}, "")
	if code != 0 {
		t.Fatalf("exit code %d, want 0", code)
	}

	var msgs []struct {
		Body []byte `json:"Body"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &msgs); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if string(msgs[0].Body) != "not json at all" {
		t.Errorf("decoded Body = %q, want %q", string(msgs[0].Body), "not json at all")
	}
	if !strings.Contains(stderr, "envelope parse failed") {
		t.Errorf("expected stderr warning about envelope parse, got: %q", stderr)
	}
}

func TestOutput_NoSinceCursor(t *testing.T) {
	var captured map[string]interface{}
	sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		json.Unmarshal(req, &captured)
		return json.RawMessage(`{"ok":true,"messages":[]}`)
	})

	_, _, code := runPlugin(t, sock, []string{"output", "sess-123"}, "")
	if code != 0 {
		t.Fatalf("exit code %d, want 0", code)
	}

	if _, ok := captured["since"]; ok {
		t.Errorf("since field should be omitted when no --since flag, got %v", captured["since"])
	}
}

func TestOutput_ErrorResponse(t *testing.T) {
	sock := startFakeHost(t, func(req json.RawMessage) json.RawMessage {
		return json.RawMessage(`{"ok":false,"error":"session not found"}`)
	})

	_, stderr, code := runPlugin(t, sock, []string{"output", "sess-bad"}, "")
	if code == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr, "session not found") {
		t.Errorf("stderr = %q, want to contain 'session not found'", stderr)
	}
}

func TestOutput_HostUnreachable(t *testing.T) {
	badPath := filepath.Join(t.TempDir(), "no.sock")
	_, stderr, code := runPlugin(t, badPath, []string{"output", "sess-123"}, "")
	if code == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr, "host unreachable:") {
		t.Errorf("stderr = %q, want to contain 'host unreachable:'", stderr)
	}
}

// --- edge cases ---

func TestUnknownSubcommand(t *testing.T) {
	_, _, code := runPlugin(t, "/dev/null", []string{"foobar"}, "")
	if code == 0 {
		t.Fatal("expected non-zero exit code for unknown subcommand")
	}
}

func TestNoSubcommand(t *testing.T) {
	cmd := exec.Command(testBinary)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit code")
	}
}
