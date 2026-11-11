package auth

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"
)

const testPassword = "correct horse battery"

func mustEncode(t *testing.T, algorithm, version string, memory, iterations, parallelism uint32, salt, key []byte) string {
	t.Helper()
	return fmt.Sprintf("$%s$v=%s$m=%d,t=%d,p=%d$%s$%s", algorithm, version, memory, iterations, parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

func TestHashPasswordEncodesFrozenParameters(t *testing.T) {
	encoded, err := HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	params, salt, key, err := decodeArgon2idHash(encoded)
	if err != nil {
		t.Fatalf("cannot decode fresh hash: %v", err)
	}
	if params.memory != 19*1024 || params.iterations != 2 || params.parallelism != 1 {
		t.Fatalf("unexpected parameters: %+v", params)
	}
	if len(salt) != 16 || len(key) != 32 {
		t.Fatalf("unexpected salt/key length: %d/%d", len(salt), len(key))
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("missing parameter encoding: %q", encoded)
	}
	if strings.Contains(encoded, testPassword) {
		t.Fatal("encoded hash must not contain the plaintext password")
	}
}

func TestHashPasswordUsesRandomSalt(t *testing.T) {
	first, err := HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("identical passwords must not produce identical encodings")
	}
}

func TestHashPasswordRejectsInvalidInput(t *testing.T) {
	for name, raw := range map[string]string{
		"too short": strings.Repeat("a", 11),
		"too long":  strings.Repeat("a", 129),
		"invalid":   string([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := HashPassword(raw); err == nil {
				t.Fatal("hashed an invalid password")
			}
		})
	}
}

func TestVerifyPasswordAcceptsOnlyTheOriginalPassword(t *testing.T) {
	encoded, err := HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword(encoded, testPassword)
	if err != nil || !ok {
		t.Fatalf("correct password rejected: ok=%v err=%v", ok, err)
	}
	// 原样比较：大小写、尾随空格和截断都不能通过。
	for _, wrong := range []string{
		"Correct horse battery",
		testPassword + " ",
		testPassword[:len(testPassword)-1],
		strings.Repeat("a", 12),
	} {
		ok, err := VerifyPassword(encoded, wrong)
		if err != nil {
			t.Fatalf("wrong password must not surface an error: %v", err)
		}
		if ok {
			t.Fatalf("accepted wrong password %q", wrong)
		}
	}
}

func TestVerifyPasswordIsDeterministicAcrossEncodings(t *testing.T) {
	salt := []byte("0123456789abcdef")
	derived, err := deriveKey(testPassword, salt, defaultArgon2Params)
	if err != nil {
		t.Fatal(err)
	}
	again, err := deriveKey(testPassword, salt, defaultArgon2Params)
	if err != nil {
		t.Fatal(err)
	}
	if string(derived) != string(again) {
		t.Fatal("same salt and parameters must derive the same key")
	}
	other, err := deriveKey(testPassword, []byte("fedcba9876543210"), defaultArgon2Params)
	if err != nil {
		t.Fatal(err)
	}
	if string(derived) == string(other) {
		t.Fatal("different salts must derive different keys")
	}
	if len(derived) != 32 {
		t.Fatalf("unexpected key length %d", len(derived))
	}
}

func TestVerifyPasswordRejectsMalformedEncodings(t *testing.T) {
	salt := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef"))
	key := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	cases := map[string]string{
		"empty":              "",
		"plain text":         testPassword,
		"missing key":        "$argon2id$v=19$m=19456,t=2,p=1$" + salt,
		"missing salt":       "$argon2id$v=19$m=19456,t=2,p=1$$" + key,
		"wrong algorithm":    mustEncode(t, "argon2i", "19", 19456, 2, 1, []byte("0123456789abcdef"), make([]byte, 32)),
		"bcrypt":             "$2a$10$abcdefghijklmnopqrstuv",
		"old version":        mustEncode(t, "argon2id", "16", 19456, 2, 1, []byte("0123456789abcdef"), make([]byte, 32)),
		"missing version":    "$argon2id$m=19456,t=2,p=1$" + salt + "$" + key,
		"salt not base64":    "$argon2id$v=19$m=19456,t=2,p=1$!!!$" + key,
		"key not base64":     "$argon2id$v=19$m=19456,t=2,p=1$" + salt + "$!!!",
		"short salt":         mustEncode(t, "argon2id", "19", 19456, 2, 1, make([]byte, 8), make([]byte, 32)),
		"short key":          mustEncode(t, "argon2id", "19", 19456, 2, 1, []byte("0123456789abcdef"), make([]byte, 16)),
		"non numeric memory": "$argon2id$v=19$m=abc,t=2,p=1$" + salt + "$" + key,
		"unknown parameter":  "$argon2id$v=19$m=19456,t=2,p=1,x=1$" + salt + "$" + key,
		"trailing segment":   "$argon2id$v=19$m=19456,t=2,p=1$" + salt + "$" + key + "$extra",
		"too long":           "$argon2id$v=19$m=19456,t=2,p=1$" + salt + "$" + strings.Repeat("A", 1024),
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			ok, err := VerifyPassword(encoded, testPassword)
			if err == nil || ok {
				t.Fatalf("accepted malformed encoding: ok=%v err=%v", ok, err)
			}
		})
	}
}

func TestVerifyPasswordRejectsUnboundedParameters(t *testing.T) {
	// 损坏或攻击者构造的编码不能迫使服务器分配超大内存或长时间计算。
	salt := []byte("0123456789abcdef")
	key := make([]byte, 32)
	for name, tc := range map[string]struct {
		memory, iterations, parallelism uint32
	}{
		"huge memory":      {1 << 30, 2, 1},
		"zero memory":      {0, 2, 1},
		"small memory":     {8, 2, 1},
		"huge iterations":  {19456, 1000, 1},
		"zero iterations":  {19456, 0, 1},
		"huge parallelism": {19456, 2, 64},
		"zero parallelism": {19456, 2, 0},
		"memory above cap": {64*1024 + 1, 2, 1},
	} {
		t.Run(name, func(t *testing.T) {
			encoded := mustEncode(t, "argon2id", "19", tc.memory, tc.iterations, tc.parallelism, salt, key)
			started := time.Now()
			ok, err := VerifyPassword(encoded, testPassword)
			if err == nil || ok {
				t.Fatalf("accepted unsupported parameters: ok=%v err=%v", ok, err)
			}
			if elapsed := time.Since(started); elapsed > 2*time.Second {
				t.Fatalf("rejected parameters still consumed time: %s", elapsed)
			}
		})
	}
}

func TestVerifyPasswordAcceptsTamperedKeyAsMismatch(t *testing.T) {
	// 结构合法但内容不同的哈希只返回“不匹配”，不泄露内部结构。
	salt := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef"))
	key := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	encoded := "$argon2id$v=19$m=19456,t=2,p=1$" + salt + "$" + key
	ok, err := VerifyPassword(encoded, testPassword)
	if err != nil {
		t.Fatalf("well-formed encoding must verify, not error: %v", err)
	}
	if ok {
		t.Fatal("all-zero key must not match")
	}
}
