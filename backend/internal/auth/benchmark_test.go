package auth

import (
	"testing"
)

// BenchmarkVerifyPassword 记录本机单次 Argon2id 验证耗时，用于说明哈希并发上限（2）
// 的容量含义。这是本机小样本观测，不是生产容量承诺；真实部署需在目标机器复测。
func BenchmarkVerifyPassword(b *testing.B) {
	encoded, err := HashPassword("benchmark-password-01")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		matched, err := VerifyPassword(encoded, "benchmark-password-01")
		if err != nil || !matched {
			b.Fatalf("benchmark verification failed: matched=%v err=%v", matched, err != nil)
		}
	}
}

// BenchmarkHashPassword 记录本机单次 Argon2id 哈希（含随机盐生成）耗时。
func BenchmarkHashPassword(b *testing.B) {
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := HashPassword("benchmark-password-01"); err != nil {
			b.Fatal(err)
		}
	}
}
