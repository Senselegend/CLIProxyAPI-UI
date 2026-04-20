package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyToAPIForwardsBodyAndHeaders(t *testing.T) {
	var gotMethod string
	var gotBody string
	var gotContentType string
	var gotKey string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		gotMethod = r.Method
		gotBody = string(body)
		gotContentType = r.Header.Get("Content-Type")
		gotKey = r.Header.Get("X-Management-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	originalHost := *apiHost
	originalKey := autoKey
	*apiHost = apiHostFromURL(upstream.URL)
	autoKey = "console-secret"
	defer func() {
		*apiHost = originalHost
		autoKey = originalKey
	}()

	req := httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files/status", strings.NewReader(`{"name":"auth.json","disabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	proxyToAPI(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if gotMethod != http.MethodPatch {
		t.Fatalf("upstream method = %q, want %q", gotMethod, http.MethodPatch)
	}
	if gotBody != `{"name":"auth.json","disabled":true}` {
		t.Fatalf("upstream body = %q", gotBody)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content type = %q, want application/json", gotContentType)
	}
	if gotKey != "console-secret" {
		t.Fatalf("management key = %q, want console-secret", gotKey)
	}
}
