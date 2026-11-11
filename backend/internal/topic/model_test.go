package topic

import (
	"strings"
	"testing"
)

func TestValidateTopicBoundaries(t *testing.T) {
	valid := Topic{ID: 1, Title: strings.Repeat("题", 200), Description: strings.Repeat("述", 2000)}
	if err := validateTopic(valid); err != nil {
		t.Fatalf("边界内辩题应通过校验: %v", err)
	}
	cases := map[string]Topic{
		"零 ID":     {ID: 0, Title: "题", Description: "述"},
		"标题超长":     {ID: 1, Title: strings.Repeat("题", 201), Description: "述"},
		"描述超长":     {ID: 1, Title: "题", Description: strings.Repeat("述", 2001)},
		"标题全空白":    {ID: 1, Title: " \t　", Description: "述"},
		"描述全空白":    {ID: 1, Title: "题", Description: "\n\r "},
		"标题非法UTF8": {ID: 1, Title: string([]byte{0xff, 0xfe}), Description: "述"},
	}
	for name, topic := range cases {
		if err := validateTopic(topic); err == nil {
			t.Fatalf("用例 %s 应被拒绝", name)
		}
	}
}

func TestFormatEntityIDRejectsZero(t *testing.T) {
	if _, err := FormatEntityID(0); err == nil {
		t.Fatal("ID 0 不是合法实体 ID")
	}
	id, err := FormatEntityID(18446744073709551615)
	if err != nil || id != "18446744073709551615" {
		t.Fatalf("BIGINT UNSIGNED 上界格式错误: %q %v", id, err)
	}
}
