// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Package aos is a minimal client for the ArubaOS-Switch (AOS-S) REST API
// (v8, HTTPS, cookie-based session auth) served by ProVision-era switches
// such as the 2530 / 2920 / 2930F running WB/YA/YC firmware (16.x).
//
// AOS-S is NOT ArubaOS-CX: the official aruba/terraform-provider-aoscx targets
// CX only and does not manage these switches. AOS-S has no Terraform provider
// upstream, hence this one. The API surface is documented in HPE's "REST API
// for AOS-S" guides; this client is generic over it (any /rest/v8 path).
package aos

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client is a session-authenticated AOS-S REST client. It logs in lazily on
// the first call and reuses the session cookie; callers may share one Client
// across resources (the provider does). Safe for concurrent use.
type Client struct {
	base     string // e.g. https://192.168.2.210/rest/v8
	user     string
	password string
	http     *http.Client

	mu     sync.Mutex
	cookie string // "sessionId=..." once logged in
}

// Config configures a Client.
type Config struct {
	// Host is the switch address (host or host:port), no scheme.
	Host string
	// Username / Password are the AOS-S operator/manager credentials.
	Username string
	Password string
	// Insecure skips TLS verification (AOS-S ships a self-signed cert; true is
	// the norm on a lab/OOB management network).
	Insecure bool
	// Timeout per request (default 30s).
	Timeout time.Duration
}

// NewClient builds a Client. It does not contact the switch until the first
// API call.
func NewClient(c Config) *Client {
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: c.Insecure}, //nolint:gosec // self-signed mgmt cert
		// AOS-S serves one session at a time; keep connections lean.
		MaxIdleConns:    2,
		IdleConnTimeout: 30 * time.Second,
	}
	host := strings.TrimSuffix(strings.TrimPrefix(c.Host, "https://"), "/")
	host = strings.TrimPrefix(host, "http://")
	return &Client{
		base:     fmt.Sprintf("https://%s/rest/v8", host),
		user:     c.Username,
		password: c.Password,
		http:     &http.Client{Timeout: c.Timeout, Transport: tr},
	}
}

// APIError is returned when the switch responds with a non-2xx status.
type APIError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("aos %s %s: HTTP %d: %s", e.Method, e.Path, e.Status, e.Body)
}

// NotFound reports whether err is an APIError with a 404 status.
func NotFound(err error) bool {
	var ae *APIError
	if e, ok := err.(*APIError); ok {
		ae = e
	}
	return ae != nil && ae.Status == http.StatusNotFound
}

// login establishes a session cookie. Caller must hold c.mu.
func (c *Client) login() error {
	body, _ := json.Marshal(map[string]string{"userName": c.user, "password": c.password})
	req, err := http.NewRequest(http.MethodPost, c.base+"/login-sessions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("aos login: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return &APIError{Method: "POST", Path: "/login-sessions", Status: resp.StatusCode, Body: string(raw)}
	}
	var out struct {
		Cookie string `json:"cookie"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Cookie == "" {
		return fmt.Errorf("aos login: no cookie in response: %s", string(raw))
	}
	c.cookie = out.Cookie
	return nil
}

// loginRetry logs in, retrying with backoff on HTTP 503 "no free REST sessions"
// — AOS-S caps concurrent REST sessions very low (≈5), so a slot may need a
// moment to free (idle sessions time out). Caller must hold c.mu.
func (c *Client) loginRetry() error {
	delays := []time.Duration{0, 3 * time.Second, 6 * time.Second, 12 * time.Second, 20 * time.Second, 30 * time.Second}
	var last error
	for _, d := range delays {
		if d > 0 {
			time.Sleep(d)
		}
		err := c.login()
		if err == nil {
			return nil
		}
		var ae *APIError
		if e, ok := err.(*APIError); ok {
			ae = e
		}
		if ae == nil || ae.Status != http.StatusServiceUnavailable {
			return err // not a session-pressure error — fail fast
		}
		last = err
	}
	return fmt.Errorf("aos login: exhausted retries waiting for a free REST session: %w", last)
}

// logoutLocked tears down the current session. Caller must hold c.mu.
func (c *Client) logoutLocked() {
	if c.cookie == "" {
		return
	}
	req, err := http.NewRequest(http.MethodDelete, c.base+"/login-sessions", nil)
	if err == nil {
		req.Header.Set("Cookie", c.cookie)
		if resp, derr := c.http.Do(req); derr == nil {
			resp.Body.Close()
		}
	}
	c.cookie = ""
}

// Logout tears down the session. Best-effort; errors are ignored.
func (c *Client) Logout() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logoutLocked()
}

// do performs one authenticated request and ALWAYS releases the session
// afterwards (login → request → logout, under the mutex). AOS-S allows only a
// handful of concurrent REST sessions, so holding one across a long Terraform
// run (or leaking one per run) exhausts the cap; acquiring and releasing per
// operation keeps at most one session live and never leaks. path is relative to
// /rest/v8 and must start with "/". body may be nil.
func (c *Client) do(method, path string, body []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loginRetry(); err != nil {
		return nil, err
	}
	defer c.logoutLocked()
	raw, status, err := c.attempt(method, path, body)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		// Session rejected — re-login once and retry.
		c.cookie = ""
		if err := c.loginRetry(); err != nil {
			return nil, err
		}
		raw, status, err = c.attempt(method, path, body)
		if err != nil {
			return nil, err
		}
	}
	if status/100 != 2 {
		return nil, &APIError{Method: method, Path: path, Status: status, Body: string(raw)}
	}
	return raw, nil
}

func (c *Client) attempt(method, path string, body []byte) ([]byte, int, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Cookie", c.cookie)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("aos %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return raw, resp.StatusCode, nil
}

// Get fetches a resource. path is relative to /rest/v8 (must start with "/").
func (c *Client) Get(path string) ([]byte, error) { return c.do(http.MethodGet, path, nil) }

// Put upserts a resource with the given JSON body.
func (c *Client) Put(path string, body []byte) ([]byte, error) {
	return c.do(http.MethodPut, path, body)
}

// Post creates a resource in a collection with the given JSON body.
func (c *Client) Post(path string, body []byte) ([]byte, error) {
	return c.do(http.MethodPost, path, body)
}

// Delete removes a resource.
func (c *Client) Delete(path string) ([]byte, error) { return c.do(http.MethodDelete, path, nil) }
