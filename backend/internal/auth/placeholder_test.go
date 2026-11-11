package auth

import "testing"

func TestPlaceholderHashMatchesFrozenParameters(t *testing.T) {
	// 未知用户名必须用“同参数”的占位哈希做一次真实验证，否则响应时间可被用于枚举账号。
	params, salt, key, err := decodeArgon2idHash(placeholderPasswordHash)
	if err != nil {
		t.Fatalf("placeholder hash is malformed: %v", err)
	}
	if params != defaultArgon2Params {
		t.Fatalf("placeholder parameters differ from hashing parameters: %+v", params)
	}
	if len(salt) != argon2SaltLength || len(key) != argon2KeyLength {
		t.Fatalf("unexpected placeholder salt/key length: %d/%d", len(salt), len(key))
	}
	// 占位哈希不得匹配任何真实口令。
	matched, err := VerifyPassword(placeholderPasswordHash, "correct horse battery")
	if err != nil {
		t.Fatalf("placeholder must be structurally valid: %v", err)
	}
	if matched {
		t.Fatal("placeholder hash matched a real password")
	}
}
