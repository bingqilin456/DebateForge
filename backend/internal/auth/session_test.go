package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"strings"
	"testing"
	"time"
)

func sequentialBytes(count int) []byte {
	buffer := make([]byte, count)
	for index := range buffer {
		buffer[index] = byte(index + 1)
	}
	return buffer
}

func randReader(t *testing.T) io.Reader {
	t.Helper()
	return rand.Reader
}

func TestNewSessionRecordDerivesFrozenShape(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	random := bytes.NewReader(sequentialBytes(SessionTokenBytes + SessionCSRFBytes))
	session, token, err := newSessionRecord(7, random, now, DefaultSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	if session.UserID != 7 || session.ID != 0 {
		t.Fatalf("unexpected identity: userID=%d id=%d", session.UserID, session.ID)
	}
	if got := len(token); got != base64.RawURLEncoding.EncodedLen(SessionTokenBytes) {
		t.Fatalf("token length = %d", got)
	}
	rawToken, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || !bytes.Equal(rawToken, sequentialBytes(SessionTokenBytes)) {
		t.Fatalf("token is not the raw random bytes: %v", err)
	}
	if len(session.TokenHash) != sha256.Size {
		t.Fatalf("token hash length = %d", len(session.TokenHash))
	}
	digest := sha256.Sum256([]byte(token))
	if !bytes.Equal(session.TokenHash, digest[:]) {
		t.Fatal("stored digest is not SHA-256 of the raw token")
	}
	if bytes.Equal(session.TokenHash, []byte(token)) || strings.Contains(string(session.TokenHash), token) {
		t.Fatal("plaintext token must never be stored")
	}
	if session.CSRFToken == token || len(session.CSRFToken) != base64.RawURLEncoding.EncodedLen(SessionCSRFBytes) {
		t.Fatal("csrf token must be independently random and distinct from the session token")
	}
	rawCSRF, err := base64.RawURLEncoding.Strict().DecodeString(session.CSRFToken)
	if err != nil || !bytes.Equal(rawCSRF, sequentialBytes(SessionTokenBytes + SessionCSRFBytes)[SessionTokenBytes:]) {
		t.Fatalf("csrf token is not the independent random bytes: %v", err)
	}
	if !session.ExpiresAt.Equal(now.Add(DefaultSessionTTL)) || !session.CreatedAt.Equal(now) {
		t.Fatalf("unexpected expiry: %s", session.ExpiresAt)
	}
	if DefaultSessionTTL != 24*time.Hour {
		t.Fatalf("session ttl must be fixed at 24h, got %s", DefaultSessionTTL)
	}
}

func TestNewSessionRecordRejectsBadInput(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		userID  uint64
		ttl     time.Duration
		entropy int
	}{
		"zero user":     {0, DefaultSessionTTL, 64},
		"zero ttl":      {1, 0, 64},
		"negative ttl":  {1, -time.Hour, 64},
		"short entropy": {1, DefaultSessionTTL, 32},
		"no entropy":    {1, DefaultSessionTTL, 0},
	} {
		t.Run(name, func(t *testing.T) {
			reader := bytes.NewReader(sequentialBytes(tc.entropy))
			if session, token, err := newSessionRecord(tc.userID, reader, now, tc.ttl); err == nil || session != nil || token != "" {
				t.Fatalf("accepted invalid session input: sessionNil=%v tokenEmpty=%v", session == nil, token == "")
			}
		})
	}
}

func TestSessionTokenDigestRejectsMalformedTokens(t *testing.T) {
	valid := base64.RawURLEncoding.EncodeToString(sequentialBytes(SessionTokenBytes))
	digest, err := sessionTokenDigest(valid)
	if err != nil || len(digest) != sha256.Size {
		t.Fatalf("rejected well-formed token: %v", err)
	}
	if want := sha256.Sum256([]byte(valid)); !bytes.Equal(digest, want[:]) {
		t.Fatal("digest does not match SHA-256 of the raw token string")
	}
	malformed := map[string]string{
		"empty":           "",
		"too short":       base64.RawURLEncoding.EncodeToString(sequentialBytes(31)),
		"too long":        base64.RawURLEncoding.EncodeToString(sequentialBytes(33)),
		"not base64":      "!!!not-base64!!!",
		"padded":          base64.URLEncoding.EncodeToString(sequentialBytes(SessionTokenBytes)),
		"hex alternative": strings.Repeat("a1", SessionTokenBytes),
		"plain":           "siyige-session-token",
	}
	for name, token := range malformed {
		t.Run(name, func(t *testing.T) {
			if digest, err := sessionTokenDigest(token); err == nil || digest != nil {
				t.Fatalf("accepted malformed token with digest length %d", len(digest))
			}
		})
	}
}

func TestEqualCSRFIsConstantTimeComparison(t *testing.T) {
	if !equalCSRF("csrf-token-value", "csrf-token-value") {
		t.Fatal("identical csrf tokens must match")
	}
	for name, provided := range map[string]string{
		"empty":     "",
		"prefix":    "csrf-token-valu",
		"suffix":    "csrf-token-valueX",
		"different": "csrf-token-valuf",
	} {
		t.Run(name, func(t *testing.T) {
			if equalCSRF("csrf-token-value", provided) {
				t.Fatal("mismatched csrf token accepted")
			}
		})
	}
}

func TestSessionValidAtUsesFixedExpiry(t *testing.T) {
	expires := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	session := Session{ExpiresAt: expires}
	if !sessionValidAt(session, expires.Add(-time.Nanosecond)) {
		t.Fatal("session must be valid before the fixed expiry")
	}
	if sessionValidAt(session, expires) {
		t.Fatal("session must expire at the exact fixed instant")
	}
	if sessionValidAt(session, expires.Add(time.Hour)) {
		t.Fatal("session must stay expired")
	}
}

func TestSessionTokensAreUniqueWithRealEntropy(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	seenTokens := map[string]bool{}
	seenDigests := map[string]bool{}
	for iteration := 0; iteration < 32; iteration++ {
		session, token, err := newSessionRecord(1, rand.Reader, now, DefaultSessionTTL)
		if err != nil {
			t.Fatal(err)
		}
		if seenTokens[token] || seenDigests[string(session.TokenHash)] {
			t.Fatal("duplicate session token generated")
		}
		seenTokens[token] = true
		seenDigests[string(session.TokenHash)] = true
	}
}
