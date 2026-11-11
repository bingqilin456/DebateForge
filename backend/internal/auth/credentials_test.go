package auth

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeUsername(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"lowercase", "alice", "alice"},
		{"trim and lowercase", "  Alice  ", "alice"},
		{"tab newline trimmed", "\tBob_01\n", "bob_01"},
		{"boundary minimum", "a_1", "a_1"},
		{"boundary maximum", strings.Repeat("Z", 32), strings.Repeat("z", 32)},
		{"digits and underscore", "_0_9", "_0_9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeUsername(tc.raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeUsernameRejectsInvalid(t *testing.T) {
	invalid := map[string]string{
		"empty":                "",
		"spaces only":          "    ",
		"too short":            "ab",
		"too short after trim": " a ",
		"too long":             strings.Repeat("a", 33),
		"embedded space":       "ali ce",
		"punctuation":          "alice!",
		"hyphen":               "ali-ce",
		"dot":                  "ali.ce",
		"non ascii letters":    "阿里斯",
		"fullwidth letters":    "ａｌｉｃｅ",
		"nul byte":             "alice\x00",
		"invalid utf8":         "alice\xff",
		"unicode uppercase":    "ÁLICE",
		"at sign":              "alice@example",
		"newline inside":       "ali\nce",
		"normalization needed": "İstanbul",
		"combining accent":     "alice\u0301",
		"emoji":                "alice😀",
	}
	for name, raw := range invalid {
		t.Run(name, func(t *testing.T) {
			got, err := NormalizeUsername(raw)
			if err == nil {
				t.Fatalf("accepted invalid username %q normalized to %q", raw, got)
			}
			if got != "" {
				t.Fatalf("rejected username must not return a value, got %q", got)
			}
		})
	}
}

func TestNormalizeUsernameLengthIsMeasuredAfterTrim(t *testing.T) {
	// 契约的长度限制作用于归一化值，而不是原始输入。
	if _, err := NormalizeUsername("   abc   "); err != nil {
		t.Fatalf("normalized length 3 must be accepted: %v", err)
	}
	if _, err := NormalizeUsername("  " + strings.Repeat("a", 33) + "  "); err == nil {
		t.Fatal("normalized length 33 must be rejected")
	}
}

func TestValidatePasswordAcceptsContractLengths(t *testing.T) {
	for name, raw := range map[string]string{
		"12 ascii":            strings.Repeat("a", 12),
		"128 ascii":           strings.Repeat("a", 128),
		"12 chinese runes":    strings.Repeat("密", 12),
		"128 four byte runes": strings.Repeat("😀", 128),
		"mixed":               "pässwörd-1234",
		"spaces are data":     "            ",
		"trailing space":      "password1234 ",
		"boundary 512 bytes":  strings.Repeat("😀", 128),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePassword(raw); err != nil {
				t.Fatalf("rejected valid password: %v", err)
			}
		})
	}
	// 显式确认边界：128 个四字节码点恰好 512 字节，129 个越界。
	if got := utf8.RuneCountInString(strings.Repeat("😀", 128)); got != 128 {
		t.Fatalf("unexpected rune count %d", got)
	}
}

func TestValidatePasswordRejectsInvalid(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":             "",
		"11 runes":          strings.Repeat("a", 11),
		"129 runes":         strings.Repeat("a", 129),
		"129 chinese runes": strings.Repeat("密", 129),
		"invalid utf8":      string([]byte{0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8, 0xf7, 0xf6, 0xf5, 0xf4}),
		"truncated utf8":    strings.Repeat("密", 11) + string([]byte{0xe5, 0xaf}),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePassword(raw); err == nil {
				t.Fatalf("accepted invalid password of %d bytes", len(raw))
			}
		})
	}
}

func TestValidatePasswordDoesNotTrim(t *testing.T) {
	// 去掉首尾空白后会变短，但契约要求原样校验，不得 trim。
	if err := ValidatePassword("   abcdefghi   "); err != nil {
		t.Fatalf("password must not be trimmed: %v", err)
	}
	if err := ValidatePassword(" 短密码   "); err == nil {
		t.Fatal("password shorter than 12 runes must be rejected even if bytes >= 12")
	}
}
