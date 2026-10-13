// Package brownout provides HTTP middleware for shedding traffic to selected
// endpoints at runtime ("browning out") without restarting the server.
//
// A Controller holds a set of Rules. Each Rule matches requests by method and
// path and decides what fraction of matching traffic is allowed through; the
// rest is rejected with a configurable status code (503 by default). Rules can
// be changed at any time — programmatically, through the JSON admin API
// returned by AdminHandler, or by watching a file with WatchFile — and the
// change takes effect on the very next request.
//
//	c := brownout.New()
//	mux := http.NewServeMux()
//	mux.Handle("/admin/brownout/", http.StripPrefix("/admin/brownout", c.AdminHandler()))
//	http.ListenAndServe(":8080", c.Middleware(appHandler))
//
//	// Later, from anywhere:
//	c.Set(brownout.Rule{ID: "reports", Path: "/reports/*"})
package brownout

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Rule describes an endpoint (or group of endpoints) to brown out.
type Rule struct {
	// ID uniquely identifies the rule. Setting a rule with an existing ID
	// replaces it in place.
	ID string `json:"id"`

	// Path is matched against the request's URL path. A trailing "*" makes
	// it a prefix match ("/api/reports/*" matches "/api/reports/123"); a
	// lone "*" matches every path. Otherwise the match is exact.
	Path string `json:"path"`

	// Methods restricts the rule to the given HTTP methods. Empty matches
	// any method.
	Methods []string `json:"methods,omitempty"`

	// AllowPercent is the percentage (0–100) of matching requests that are
	// still let through. The zero value sheds all matching traffic; 25 lets
	// roughly one in four requests through.
	AllowPercent float64 `json:"allow_percent,omitempty"`

	// StatusCode is the response status for shed requests. Defaults to the
	// Controller's default (503 Service Unavailable unless overridden).
	StatusCode int `json:"status_code,omitempty"`

	// RetryAfterSeconds, when positive, is sent as the Retry-After header.
	RetryAfterSeconds int `json:"retry_after_seconds,omitempty"`

	// Message is written as the response body for shed requests. Defaults
	// to the status text of StatusCode.
	Message string `json:"message,omitempty"`

	// ExpiresAt, when non-zero, automatically disables the rule after the
	// given time.
	ExpiresAt time.Time `json:"expires_at,omitzero"`
}

// Validate reports whether the rule is well formed.
func (r Rule) Validate() error {
	switch {
	case r.ID == "":
		return errors.New("brownout: rule id is required")
	case r.Path == "":
		return errors.New("brownout: rule path is required")
	case r.Path != "*" && !strings.HasPrefix(r.Path, "/"):
		return fmt.Errorf("brownout: rule path %q must start with \"/\" or be \"*\"", r.Path)
	case strings.Contains(strings.TrimSuffix(r.Path, "*"), "*"):
		return fmt.Errorf("brownout: rule path %q may only contain \"*\" at the end", r.Path)
	case r.AllowPercent < 0 || r.AllowPercent > 100:
		return fmt.Errorf("brownout: allow_percent %v must be between 0 and 100", r.AllowPercent)
	case r.StatusCode != 0 && (r.StatusCode < 100 || r.StatusCode > 599):
		return fmt.Errorf("brownout: status_code %d is not a valid HTTP status", r.StatusCode)
	case r.RetryAfterSeconds < 0:
		return errors.New("brownout: retry_after_seconds must not be negative")
	}
	return nil
}

// Matches reports whether the rule applies to the request at time now,
// ignoring AllowPercent.
func (r Rule) Matches(req *http.Request, now time.Time) bool {
	if r.expired(now) {
		return false
	}
	if len(r.Methods) > 0 && !slices.Contains(r.Methods, req.Method) {
		return false
	}
	return matchPath(r.Path, req.URL.Path)
}

func (r Rule) expired(now time.Time) bool {
	return !r.ExpiresAt.IsZero() && !now.Before(r.ExpiresAt)
}

func matchPath(pattern, path string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(path, prefix)
	}
	return pattern == path
}

// normalize returns a copy of r that is safe to store: method names are
// upper-cased and slices are not shared with the caller.
func (r Rule) normalize() Rule {
	if len(r.Methods) > 0 {
		methods := make([]string, len(r.Methods))
		for i, m := range r.Methods {
			methods[i] = strings.ToUpper(m)
		}
		r.Methods = methods
	}
	return r
}

func (r Rule) clone() Rule {
	r.Methods = slices.Clone(r.Methods)
	return r
}

// Responder writes the response for a shed request.
type Responder func(w http.ResponseWriter, req *http.Request, rule Rule)

// Option configures a Controller.
type Option func(*Controller)

// WithDefaultStatus sets the status code used for rules that don't specify
// one. The default is 503 Service Unavailable.
func WithDefaultStatus(code int) Option {
	return func(c *Controller) { c.defaultStatus = code }
}

// WithResponder replaces the default response writer for shed requests. The
// Rule passed to the responder has StatusCode already defaulted.
func WithResponder(fn Responder) Option {
	return func(c *Controller) { c.respond = fn }
}

// WithOnShed registers a callback invoked for every shed request, e.g. to
// record metrics or logs. It runs synchronously before the response is
// written, so it should be fast.
func WithOnShed(fn func(req *http.Request, rule Rule)) Option {
	return func(c *Controller) { c.onShed = fn }
}

// WithClock overrides the time source used to evaluate rule expiry. Intended
// for tests.
func WithClock(now func() time.Time) Option {
	return func(c *Controller) { c.now = now }
}

// Controller holds the active brownout rules and produces middleware that
// enforces them. It is safe for concurrent use; the request path is lock-free.
type Controller struct {
	mu    sync.Mutex // serializes writers
	rules atomic.Pointer[[]Rule]

	defaultStatus int
	respond       Responder
	onShed        func(*http.Request, Rule)
	now           func() time.Time
	random        func() float64 // returns [0, 100)
}

// New returns a Controller with no rules.
func New(opts ...Option) *Controller {
	c := &Controller{
		defaultStatus: http.StatusServiceUnavailable,
		respond:       defaultResponder,
		now:           time.Now,
		random:        func() float64 { return rand.Float64() * 100 },
	}
	for _, opt := range opts {
		opt(c)
	}
	c.rules.Store(&[]Rule{})
	return c
}

// Middleware wraps next so that requests matching an active rule are shed.
func (c *Controller) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if rule, shed := c.Check(req); shed {
			if c.onShed != nil {
				c.onShed(req, rule)
			}
			c.respond(w, req, rule)
			return
		}
		next.ServeHTTP(w, req)
	})
}

// Check reports whether req should be shed and, if so, the rule responsible.
// Rules are evaluated in order and the first match decides: if it lets the
// request through (per AllowPercent), later rules are not consulted.
func (c *Controller) Check(req *http.Request) (Rule, bool) {
	rules := *c.rules.Load()
	if len(rules) == 0 {
		return Rule{}, false
	}
	now := c.now()
	for _, r := range rules {
		if !r.Matches(req, now) {
			continue
		}
		if r.AllowPercent > 0 && c.random() < r.AllowPercent {
			return Rule{}, false
		}
		if r.StatusCode == 0 {
			r.StatusCode = c.defaultStatus
		}
		return r.clone(), true
	}
	return Rule{}, false
}

// Rules returns a copy of the current rules, in evaluation order, excluding
// any that have expired.
func (c *Controller) Rules() []Rule {
	now := c.now()
	rules := *c.rules.Load()
	out := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if !r.expired(now) {
			out = append(out, r.clone())
		}
	}
	return out
}

// Rule returns the active rule with the given ID.
func (c *Controller) Rule(id string) (Rule, bool) {
	now := c.now()
	for _, r := range *c.rules.Load() {
		if r.ID == id && !r.expired(now) {
			return r.clone(), true
		}
	}
	return Rule{}, false
}

// Set adds a rule, or replaces the existing rule with the same ID while
// keeping its position in the evaluation order.
func (c *Controller) Set(rule Rule) error {
	if err := rule.Validate(); err != nil {
		return err
	}
	rule = rule.normalize()

	c.mu.Lock()
	defer c.mu.Unlock()
	rules := c.liveRules()
	if i := slices.IndexFunc(rules, func(r Rule) bool { return r.ID == rule.ID }); i >= 0 {
		rules[i] = rule
	} else {
		rules = append(rules, rule)
	}
	c.rules.Store(&rules)
	return nil
}

// Remove deletes the rule with the given ID, reporting whether it existed.
func (c *Controller) Remove(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	rules := c.liveRules()
	i := slices.IndexFunc(rules, func(r Rule) bool { return r.ID == id })
	if i < 0 {
		return false
	}
	rules = slices.Delete(rules, i, i+1)
	c.rules.Store(&rules)
	return true
}

// Replace atomically swaps the entire rule set. Either all rules are valid
// and installed, or an error is returned and nothing changes.
func (c *Controller) Replace(rules []Rule) error {
	next := make([]Rule, 0, len(rules))
	seen := make(map[string]bool, len(rules))
	for _, r := range rules {
		if err := r.Validate(); err != nil {
			return err
		}
		if seen[r.ID] {
			return fmt.Errorf("brownout: duplicate rule id %q", r.ID)
		}
		seen[r.ID] = true
		next = append(next, r.normalize())
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.rules.Store(&next)
	return nil
}

// Clear removes all rules.
func (c *Controller) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rules.Store(&[]Rule{})
}

// liveRules returns a fresh copy of the stored rules with expired ones pruned.
// Callers must hold c.mu.
func (c *Controller) liveRules() []Rule {
	now := c.now()
	current := *c.rules.Load()
	out := make([]Rule, 0, len(current)+1)
	for _, r := range current {
		if !r.expired(now) {
			out = append(out, r)
		}
	}
	return out
}

func defaultResponder(w http.ResponseWriter, _ *http.Request, rule Rule) {
	if rule.RetryAfterSeconds > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(rule.RetryAfterSeconds))
	}
	msg := rule.Message
	if msg == "" {
		msg = http.StatusText(rule.StatusCode)
	}
	http.Error(w, msg, rule.StatusCode)
}
