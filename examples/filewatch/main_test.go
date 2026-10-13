package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeRules replaces path atomically, the way a ConfigMap update does, so
// the watcher never sees a half-written file.
func writeRules(t *testing.T, path, data string) {
	t.Helper()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func init() {
	// Keep shutdown fast in tests; the forced-close path still runs.
	shutdownGrace = 100 * time.Millisecond
}

func waitFor(t *testing.T, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", desc)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func status(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestReloadsRulesFromFile(t *testing.T) {
	rules := filepath.Join(t.TempDir(), "rules.json")
	writeRules(t, rules, `{"rules":[{"id":"reports","path":"/reports/*"}]}`)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, ln, rules, 5*time.Millisecond) }()

	if got := status(t, base+"/reports/q3"); got != http.StatusServiceUnavailable {
		t.Fatalf("initial rules: %d, want 503", got)
	}
	if got := status(t, base+"/other"); got != http.StatusOK {
		t.Fatalf("unmatched path: %d, want 200", got)
	}

	// An invalid file is ignored and the previous rules stay in force.
	writeRules(t, rules, `{"rules":[`)
	time.Sleep(50 * time.Millisecond)
	if got := status(t, base+"/reports/q3"); got != http.StatusServiceUnavailable {
		t.Fatalf("after bad write: %d, want 503 (previous rules kept)", got)
	}

	writeRules(t, rules, `{"rules":[]}`)
	waitFor(t, "rules to reload", func() bool { return status(t, base+"/reports/q3") == http.StatusOK })

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v, want nil after shutdown", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after cancel")
	}
}

func TestInvalidRulesFailStartup(t *testing.T) {
	rules := filepath.Join(t.TempDir(), "rules.json")
	writeRules(t, rules, `{"rules":[{"id":"x","path":"no-slash"}]}`)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := run(context.Background(), ln, rules, time.Second); err == nil {
		t.Fatal("run accepted an invalid rules file")
	}
}

func TestServeErrorIsReturned(t *testing.T) {
	rules := filepath.Join(t.TempDir(), "rules.json")
	writeRules(t, rules, `{"rules":[]}`)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close() // Serve fails immediately on a closed listener.
	if err := run(context.Background(), ln, rules, time.Second); err == nil {
		t.Fatal("run returned nil for a closed listener")
	}
}

func TestShippedRulesFileIsValid(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, ln, "rules.json", time.Second) }()

	if got := status(t, "http://"+ln.Addr().String()+"/reports/q3"); got != http.StatusServiceUnavailable {
		t.Errorf("rules.json /reports/q3: %d, want 503", got)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCLI(t *testing.T) {
	rules := filepath.Join(t.TempDir(), "rules.json")
	writeRules(t, rules, `{"rules":[]}`)

	if err := cli(context.Background(), []string{"-no-such-flag"}); err == nil {
		t.Error("cli accepted an unknown flag")
	}
	if err := cli(context.Background(), []string{"-addr", "not an address", "-rules", rules}); err == nil {
		t.Error("cli accepted an invalid listen address")
	}

	// With an already-canceled context, cli starts up fully and then shuts
	// straight down, so this exercises the whole path without a fixed port.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cli(ctx, []string{"-addr", "127.0.0.1:0", "-rules", rules, "-interval", "1s"}); err != nil {
		t.Errorf("cli returned %v, want nil", err)
	}
}

// A client can open a connection and never send a request on it. Go's HTTP
// client does this when a spare dial loses the race to a pooled connection.
// The server counts such a connection as active for up to 5s, which used to
// make graceful shutdown time out and run return an error.
func TestShutdownWithUnusedConnection(t *testing.T) {
	rules := filepath.Join(t.TempDir(), "rules.json")
	writeRules(t, rules, `{"rules":[]}`)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, ln, rules, time.Second) }()

	// Make sure the server is serving, then open a connection and leave it
	// unused.
	if got := status(t, "http://"+ln.Addr().String()+"/"); got != http.StatusOK {
		t.Fatalf("status = %d", got)
	}
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v, want nil", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("run did not return after cancel")
	}
}
