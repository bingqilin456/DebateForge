package platform

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run this test in a network-disabled Linux container with nginx installed.
func TestT07NginxForwardsUpstream413(t *testing.T) {
	proxyConfig := os.Getenv("SIYIGE_TEST_NGINX_PROXY_CONF")
	if proxyConfig == "" {
		t.Skip("set SIYIGE_TEST_NGINX_PROXY_CONF to the repository proxy.conf in an isolated Linux container")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "go.log")
	goLog, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer goLog.Close()
	goServer := httptest.NewServer(NewRouter(slog.New(slog.NewJSONHandler(goLog, nil)), nil, testAuthHandler(t)))
	defer goServer.Close()

	const address = "127.0.0.1:18080"
	config := fmt.Sprintf(`
pid %s/nginx.pid;
error_log stderr crit;
events {}
http {
  access_log off;
  client_max_body_size 64k;
  client_body_temp_path %s/body;
  proxy_temp_path %s/proxy;
  fastcgi_temp_path %s/fastcgi;
  uwsgi_temp_path %s/uwsgi;
  scgi_temp_path %s/scgi;
  upstream siyige_api { server %s; }
  server {
    listen %s;
    location /api/ {
      include "%s";
      add_header X-Test-Upstream $upstream_status always;
      add_header X-Test-Framing $http_transfer_encoding always;
    }
    location @api_body_too_large { return 413 "edge-local"; }
  }
}
`, dir, dir, dir, dir, dir, dir, strings.TrimPrefix(goServer.URL, "http://"), address, proxyConfig)
	confPath := filepath.Join(dir, "nginx.conf")
	if err := os.WriteFile(confPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	nginx := exec.Command("nginx", "-p", dir, "-c", confPath, "-g", "daemon off;")
	nginx.Stderr = os.Stderr
	if err := nginx.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = nginx.Process.Kill(); _ = nginx.Wait() }()

	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	base := "http://" + address
	for deadline := time.Now().Add(5 * time.Second); ; {
		res, err := client.Get(base + "/api/health/live")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("nginx upstream did not become ready")
		}
		time.Sleep(20 * time.Millisecond)
	}

	const marker = "t07-probe-private-marker"
	payload := `{"username":"` + strings.Repeat(marker, 2048) + `"}`
	for _, framing := range []string{"fixed", "chunked"} {
		t.Run(framing, func(t *testing.T) {
			req, err := http.NewRequest("POST", base+"/api/v1/auth/login?probe="+marker, strings.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Origin", "http://127.0.0.1:5173")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Request-ID", marker)
			req.Header.Set("Authorization", "Bearer "+marker)
			req.Header.Set("Cookie", "probe="+marker)
			if framing == "chunked" {
				req.ContentLength = -1
				req.TransferEncoding = []string{"chunked"}
			}
			res, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			media, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
			if res.StatusCode != 413 || res.Header.Get("X-Test-Upstream") != "413" ||
				media != "application/json" || res.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d upstream=%q media=%q", res.StatusCode, res.Header.Get("X-Test-Upstream"), media)
			}
			gotFraming := res.Header.Get("X-Test-Framing")
			if (framing == "chunked" && gotFraming != "chunked") || (framing == "fixed" && gotFraming != "") {
				t.Fatalf("nginx saw framing %q", gotFraming)
			}
			var envelope struct {
				RequestID string `json:"request_id"`
				Error     struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&envelope); err != nil {
				t.Fatal(err)
			}
			rid := res.Header.Get("X-Request-ID")
			if rid == "" || rid == marker || envelope.RequestID != rid || envelope.Error.Code != "request_too_large" {
				t.Fatal("upstream response ID or code changed")
			}
			raw, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), marker) {
				t.Fatal("Go log exposes private request data")
			}
			for _, entry := range strings.Split(string(raw), "\n") {
				var event struct {
					RequestID string `json:"request_id"`
					Status    int    `json:"status"`
				}
				if json.Unmarshal([]byte(entry), &event) == nil && event.RequestID == rid && event.Status == 413 {
					return
				}
			}
			t.Fatal("Go log missing matching upstream 413")
		})
	}
}
