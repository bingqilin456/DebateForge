package debate

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type personalTestTransport func(*http.Request) (*http.Response, error)

func TestPersonalModelRepracticeResponse(t *testing.T) {
	const valid = `{"change":"更明确","reason":"补充了证据","new_problem":"可以说明例外","before_score":60,"after_score":72}`
	got, err := parsePersonalRepractice(valid)
	if err != nil || got.Change != "更明确" || got.BeforeScore == nil || got.AfterScore == nil || *got.BeforeScore != 60 || *got.AfterScore != 72 {
		t.Fatal("valid feedback rejected")
	}
	for _, raw := range []string{valid + "{}", `{"change":"更明确","reason":"补充了证据"}`,
		`{"change":"更明确","reason":"有证据","new_problem":"注意范围"}`,
		`{"change":"更明确","reason":"有证据","new_problem":"注意范围","before_score":60}`,
		`{"change":"更明确","reason":"有证据","new_problem":"注意范围","score":99}`} {
		if _, err := parsePersonalRepractice(raw); err == nil {
			t.Fatal("invalid feedback accepted")
		}
	}
}

func (f personalTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestPersonalModelSyntheticConnection(t *testing.T) {
	model := PersonalModel{client: &http.Client{Transport: personalTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://model.example/v1/models" || req.Header.Get("Authorization") != "Bearer synthetic-test-token" {
			t.Fatal("unexpected connection request")
		}
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("supplier detail"))}, nil
	})}}
	if err := model.testProvider(context.Background(), "https://model.example/v1", "synthetic-test-token"); err == nil || strings.Contains(err.Error(), "supplier detail") {
		t.Fatal("provider failure must be controlled")
	}
}

func TestPersonalModelOwnerBinding(t *testing.T) {
	vault, err := newPersonalModelCipher(bytes.Repeat([]byte{42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	const value = "synthetic-test-token"
	ciphertext := sealPersonalKey(vault, 10, value)
	if bytes.Contains(ciphertext, []byte(value)) {
		t.Fatal("plaintext in encrypted record")
	}
	if got, err := openPersonalKey(vault, 10, ciphertext); err != nil || got != value {
		t.Fatal("owner could not open record")
	}
	if _, err := openPersonalKey(vault, 11, ciphertext); err == nil {
		t.Fatal("non-owner opened record")
	}
}

func TestPersonalModelEndpoints(t *testing.T) {
	t.Setenv("BYOK_CUSTOM_HOSTS", "model.example")
	in := personalModelInput{APIKey: "synthetic-test-token", Alias: "测试辩手", Model: "test-model"}
	for _, provider := range []string{"deepseek", "openai", "doubao"} {
		in.Provider, in.BaseURL = provider, "https://other.example"
		got, err := normalizePersonalModel(in)
		if err != nil || got.BaseURL == in.BaseURL {
			t.Fatalf("provider %s did not use fixed endpoint", provider)
		}
	}
	in.Provider = "custom"
	for _, target := range []string{"http://model.example", "https://model.example.other.example", "https://model.example:8443", "https://model.example/v1?token=x", "https://someone@model.example"} {
		in.BaseURL = target
		if _, err := normalizePersonalModel(in); err == nil {
			t.Fatalf("accepted %q", target)
		}
	}
	in.BaseURL = "https://model.example/v1/"
	got, err := normalizePersonalModel(in)
	if err != nil || got.BaseURL != "https://model.example/v1" {
		t.Fatal("approved endpoint rejected")
	}
}
