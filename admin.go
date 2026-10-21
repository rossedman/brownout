package brownout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxBodyBytes caps admin request bodies.
const maxBodyBytes = 1 << 20

// ruleRequest is the admin API's input form of a Rule. It accepts an optional
// "ttl" (a Go duration string such as "15m") as a convenience for ExpiresAt.
type ruleRequest struct {
	Rule
	TTL string `json:"ttl,omitempty"`
}

// toRule converts rr to a Rule, turning a TTL into an ExpiresAt relative to
// now. It rejects a request that sets both, or a TTL that isn't positive.
func (rr ruleRequest) toRule(now time.Time) (Rule, error) {
	r := rr.Rule
	if rr.TTL != "" {
		if !r.ExpiresAt.IsZero() {
			return Rule{}, errors.New("brownout: set either ttl or expires_at, not both")
		}
		d, err := time.ParseDuration(rr.TTL)
		if err != nil || d <= 0 {
			return Rule{}, fmt.Errorf("brownout: invalid ttl %q", rr.TTL)
		}
		r.ExpiresAt = now.Add(d)
	}
	return r, nil
}

type rulesDoc struct {
	Rules []Rule `json:"rules"`
}

type rulesRequest struct {
	Rules []ruleRequest `json:"rules"`
}

// AdminHandler returns an http.Handler exposing a JSON API for managing rules
// at runtime. Mount it under a prefix with http.StripPrefix:
//
//	GET    /rules        list active rules        → {"rules": [...]}
//	PUT    /rules        replace all rules        ← {"rules": [...]}
//	DELETE /rules        remove all rules
//	GET    /rules/{id}   fetch one rule
//	PUT    /rules/{id}   create or update a rule  ← {"path": "/x/*", ...}
//	DELETE /rules/{id}   remove a rule
//
// Rule bodies may include "ttl" (e.g. "10m") instead of "expires_at".
//
// The handler performs no authentication or authorization. Do not expose it
// publicly: serve it on an internal listener or wrap it with your own auth.
func (c *Controller) AdminHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /rules", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, rulesDoc{Rules: c.Rules()})
	})

	mux.HandleFunc("PUT /rules", func(w http.ResponseWriter, req *http.Request) {
		var body rulesRequest
		if !readJSON(w, req, &body) {
			return
		}
		now := c.now()
		rules := make([]Rule, 0, len(body.Rules))
		for _, rr := range body.Rules {
			r, err := rr.toRule(now)
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			rules = append(rules, r)
		}
		if err := c.Replace(rules); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, rulesDoc{Rules: c.Rules()})
	})

	mux.HandleFunc("DELETE /rules", func(w http.ResponseWriter, _ *http.Request) {
		c.Clear()
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /rules/{id}", func(w http.ResponseWriter, req *http.Request) {
		r, ok := c.Rule(req.PathValue("id"))
		if !ok {
			writeError(w, http.StatusNotFound, errors.New("brownout: rule not found"))
			return
		}
		writeJSON(w, http.StatusOK, r)
	})

	mux.HandleFunc("PUT /rules/{id}", func(w http.ResponseWriter, req *http.Request) {
		var body ruleRequest
		if !readJSON(w, req, &body) {
			return
		}
		id := req.PathValue("id")
		if body.ID != "" && body.ID != id {
			writeError(w, http.StatusBadRequest, fmt.Errorf("brownout: body id %q does not match path id %q", body.ID, id))
			return
		}
		body.ID = id
		r, err := body.toRule(c.now())
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := c.Set(r); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if stored, ok := c.Rule(id); ok {
			r = stored
		}
		writeJSON(w, http.StatusOK, r)
	})

	mux.HandleFunc("DELETE /rules/{id}", func(w http.ResponseWriter, req *http.Request) {
		if !c.Remove(req.PathValue("id")) {
			writeError(w, http.StatusNotFound, errors.New("brownout: rule not found"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	return mux
}

// readJSON decodes the request body into v, rejecting unknown fields and
// bodies over maxBodyBytes. On failure it writes a 400 and returns false.
func readJSON(w http.ResponseWriter, req *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(req.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("brownout: invalid JSON: %w", err))
		return false
	}
	return true
}

// writeJSON writes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes err as a JSON {"error": ...} response with the given
// status.
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
