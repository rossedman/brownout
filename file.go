package brownout

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// LoadFile replaces the controller's rules with those in a JSON file of the
// form {"rules": [...]}, using the same rule fields as the admin API (except
// "ttl"; use "expires_at" for an absolute deadline).
func (c *Controller) LoadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return c.loadBytes(path, data)
}

// loadBytes parses data as a rules file and replaces the controller's rules
// with it. path is used only in error messages.
func (c *Controller) loadBytes(path string, data []byte) error {
	var doc rulesDoc
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("brownout: parse %s: %w", path, err)
	}
	if err := c.Replace(doc.Rules); err != nil {
		return fmt.Errorf("brownout: load %s: %w", path, err)
	}
	return nil
}

// WatchFile loads path and then polls it every interval, reloading the rules
// whenever its contents change. It blocks until ctx is canceled and returns
// ctx.Err(), or returns early if the initial load fails.
//
// Errors on later reloads (a missing file or invalid JSON, e.g. mid-write)
// leave the current rules in place and are passed to onError if non-nil.
// This works well with Kubernetes ConfigMap volumes, which update files
// in place without restarting the pod.
func (c *Controller) WatchFile(ctx context.Context, path string, interval time.Duration, onError func(error)) error {
	last, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := c.loadBytes(path, last); err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		data, err := os.ReadFile(path)
		if err == nil {
			if bytes.Equal(data, last) {
				continue
			}
			// Remember the contents even if they fail to load so a bad
			// file is reported once, not on every tick.
			last = data
			err = c.loadBytes(path, data)
		}
		if err != nil && onError != nil {
			onError(err)
		}
	}
}
