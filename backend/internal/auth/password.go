package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id 参数冻结为架构基线 19 MiB / 2 / 1，编码进每个哈希以便未来升级。
const (
	argon2SaltLength = 16
	argon2KeyLength  = 32
	argon2Version    = argon2.Version
)

// 解码上限用于拒绝损坏或攻击者构造的哈希，避免任意内存与 CPU 分配。
const (
	argon2MaxMemory      = 64 * 1024 // 64 MiB
	argon2MinMemory      = 8 * 1024  // 8 MiB
	argon2MaxIterations  = 10
	argon2MaxParallelism = 8
	argon2MaxEncodedLen  = 512
)

var defaultArgon2Params = argon2Params{memory: 19 * 1024, iterations: 2, parallelism: 1}

// ErrInvalidHash 表示存储的密码哈希无法按冻结的 Argon2id 编码解析。
var ErrInvalidHash = errors.New("密码哈希编码无效")

// argon2Params 保留编码中的三个可调参数；字段小写以免调用方拼装任意参数。
type argon2Params struct {
	memory      uint32
	iterations  uint32
	parallelism uint32
}

// HashPassword 生成带参数编码的 Argon2id 哈希；每个密码使用独立随机盐。
func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, argon2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成密码盐失败: %w", err)
	}
	key, err := deriveKey(password, salt, defaultArgon2Params)
	if err != nil {
		return "", err
	}
	return encodeArgon2idHash(defaultArgon2Params, salt, key), nil
}

// deriveKey 是唯一执行 Argon2id 的路径，保证验证与哈希使用同一参数顺序。
func deriveKey(password string, salt []byte, params argon2Params) ([]byte, error) {
	if len(salt) != argon2SaltLength {
		return nil, ErrInvalidHash
	}
	if err := params.validate(); err != nil {
		return nil, err
	}
	// IDKey 的并行度是 uint8；上限已由 params.validate 限制在 8。
	return argon2.IDKey([]byte(password), salt, params.iterations, params.memory, uint8(params.parallelism), argon2KeyLength), nil
}

// VerifyPassword 校验密码是否匹配编码；结构无效返回 ErrInvalidHash，
// 密码不匹配返回 (false, nil)，供登录统一映射为 401 而不泄露内部原因。
func VerifyPassword(encoded, password string) (bool, error) {
	params, salt, expected, err := decodeArgon2idHash(encoded)
	if err != nil {
		return false, err
	}
	// 先用受控参数重算，再常量时间比较，避免提前返回泄露字节差异。
	actual, err := deriveKey(password, salt, params)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func encodeArgon2idHash(params argon2Params, salt, key []byte) string {
	encoding := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2Version, params.memory, params.iterations, params.parallelism,
		encoding.EncodeToString(salt), encoding.EncodeToString(key))
}

func (params argon2Params) validate() error {
	if params.memory < argon2MinMemory || params.memory > argon2MaxMemory {
		return ErrInvalidHash
	}
	if params.iterations == 0 || params.iterations > argon2MaxIterations {
		return ErrInvalidHash
	}
	if params.parallelism == 0 || params.parallelism > argon2MaxParallelism {
		return ErrInvalidHash
	}
	return nil
}

// decodeArgon2idHash 严格解析固定形状的编码，任何偏离都视为无效而不是近似接受。
func decodeArgon2idHash(encoded string) (argon2Params, []byte, []byte, error) {
	if encoded == "" || len(encoded) > argon2MaxEncodedLen {
		return argon2Params{}, nil, nil, ErrInvalidHash
	}
	// 前导空串来自开头的 '$'，因此恰好得到 6 段。
	segments := strings.Split(encoded, "$")
	if len(segments) != 6 || segments[0] != "" {
		return argon2Params{}, nil, nil, ErrInvalidHash
	}
	if segments[1] != "argon2id" {
		return argon2Params{}, nil, nil, ErrInvalidHash
	}
	version, present := strings.CutPrefix(segments[2], "v=")
	if !present || version != strconv.Itoa(argon2Version) {
		return argon2Params{}, nil, nil, ErrInvalidHash
	}
	params, err := parseArgon2Params(segments[3])
	if err != nil {
		return argon2Params{}, nil, nil, err
	}
	encoding := base64.RawStdEncoding
	salt, err := encoding.Strict().DecodeString(segments[4])
	if err != nil || len(salt) != argon2SaltLength {
		return argon2Params{}, nil, nil, ErrInvalidHash
	}
	key, err := encoding.Strict().DecodeString(segments[5])
	if err != nil || len(key) != argon2KeyLength {
		return argon2Params{}, nil, nil, ErrInvalidHash
	}
	return params, salt, key, nil
}

func parseArgon2Params(segment string) (argon2Params, error) {
	var params argon2Params
	seen := map[string]bool{}
	for _, pair := range strings.Split(segment, ",") {
		key, value, present := strings.Cut(pair, "=")
		if !present || seen[key] {
			return argon2Params{}, ErrInvalidHash
		}
		seen[key] = true
		number, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return argon2Params{}, ErrInvalidHash
		}
		switch key {
		case "m":
			params.memory = uint32(number)
		case "t":
			params.iterations = uint32(number)
		case "p":
			params.parallelism = uint32(number)
		default:
			return argon2Params{}, ErrInvalidHash
		}
	}
	if len(seen) != 3 {
		return argon2Params{}, ErrInvalidHash
	}
	if err := params.validate(); err != nil {
		return argon2Params{}, err
	}
	return params, nil
}
