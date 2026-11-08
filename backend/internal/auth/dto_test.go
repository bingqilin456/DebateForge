package auth

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserDTOExposesOnlyContractFields(t *testing.T) {
	encoded, err := json.Marshal(UserDTO{ID: "42", Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"id":"42","username":"alice"}` {
		t.Fatalf("unexpected DTO encoding: %s", encoded)
	}
	for _, forbidden := range []string{"password", "hash", "session", "csrf"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("DTO leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestAuthDTOExposesUserAndCSRFOnly(t *testing.T) {
	encoded, err := json.Marshal(AuthDTO{
		User:      UserDTO{ID: "42", Username: "alice"},
		CSRFToken: "csrf-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"user":{"id":"42","username":"alice"},"csrf_token":"csrf-value"}`
	if string(encoded) != want {
		t.Fatalf("unexpected DTO encoding: %s", encoded)
	}
	if strings.Contains(string(encoded), `"token":`) {
		t.Fatal("session token must never appear in JSON")
	}
}

func TestEntityIDIsDecimalString(t *testing.T) {
	for _, tc := range []struct {
		value  uint64
		want   string
		reject bool
	}{
		{value: 1, want: "1"},
		{value: 42, want: "42"},
		{value: 18446744073709551615, want: "18446744073709551615"},
		{value: 0, reject: true},
	} {
		got, err := FormatEntityID(tc.value)
		if tc.reject {
			if err == nil {
				t.Fatalf("accepted zero id")
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("FormatEntityID(%d) = %q, %v", tc.value, got, err)
		}
	}
}
