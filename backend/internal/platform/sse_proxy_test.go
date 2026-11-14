package platform

import (
	"os"
	"strings"
	"testing"
)

func TestSSEProxyLocationCanBeSelected(t *testing.T) {
	raw, err := os.ReadFile("../../../deploy/nginx/site.conf.template")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "location ^~ /api/ {") {
		t.Fatal("^~ generic API location prevents the stream regex from being selected")
	}
	if !strings.Contains(string(raw), "include /etc/nginx/siyige/proxy-sse.conf;") {
		t.Fatal("missing SSE proxy include")
	}
}
