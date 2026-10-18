package brownout

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// writeFile replaces path atomically, so a concurrent watcher never observes
// a truncated, partially written file.
func writeFile(t *testing.T, path, data string) {
	t.Helper()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	writeFile(t, path, `{"rules":[{"id":"a","path":"/a/*"}]}`)

	c := New()
	if err := c.LoadFile(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Rule("a"); !ok {
		t.Fatal("rule a not loaded")
	}

	writeFile(t, path, `{"rules":[{"id":"a","path":"bad"}]}`)
	if err := c.LoadFile(path); err == nil {
		t.Fatal("LoadFile accepted invalid rule")
	}
	if _, ok := c.Rule("a"); !ok {
		t.Fatal("valid rules dropped after failed load")
	}
}

func TestWatchFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	writeFile(t, path, `{"rules":[{"id":"a","path":"/a"}]}`)

	c := New()
	ctx, cancel := context.WithCancel(context.Background())
	var (
		mu   sync.Mutex
		errs []error
	)
	done := make(chan error, 1)
	go func() {
		done <- c.WatchFile(ctx, path, 5*time.Millisecond, func(err error) {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		})
	}()

	waitFor := func(desc string, cond func() bool) {
		t.Helper()
		// Generous so a loaded CI runner can't fail it; it only matters
		// when the behavior is actually broken.
		deadline := time.Now().Add(10 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", desc)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	waitFor("initial load", func() bool { _, ok := c.Rule("a"); return ok })

	writeFile(t, path, `{"rules":[{"id":"b","path":"/b"}]}`)
	waitFor("reload", func() bool { _, ok := c.Rule("b"); return ok })
	if _, ok := c.Rule("a"); ok {
		t.Error("rule a still present after reload")
	}

	writeFile(t, path, `{"rules":[`)
	waitFor("error report", func() bool { mu.Lock(); defer mu.Unlock(); return len(errs) > 0 })
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	if len(errs) != 1 {
		t.Errorf("got %d errors for one bad write, want 1", len(errs))
	}
	mu.Unlock()
	if _, ok := c.Rule("b"); !ok {
		t.Error("rules dropped after bad reload")
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("WatchFile returned %v, want context.Canceled", err)
	}
}

func TestLoadFileMissing(t *testing.T) {
	if err := New().LoadFile(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestWatchFileInitialError(t *testing.T) {
	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid.json")
	writeFile(t, invalid, `{"rules":[{"id":"x","path":"no-slash"}]}`)

	for _, path := range []string{filepath.Join(dir, "missing.json"), invalid} {
		// Startup errors return immediately; the context is never consulted.
		if err := New().WatchFile(context.Background(), path, time.Second, nil); err == nil {
			t.Errorf("WatchFile(%s): expected error", filepath.Base(path))
		}
	}
}
