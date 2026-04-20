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

func TestServeStaticRootIncludesFaviconLink(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	serveStatic(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `<link rel="icon" type="image/svg+xml" href="/favicon.svg?v=8">`) {
		t.Fatalf("root page missing favicon link: %s", body)
	}
}

func TestServeStaticServesFaviconSVG(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/favicon.svg", nil)
	rr := httptest.NewRecorder()

	serveStatic(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Header().Get("Content-Type"); got != "image/svg+xml" {
		t.Fatalf("content type = %q, want image/svg+xml", got)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `viewBox="0 0 64 64"`) {
		t.Fatalf("favicon missing square svg viewBox: %s", body)
	}
	if !strings.Contains(body, `fill="#f8fafc"`) {
		t.Fatalf("favicon missing light background tile: %s", body)
	}
	if !strings.Contains(body, `stroke="#6d28d9"`) {
		t.Fatalf("favicon missing center dot outline: %s", body)
	}
	if !strings.Contains(body, `d="M14 32H31"`) {
		t.Fatalf("favicon missing finalized left branch geometry: %s", body)
	}
	if !strings.Contains(body, `d="M31 32L49 14"`) {
		t.Fatalf("favicon missing finalized upper branch geometry: %s", body)
	}
	if !strings.Contains(body, `d="M31 32L49 50"`) {
		t.Fatalf("favicon missing finalized lower branch geometry: %s", body)
	}
}
