package app_test

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// serveProcess runs `docmcp serve` as a real child process, because the property
// under test is about which stream each byte lands on — only a real process with
// real pipes can prove that.
type serveProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufReader
	stderr *strings.Builder
}

func startServe(t *testing.T, dataDir string) *serveProcess {
	t.Helper()

	binary := buildBinary(t)

	// The child process needs an explicit config: without one it would read the
	// real user config, and its embedder would have to be a real model.
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath,
		[]byte("[embedding]\nprovider = \"fake\"\nmodel = \"fake-model\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(binary, "serve",
		"--data-dir", dataDir,
		"--config", configPath,
	)
	cmd.Env = append(os.Environ(), "DOCMCP_LOG_LEVEL=error")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}

	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}

	t.Cleanup(func() {
		stdin.Close()
		cmd.Process.Kill()
		cmd.Wait()
	})

	return &serveProcess{
		cmd:    cmd,
		stdin:  stdin,
		stdout: newBufReader(stdoutPipe),
		stderr: &stderr,
	}
}

// initialize performs the MCP handshake and returns the server's response.
func (s *serveProcess) initialize(t *testing.T) map[string]any {
	t.Helper()

	s.send(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1"},
		},
	})

	response := s.read(t)
	// The initialized notification is required before any tool call.
	s.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})

	return response
}

func (s *serveProcess) call(t *testing.T, id int, name string, args map[string]any) map[string]any {
	t.Helper()

	s.send(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	})
	return s.read(t)
}

func (s *serveProcess) send(t *testing.T, message map[string]any) {
	t.Helper()

	line, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := s.stdin.Write(append(line, '\n')); err != nil {
		t.Fatalf("write to serve: %v", err)
	}
}

func (s *serveProcess) read(t *testing.T) map[string]any {
	t.Helper()

	line, err := s.stdout.ReadLine(5 * time.Second)
	if err != nil {
		t.Fatalf("read from serve: %v\nstderr so far: %s", err, s.stderr.String())
	}

	var response map[string]any
	if err := json.Unmarshal([]byte(line), &response); err != nil {
		t.Fatalf("serve wrote non-JSON to stdout: %q: %v\nstderr: %s",
			line, err, s.stderr.String())
	}
	return response
}

func TestServe_CompletesHandshake(t *testing.T) {
	proc := startServe(t, t.TempDir())

	response := proc.initialize(t)

	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize returned no result: %v\nstderr: %s", response, proc.stderr.String())
	}

	info, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("initialize returned no serverInfo: %v", result)
	}
	if info["name"] != "docmcp" {
		t.Errorf("server name = %v, want docmcp", info["name"])
	}
}

func TestServe_AdvertisesExactlyTwoTools(t *testing.T) {
	proc := startServe(t, t.TempDir())
	proc.initialize(t)

	proc.send(t, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	response := proc.read(t)

	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list returned no result: %v", response)
	}

	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list returned no tools array: %v", result)
	}
	if len(tools) != 2 {
		t.Fatalf("server advertised %d tools over stdio, want exactly 2", len(tools))
	}

	names := map[string]bool{}
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("tool entry is not an object: %v", raw)
		}
		names[tool["name"].(string)] = true
	}
	for _, want := range []string{"resolve-library-id", "query-docs"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestServe_LogsGoToStderrOnly(t *testing.T) {
	// The critical stdio property: one stray log line on stdout corrupts the
	// JSON-RPC framing and the client drops the connection.
	proc := startServe(t, t.TempDir())
	proc.initialize(t)

	// Ask for a library that does not exist, which is the path most likely to log.
	proc.call(t, 2, "query-docs", map[string]any{
		"libraryId": "/local/missing/1",
		"query":     "anything",
	})

	// Every line the server wrote to stdout so far parsed as JSON; a log line
	// would have failed there. Verify explicitly that nothing non-JSON leaked.
	line, err := proc.stdout.PeekNonBlocking()
	if err == nil && strings.TrimSpace(line) != "" {
		t.Errorf("non-JSON on stdout: %q", line)
	}
}

func TestServe_ReportsErrorsAsToolContent(t *testing.T) {
	proc := startServe(t, t.TempDir())
	proc.initialize(t)

	response := proc.call(t, 2, "query-docs", map[string]any{
		"libraryId": "/local/missing/1",
		"query":     "anything",
	})

	// The model has to be able to see the failure to correct itself, so it comes
	// back as result content rather than a transport error.
	if _, hasResult := response["result"]; !hasResult {
		t.Fatalf("a tool failure came back as a protocol error: %v", response)
	}

	result, _ := response["result"].(map[string]any)
	if result["isError"] != true {
		t.Errorf("isError = %v, want true for a failed tool call", result["isError"])
	}
}

func TestServe_ResolvesLibraryOverStdio(t *testing.T) {
	dataDir := t.TempDir()
	seedLibrary(t, dataDir)

	proc := startServe(t, dataDir)
	proc.initialize(t)

	response := proc.call(t, 2, "resolve-library-id", map[string]any{
		"libraryName": "fixture",
		"query":       "installation steps",
	})

	content := textOf(t, response)
	if !strings.Contains(content, "/local/fixture/1") {
		t.Errorf("resolve output missing the library ID:\n%s", content)
	}
}

func TestServe_QueriesDocsOverStdio(t *testing.T) {
	dataDir := t.TempDir()
	seedLibrary(t, dataDir)

	proc := startServe(t, dataDir)
	proc.initialize(t)

	response := proc.call(t, 2, "query-docs", map[string]any{
		"libraryId": "/local/fixture/1",
		"query":     "how do I exchange client credentials for a bearer token",
	})

	content := textOf(t, response)
	if !strings.Contains(content, "Source:") {
		t.Errorf("query output has no source URL:\n%s", content)
	}
}

func TestServe_QueriesLocalIndexWithoutNetwork(t *testing.T) {
	// The server answers from the index after the site is gone: MCP reads are
	// offline. If a query needed the network, this would fail.
	dataDir := t.TempDir()
	seedLibrary(t, dataDir)

	proc := startServe(t, dataDir)
	proc.initialize(t)

	response := proc.call(t, 2, "query-docs", map[string]any{
		"libraryId": "/local/fixture/1",
		"query":     "bearer token exchange",
	})

	if result, _ := response["result"].(map[string]any); result["isError"] == true {
		t.Fatalf("query failed against a local index: %s", textOf(t, response))
	}
}

func textOf(t *testing.T, response map[string]any) string {
	t.Helper()

	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("response has no result: %v", response)
	}

	content, ok := result["content"].([]any)
	if !ok {
		t.Fatalf("result has no content: %v", result)
	}

	var b strings.Builder
	for _, raw := range content {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := item["text"].(string); ok {
			b.WriteString(text)
		}
	}
	return b.String()
}

// buildBinary compiles the CLI so the test drives a real process.
func buildBinary(t *testing.T) string {
	t.Helper()

	binary := filepath.Join(t.TempDir(), "docmcp")

	cmd := exec.Command("go", "build", "-o", binary, "github.com/docmcp/docmcp/cmd/docmcp")
	cmd.Dir = repoRoot(t)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build docmcp: %v\n%s", err, out)
	}
	return binary
}

func repoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Dir(wd)
}
