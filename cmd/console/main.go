// Console is a standalone dashboard for CLI Proxy API.
package main

import (
	"embed"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed static/*
var staticFiles embed.FS

var (
	port              = flag.Int("port", 8318, "Console port")
	apiHost           = flag.String("api", "localhost:8317", "API host:port")
	config            = flag.String("config", "config.yaml", "Path to config.yaml")
	apiKey            = flag.String("key", "", "API key (overrides config)")
	autoKey           string
	oauthCallbackPort = flag.Int("oauth-callback-port", 18444, "Port for OAuth callback")
)

type configYAML struct {
	RemoteManagement struct {
		SecretKey string `yaml:"secret-key"`
	} `yaml:"remote-management"`
}

func main() {
	flag.Parse()

	// Read config to get API key
	loadConfig()

	http.HandleFunc("/", serveStatic)
	http.HandleFunc("/v0/", proxyToAPI)
	http.HandleFunc("/api/status", serveStatus)
	http.HandleFunc("/auth/callback", handleOAuthCallback)

	log.Printf("Console starting on :%d", *port)
	log.Printf("API: http://%s", *apiHost)
	if autoKey != "" {
		log.Printf("Using API key from config")
	}

	// Start OAuth callback server on separate port
	go startOAuthCallbackServers()

	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil); err != nil {
		log.Fatal(err)
	}
}

func loadConfig() {
	// Priority: CLI flag > ENV > config file
	if *apiKey != "" {
		autoKey = *apiKey
		log.Printf("Using key from --key flag")
		return
	}

	if key := os.Getenv("CONSOLE_API_KEY"); key != "" {
		autoKey = key
		log.Printf("Using key from CONSOLE_API_KEY env")
		return
	}

	data, err := os.ReadFile(*config)
	if err != nil {
		log.Printf("No config.yaml found, running without API key")
		return
	}
	var cfg configYAML
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return
	}
	autoKey = cfg.RemoteManagement.SecretKey
	if autoKey != "" {
		if len(autoKey) > 15 && autoKey[:3] == "$2a" {
			log.Printf("Config contains hashed key, use --key or CONSOLE_API_KEY env")
			autoKey = ""
		} else {
			log.Printf("Using plaintext key from config")
		}
	}
}

func serveStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"keyRequired":%v}`, autoKey == "")
}

func serveStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	data, err := staticFiles.ReadFile("static" + path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	contentType := "text/plain"
	switch {
	case strings.HasSuffix(path, ".html"):
		contentType = "text/html"
	case strings.HasSuffix(path, ".css"):
		contentType = "text/css"
	case strings.HasSuffix(path, ".js"):
		contentType = "application/javascript"
	case strings.HasSuffix(path, ".svg"):
		contentType = "image/svg+xml"
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}

func proxyToAPI(w http.ResponseWriter, r *http.Request) {
	url := fmt.Sprintf("http://%s%s", *apiHost, r.URL.Path)
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, url, r.Body)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	req.Header = r.Header.Clone()

	// Use auto-loaded key, or user-provided key
	if key := r.Header.Get("X-Management-Key"); key != "" {
		req.Header.Set("X-Management-Key", key)
	} else if autoKey != "" {
		req.Header.Set("X-Management-Key", autoKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "API unavailable: "+err.Error(), 503)
		return
	}
	defer resp.Body.Close()

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func apiHostFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	return parsed.Host
}

// OAuth Callback - redirect to console
func handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	// Redirect to console dashboard with success message
	query := r.URL.RawQuery
	redirectURL := fmt.Sprintf("http://localhost:%d/?oauth_callback=success&%s", *port, query)
	log.Printf("OAuth callback received, redirecting to console")
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// Forward OAuth callback to API server and redirect to console
func forwardOAuthCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.RawQuery
	log.Printf("OAuth callback received: %s?%s", r.URL.Path, query)

	// Determine API callback path based on the port the callback arrived on
	host := r.Host
	portStr := strings.TrimPrefix(host, "localhost:")
	callbackPort := 18445 // default codex callback port
	if portStr != host {
		if p, err := strconv.Atoi(portStr); err == nil {
			callbackPort = p
		}
	}

	// Map callback port to provider-specific API callback path
	apiCallbackPath := "/callback"
	switch callbackPort {
	case 54545:
		apiCallbackPath = "/anthropic/callback" // Claude uses 54545
	case 18445:
		apiCallbackPath = "/codex/callback" // Codex uses 18445
	case 18080:
		apiCallbackPath = "/google/callback" // Gemini uses 18080
	case 18081:
		apiCallbackPath = "/antigravity/callback" // Antigravity uses 18081
	}

	// Forward to API server
	apiURL := fmt.Sprintf("http://%s%s?%s", *apiHost, apiCallbackPath, query)
	log.Printf("Forwarding OAuth callback to API: %s", apiURL)

	resp, err := http.Get(apiURL)
	if err != nil {
		log.Printf("Failed to forward OAuth callback to API: %v", err)
		redirectURL := fmt.Sprintf("http://localhost:%d/?oauth_callback=error", *port)
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("OAuth callback API response: %s", string(body))

	// Redirect to main console
	redirectURL := fmt.Sprintf("http://localhost:%d/?oauth_callback=success&%s", *port, query)
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// Start OAuth callback servers on multiple ports
func startOAuthCallbackServers() {
	// Ports that different OAuth providers use
	ports := []int{*oauthCallbackPort, 18445, 54545, 18080, 18081}

	for _, callbackPort := range ports {
		port := callbackPort
		go func() {
			mux := http.NewServeMux()

			// Handle all possible callback paths
			mux.HandleFunc("/auth/callback", forwardOAuthCallback)
			mux.HandleFunc("/callback", forwardOAuthCallback)

			addr := fmt.Sprintf(":%d", port)
			log.Printf("OAuth callback server starting on %s", addr)
			if err := http.ListenAndServe(addr, mux); err != nil {
				log.Printf("OAuth callback server on %d error: %v", port, err)
			}
		}()
	}
}
