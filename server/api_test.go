package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func statusCode(s *Server, path, auth string) int {
	req := httptest.NewRequest("GET", path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	s.HTTP().ServeHTTP(w, req)
	return w.Code
}

func TestStatusToken(t *testing.T) {
	s := NewServer(10, false)
	if got := statusCode(s, "/api/stats", ""); got != http.StatusOK {
		t.Fatalf("no token configured: got %d, want 200", got)
	}
	s.httpToken = "sesame"
	for _, c := range []struct {
		path, auth string
		want       int
	}{
		{"/", "", 401},
		{"/api/stats", "", 401},
		{"/api/stats", "Bearer wrong", 401},
		{"/api/stats?token=wrong", "", 401},
		{"/api/stats", "Bearer sesame", 200},
		{"/api/stats?token=sesame", "", 200},
		{"/?token=sesame", "", 200},
	} {
		if got := statusCode(s, c.path, c.auth); got != c.want {
			t.Errorf("%s auth %q: got %d, want %d", c.path, c.auth, got, c.want)
		}
	}
}

func TestReachable(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:36101": false,
		"localhost:36101": false,
		"[::1]:36101":     false,
		":36101":          true,
		"0.0.0.0:36101":   true,
		"192.168.1.5:80":  true,
	} {
		if got := reachable(addr); got != want {
			t.Errorf("reachable(%q) = %v, want %v", addr, got, want)
		}
	}
}
