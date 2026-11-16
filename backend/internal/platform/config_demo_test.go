package platform

import "testing"

func TestDemoConfigIsolatedAndLoopbackOrigin(t *testing.T) {
	setDatabaseEnv(t)
	t.Setenv("APP_ENV", "demo")
	t.Setenv("HTTP_ADDR", "0.0.0.0:8080")
	t.Setenv("AUTH_ORIGIN", "http://127.0.0.1:18081")
	t.Setenv("DB_ADDR", "mysql:3306")
	t.Setenv("DB_NAME", "siyige_demo")
	t.Setenv("DB_USER", "siyige_demo_app")
	t.Setenv("AI_MODE", "fake")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.ValidateTestTarget() == nil {
		t.Fatal("demo accepted as resettable integration target")
	}
	for _, tc := range []struct{ key, value string }{{"DB_NAME", "siyige_test"}, {"DB_USER", "siyige_deploy_app"}, {"DB_ADDR", "127.0.0.1:13307"}, {"AUTH_ORIGIN", "https://demo.example.invalid"}, {"HTTP_ADDR", "0.0.0.0:9000"}, {"AI_MODE", "deepseek"}, {"AI_MODE", "local-demo"}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := LoadConfig(); err == nil {
				t.Fatal("unsafe demo config accepted")
			}
		})
	}
}
