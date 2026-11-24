package debate

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var rolePrefix = regexp.MustCompile(`^(?:[[:space:]　·：:，,|/_-]*(?:第[[:space:]　]*[0-9一二三四五六七八九十]+[[:space:]　]*(?:轮|回合)|正方|反方|我方|对方|辩手|[一二三四]辩|立论|攻辩|交锋|自由辩|结辩|总结|发言|回复|反驳|(?i:assistant|user|ai|round[[:space:]　:：]*[0-9]+)))+[[:space:]　·：:，,|/_.-]*$`)

// 流式片段可能从括号中间断开，先等到闭合再区分角色标签和正文括注。
func cleanReplyPrefix(text string, final bool) (string, bool) {
	text = strings.TrimLeftFunc(text, unicode.IsSpace)
	for text != "" {
		first, open := utf8.DecodeRuneInString(text)
		var close string
		switch first {
		case '[':
			close = "]"
		case '［':
			close = "］"
		case '【':
			close = "】"
		case '(':
			close = ")"
		case '（':
			close = "）"
		default:
			return text, true
		}
		at := strings.Index(text[open:], close)
		if at < 0 {
			if final && len(text) <= 160 && rolePrefix.MatchString(text[open:]) {
				return "", true
			}
			return text, final || len(text) > 160
		}
		if at > 160 || !rolePrefix.MatchString(text[open:open+at]) {
			return text, true
		}
		text = strings.TrimLeftFunc(text[open+at+len(close):], func(r rune) bool {
			return unicode.IsSpace(r) || r == ':' || r == '：'
		})
	}
	return "", final
}
