package platform

import (
	"os"
	"testing"
)

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("HTTP_ADDR", "")
	if err := os.Unsetenv("HTTP_ADDR"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil || cfg.HTTPAddr != "127.0.0.1:8080" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestLoadConfigValidatesListener(t *testing.T) {
	for _, tc := range []struct {
		addr  string
		valid bool
	}{
		{"127.0.0.1:8081", true}, {"[::1]:8080", true},
		{"", false}, {":8080", false}, {"0.0.0.0:8080", false}, {"192.0.2.1:8080", false},
		{"127.0.0.1:0", false}, {"127.0.0.1:65536", false}, {"127.0.0.1:abc", false},
		{"127.0.0.1", false}, {" 127.0.0.1:8080", false},
	} {
		t.Run(tc.addr, func(t *testing.T) {
			t.Setenv("HTTP_ADDR", tc.addr)
			cfg, err := LoadConfig()
			if (err == nil) != tc.valid {
				t.Fatalf("cfg=%+v err=%v valid=%v", cfg, err, tc.valid)
			}
		})
	}
}
