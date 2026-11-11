package auth

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// 用户名与密码的长度边界直接冻结自 API 契约，避免 Handler、Service 各自解释。
const (
	usernameMinLength = 3
	usernameMaxLength = 32
	passwordMinRunes  = 12
	passwordMaxRunes  = 128
	passwordMaxBytes  = 512
)

var (
	// ErrInvalidUsername 与 ErrInvalidPassword 都是同一种 400 invalid_request，
	// 单独区分只为测试与内部诊断，不对外暴露具体原因。
	ErrInvalidUsername = errors.New("用户名不符合契约")
	ErrInvalidPassword = errors.New("密码不符合契约")
)

// NormalizeUsername 返回 trim 后小写的规范化用户名。
// 只接受 ASCII 字母、数字和下划线；长度限制作用于归一化后的值，
// 因此不会因首尾空白误判长度。非 ASCII 输入（含全角字符、带组合符的字母、
// 能被 Unicode 折叠成 ASCII 的字母）一律拒绝，避免同一账号出现多种拼写。
func NormalizeUsername(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", ErrInvalidUsername
	}
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) < usernameMinLength || len(trimmed) > usernameMaxLength {
		return "", ErrInvalidUsername
	}
	normalized := strings.ToLower(trimmed)
	if len(normalized) != len(trimmed) {
		// 大小写折叠改变了字节长度，说明原始输入不是纯 ASCII。
		return "", ErrInvalidUsername
	}
	for index := 0; index < len(trimmed); index++ {
		// 先用原始字节拒绝非 ASCII，任何折叠都发生在白名单检查之前。
		if trimmed[index] >= utf8.RuneSelf {
			return "", ErrInvalidUsername
		}
	}
	for index := 0; index < len(normalized); index++ {
		character := normalized[index]
		switch {
		case character >= 'a' && character <= 'z':
		case character >= '0' && character <= '9':
		case character == '_':
		default:
			return "", ErrInvalidUsername
		}
	}
	return normalized, nil
}

// ValidatePassword 按原文校验密码：不 trim、不要求字符类别。
// 码点与字节上限同时生效，避免短码点序列绕过 UTF-8 体积预算。
func ValidatePassword(raw string) error {
	if !utf8.ValidString(raw) {
		return ErrInvalidPassword
	}
	runes := utf8.RuneCountInString(raw)
	if runes < passwordMinRunes || runes > passwordMaxRunes {
		return ErrInvalidPassword
	}
	if len(raw) > passwordMaxBytes {
		return ErrInvalidPassword
	}
	return nil
}
