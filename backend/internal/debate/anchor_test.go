package debate

import (
	"errors"
	"math"
	"strings"
	"testing"
)

// 锚点层的表驱动用例：只覆盖纯定位与结果校验，不连接数据库、不调用模型。
// 覆盖中文、emoji 补充平面字符、组合字符、不重复/重复句、带偏移的重复句、
// 越界、非法引用、未来轮/跨消息、0 条与 4 条边界。

func hint(value int) *int { return &value }

func TestResolveAnchorExactUniqueQuote(t *testing.T) {
	content := "我认为应当延长图书馆开放时间。"
	anchor, err := ResolveAnchor(7, content, "延长图书馆开放时间", nil)
	if err != nil {
		t.Fatalf("唯一引用应定位成功: %v", err)
	}
	if !anchor.Resolved || anchor.MessageID != "7" || anchor.StartCP == nil || anchor.EndCP == nil {
		t.Fatalf("缺少坐标: %+v", anchor)
	}
	// "我(0)认(1)为(2)应(3)当(4)延(5)…"：中文按码点计数，不是 UTF-8 字节。
	if *anchor.StartCP != 5 || *anchor.EndCP != 14 {
		t.Fatalf("码点坐标错误: %d-%d", *anchor.StartCP, *anchor.EndCP)
	}
	if !VerifiedQuote(content, *anchor.StartCP, *anchor.EndCP, anchor.Quote) {
		t.Fatal("回读校验必须一致")
	}
}

func TestResolveAnchorSupplementaryPlaneAndCombiningMarks(t *testing.T) {
	content := "🌱理由：e\u0301 与 👍🏽 都保留原码点"
	cases := []struct {
		name  string
		quote string
		start int
	}{
		// emoji 在 Go 的 []rune 中就是一个码点，UTF-16 会算成两个。
		{"emoji 单码点", "🌱", 0},
		// 组合字符 é 拆成 e + U+0301 两个码点，引用必须与原文逐码点一致。
		{"组合字符", "e\u0301", 4},
		// 肤色修饰符是独立码点；切中可见字素（👍 与 🏽 分开）仍按码点定位并保持可回读。
		{"肤色修饰", "👍🏽", 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			anchor, err := ResolveAnchor(9, content, tc.quote, nil)
			if err != nil || !anchor.Resolved {
				t.Fatalf("应定位成功: %+v %v", anchor, err)
			}
			if *anchor.StartCP != tc.start {
				t.Fatalf("起始码点应为 %d，实际 %d", tc.start, *anchor.StartCP)
			}
			if !VerifiedQuote(content, *anchor.StartCP, *anchor.EndCP, tc.quote) {
				t.Fatal("回读校验必须一致")
			}
		})
	}
}

func TestResolveAnchorRepeatedSentenceNeedsHint(t *testing.T) {
	content := "这句话有问题。中间还有别的内容。这句话有问题。"
	// 无提示偏移：出现两次，视为歧义，不猜位置。
	anchor, err := ResolveAnchor(3, content, "这句话有问题。", nil)
	if err != nil {
		t.Fatalf("歧义不是非法引用: %v", err)
	}
	if anchor.Resolved || anchor.StartCP != nil || anchor.EndCP != nil {
		t.Fatalf("歧义必须保持待核对: %+v", anchor)
	}
	// 带正确偏移：落在第二个区间。
	second := strings.Index(content, "。") // 仅用于构造偏移，下面按码点重算
	_ = second
	runes := []rune(content)
	secondStart := len(runes) - len([]rune("这句话有问题。"))
	got, err := ResolveAnchor(3, content, "这句话有问题。", hint(secondStart))
	if err != nil || !got.Resolved {
		t.Fatalf("正确偏移应定位: %+v %v", got, err)
	}
	if *got.StartCP != secondStart {
		t.Fatalf("应落在第二处，实际 %d", *got.StartCP)
	}
	// 带错误偏移：不回退到其它出现位置，降级为待核对。
	wrong, err := ResolveAnchor(3, content, "这句话有问题。", hint(1))
	if err != nil {
		t.Fatalf("坐标失配不是非法引用: %v", err)
	}
	if wrong.Resolved || wrong.StartCP != nil {
		t.Fatalf("坐标失配必须待核对，不得回退: %+v", wrong)
	}
}

func TestResolveAnchorRejectsInvalid(t *testing.T) {
	content := "正常的一句话"
	cases := []struct {
		name    string
		content string
		quote   string
		hint    *int
	}{
		{"空引用", content, "", nil},
		{"全空白", content, "  \u3000\t", nil},
		{"仅 BOM", content, "\ufeff", nil},
		{"引用不存在", content, "不存在的句子", nil},
		{"大小写折叠不接受", "ABC", "abc", nil},
		{"不做归一化", "e\u0301", "é", nil},
		{"不 trim", content, "正常的一句话 ", nil},
		{"非法 UTF-8 原文", "bad\xff", "bad", nil},
		{"非法 UTF-8 引用", content, "\xff", nil},
		{"消息 ID 为 0", content, "正常", nil},
		{"引用超长", content, strings.Repeat("正", MaxAnnotationQuoteCodepoints+1), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := uint64(5)
			if tc.name == "消息 ID 为 0" {
				id = 0
			}
			anchor, err := ResolveAnchor(id, tc.content, tc.quote, tc.hint)
			if !errors.Is(err, ErrAnchorNotLocated) {
				t.Fatalf("应返回 ErrAnchorNotLocated，实际 %v (%+v)", err, anchor)
			}
			if anchor.Quote != "" && err == nil {
				t.Fatal("失败时不得保留可回显的引用")
			}
		})
	}
}

// 重叠出现也必须算作歧义：原文“哈哈哈”里“哈哈”有两处重叠匹配，
// 无提示偏移时不能挑其中一个位置高亮。
func TestResolveAnchorOverlappingRepeats(t *testing.T) {
	cases := []struct {
		name    string
		content string
		quote   string
		hint    *int
	}{
		{"重叠两次无偏移", "哈哈哈", "哈哈", nil},
		{"重叠三次无偏移", "哈哈哈哈", "哈哈", nil},
		{"相邻重复无偏移", "aaaa", "aa", nil},
		{"仅一次不重叠", "haha", "ha", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			anchor, err := ResolveAnchor(11, tc.content, tc.quote, tc.hint)
			if err != nil {
				t.Fatalf("歧义不是非法引用: %v", err)
			}
			unique := len(matchStarts([]rune(tc.content), []rune(tc.quote), 2)) == 1
			if unique != anchor.Resolved {
				t.Fatalf("唯一定位结论错误: unique=%v anchor=%+v", unique, anchor)
			}
			if anchor.Resolved {
				if *anchor.StartCP != 0 {
					t.Fatalf("唯一出现应在 0，实际 %d", *anchor.StartCP)
				}
				if !VerifiedQuote(tc.content, *anchor.StartCP, *anchor.EndCP, tc.quote) {
					t.Fatal("回读校验必须一致")
				}
			} else if anchor.StartCP != nil || anchor.EndCP != nil {
				t.Fatalf("歧义不得返回坐标: %+v", anchor)
			}
		})
	}
	// 带正确偏移时重叠重复仍可定位到指定位置，且不因重叠而拒绝。
	anchor, err := ResolveAnchor(11, "哈哈哈", "哈哈", hint(1))
	if err != nil || !anchor.Resolved || *anchor.StartCP != 1 {
		t.Fatalf("提示偏移应精确定位到 1: %+v %v", anchor, err)
	}
	if !VerifiedQuote("哈哈哈", 1, 3, "哈哈") {
		t.Fatal("重叠位置的切片必须回读一致")
	}
}

// 越界或溢出偏移属于坐标失配：引用确实在这条已授权消息中存在，
// 因此降级为待核对，而不是拒绝整份结果或 panic。
func TestResolveAnchorOutOfRangeHintsDegrade(t *testing.T) {
	content := "正常的一句话，正常的另一句话"
	cases := []struct {
		name string
		hint *int
	}{
		{"越界偏移", hint(99)},
		{"负偏移", hint(-1)},
		{"偏移跨尾部", hint(len([]rune(content)) - 1)},
		// start+len(quote) 在 MaxInt 下会溢出为负数，必须由 start > len-len 拦住。
		{"最大整数偏移", hint(math.MaxInt)},
		{"接近最大整数偏移", hint(math.MaxInt - 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			anchor, err := ResolveAnchor(5, content, "正常", tc.hint)
			if err != nil {
				t.Fatalf("越界偏移应降级而非报错: %v", err)
			}
			if anchor.Resolved || anchor.StartCP != nil || anchor.EndCP != nil {
				t.Fatalf("越界偏移必须待核对且不带坐标: %+v", anchor)
			}
			if anchor.Quote != "正常" {
				t.Fatalf("待核对时可保留引用供已授权展示: %+v", anchor)
			}
		})
	}
}

func TestResolveAnchorRecoversUniqueLiteralAfterIncorrectHint(t *testing.T) {
	for _, offset := range []int{0, -1, math.MaxInt} {
		anchor, err := ResolveAnchor(5, "🌱前文：唯一原句。", "唯一原句", hint(offset))
		if err != nil || !anchor.Resolved || anchor.StartCP == nil || *anchor.StartCP != 4 || anchor.EndCP == nil || *anchor.EndCP != 8 {
			t.Fatalf("偏移 %d 失配后应按唯一原句定位，结果=%+v 错误=%v", offset, anchor, err)
		}
	}
}

// 引用根本不在该消息里时仍必须拒绝：既不能定位，也没有资格作为候选原文展示。
func TestResolveAnchorMissingQuoteIsRejected(t *testing.T) {
	if _, err := ResolveAnchor(5, "正常的一句话", "五个字", hint(0)); !errors.Is(err, ErrAnchorNotLocated) {
		t.Fatalf("不存在的引用应拒绝: %v", err)
	}
}

func TestMatchStartsStopsAtLimit(t *testing.T) {
	if got := matchStarts([]rune("哈哈哈"), []rune("哈哈"), 1); len(got) != 1 {
		t.Fatalf("limit=1 应只返回首处: %v", got)
	}
	if got := matchStarts([]rune("哈哈哈"), []rune("哈哈"), 2); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("应识别两处重叠: %v", got)
	}
	if got := matchStarts([]rune("abc"), []rune("abc"), 2); len(got) != 1 || got[0] != 0 {
		t.Fatalf("单处应返回一处: %v", got)
	}
	for _, tc := range []struct {
		contract, quote []rune
		limit           int
	}{
		{[]rune("abc"), []rune("abcd"), 2},
		{[]rune(""), []rune("a"), 2},
		{[]rune("abc"), []rune(""), 2},
		{[]rune("abc"), []rune("a"), 0},
	} {
		if got := matchStarts(tc.contract, tc.quote, tc.limit); got != nil {
			t.Fatalf("非法输入应返回 nil: %v", got)
		}
	}
}

func TestVerifiedQuoteRejectsBadSlices(t *testing.T) {
	content := "🌱测试文本"
	if !VerifiedQuote(content, 1, 3, "测试") {
		t.Fatal("正确切片应通过")
	}
	if VerifiedQuote(content, 1, 3, "测") {
		t.Fatal("长度不符必须失败")
	}
	if VerifiedQuote(content, 0, 2, "🌱\uFFFD") {
		t.Fatal("内容不符必须失败")
	}
	if VerifiedQuote(content, -1, 2, "🌱") || VerifiedQuote(content, 3, 1, "") || VerifiedQuote(content, 0, 99, "x") {
		t.Fatal("越界或倒序必须失败")
	}
}

func sources(rounds int) []AnnotationSource {
	// 每轮一对 user/assistant，内容故意包含重复句与 emoji。
	out := make([]AnnotationSource, 0, rounds*2)
	for round := 1; round <= rounds; round++ {
		out = append(out,
			AnnotationSource{MessageID: uint64(round*10 + 1), TurnID: uint64(round), RoundNo: round, Role: "user", Content: "用户第" + string(rune('0'+round)) + "轮：这句话有问题。🌱"},
			AnnotationSource{MessageID: uint64(round*10 + 2), TurnID: uint64(round), RoundNo: round, Role: "assistant", Content: "AI 第" + string(rune('0'+round)) + "轮回复。"},
		)
	}
	return out
}

func validItem(messageID uint64) AnnotationItemInput {
	return AnnotationItemInput{
		Category:    AnnotationStrength,
		Label:       "论证有效",
		Reason:      "把结论落在可检验的标准上，因此论证推进有效。",
		Improvement: "可以再补一句为什么该标准适用于本场辩题。",
		Anchor:      AnnotationAnchorInput{MessageID: messageID, Quote: "这句话有问题。"},
	}
}

func TestValidateAnnotationBatchEmptyIsAllowed(t *testing.T) {
	items, err := ValidateAnnotationBatch(AnnotationBatchInput{TriggerTurnID: 1, TriggerRoundNo: 1, Sources: sources(1)})
	if err != nil {
		t.Fatalf("0 条是合法结果: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("必须返回空而非 nil 切片: %#v", items)
	}
}

func TestValidateAnnotationBatchAcceptsThreeAndResolves(t *testing.T) {
	src := sources(2)
	in := AnnotationBatchInput{TriggerTurnID: 2, TriggerRoundNo: 2, Sources: src, Items: []AnnotationItemInput{
		validItem(11),
		{Category: AnnotationInsufficientSupport, Label: "缺少支持", Reason: "只给了结论，没有给出未提供来源之外的依据。", Improvement: "补一条可核验的数据来源。", Anchor: AnnotationAnchorInput{MessageID: 22, Quote: "轮回复"}},
		{Category: AnnotationUnclearScope, Label: "定义待明确", Reason: "“有问题”的范围没有界定。", Improvement: "先界定讨论范围再下判断。", Anchor: AnnotationAnchorInput{MessageID: 21, Quote: "这句话有问题。"}},
	}}
	items, err := ValidateAnnotationBatch(in)
	if err != nil {
		t.Fatalf("3 条合法结果应通过: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("条数错误: %d", len(items))
	}
	// 同范围多类别允许：第 1 条与第 3 条引用同一区间。
	if items[0].Anchor.MessageID != "11" || items[2].Anchor.MessageID != "21" {
		t.Fatalf("条目顺序或归属错误: %+v", items)
	}
	for _, item := range items {
		if !item.Anchor.Resolved {
			t.Fatalf("唯一引用应自动定位: %+v", item.Anchor)
		}
	}
	if items[0].TurnNo != 1 || items[0].Role != "user" || items[1].TurnNo != 2 || items[1].Role != "assistant" {
		t.Fatalf("轮次与角色未从白名单回填: %+v", items)
	}
}

func TestValidateAnnotationBatchRejections(t *testing.T) {
	tooMany := []AnnotationItemInput{validItem(11), validItem(12), validItem(21), validItem(22)}
	duplicate := []AnnotationItemInput{validItem(11), validItem(11)}
	cases := []struct {
		name  string
		in    AnnotationBatchInput
		field string
	}{
		{"四条拒绝", AnnotationBatchInput{TriggerTurnID: 2, TriggerRoundNo: 2, Sources: sources(2), Items: tooMany}, "条数"},
		{"完全重复条目", AnnotationBatchInput{TriggerTurnID: 2, TriggerRoundNo: 2, Sources: sources(2), Items: duplicate}, "重复"},
		{"未知类别", AnnotationBatchInput{TriggerTurnID: 1, TriggerRoundNo: 1, Sources: sources(1), Items: []AnnotationItemInput{func() AnnotationItemInput {
			item := validItem(11)
			item.Category = "fact_error"
			return item
		}()}}, "类别"},
		{"空白标签", AnnotationBatchInput{TriggerTurnID: 1, TriggerRoundNo: 1, Sources: sources(1), Items: []AnnotationItemInput{func() AnnotationItemInput {
			item := validItem(11)
			item.Label = " \u3000 "
			return item
		}()}}, "标签"},
		{"超长理由", AnnotationBatchInput{TriggerTurnID: 1, TriggerRoundNo: 1, Sources: sources(1), Items: []AnnotationItemInput{func() AnnotationItemInput {
			item := validItem(11)
			item.Reason = strings.Repeat("理", MaxAnnotationTextCodepoints+1)
			return item
		}()}}, "理由"},
		{"跨消息引用", AnnotationBatchInput{TriggerTurnID: 1, TriggerRoundNo: 1, Sources: sources(1), Items: []AnnotationItemInput{validItem(99)}}, "白名单"},
		{"未来轮消息", AnnotationBatchInput{TriggerTurnID: 1, TriggerRoundNo: 1, Sources: sources(1), Items: []AnnotationItemInput{validItem(21)}}, "未来轮"},
		{"引用不在该消息", AnnotationBatchInput{TriggerTurnID: 1, TriggerRoundNo: 1, Sources: sources(1), Items: []AnnotationItemInput{func() AnnotationItemInput {
			item := validItem(11)
			item.Anchor.Quote = "不存在的片段"
			return item
		}()}}, "不存在"},
		{"空引用", AnnotationBatchInput{TriggerTurnID: 1, TriggerRoundNo: 1, Sources: sources(1), Items: []AnnotationItemInput{func() AnnotationItemInput {
			item := validItem(11)
			item.Anchor.Quote = ""
			return item
		}()}}, "空引用"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, err := ValidateAnnotationBatch(tc.in)
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("应拒绝（%s）: %v", tc.field, err)
			}
			if items != nil {
				t.Fatalf("拒绝时不得返回条目: %#v", items)
			}
		})
	}
}

func TestValidateAnnotationBatchRejectsBadSources(t *testing.T) {
	bad := []struct {
		name    string
		sources []AnnotationSource
	}{
		{"空白名单", nil},
		{"消息 ID 为 0", []AnnotationSource{{MessageID: 0, TurnID: 1, RoundNo: 1, Role: "user", Content: "文本"}}},
		{"轮次超过触发轮", []AnnotationSource{{MessageID: 1, TurnID: 1, RoundNo: 2, Role: "user", Content: "文本"}}},
		{"非法角色", []AnnotationSource{{MessageID: 1, TurnID: 1, RoundNo: 1, Role: "system", Content: "文本"}}},
		{"重复消息", []AnnotationSource{{MessageID: 1, TurnID: 1, RoundNo: 1, Role: "user", Content: "文本"}, {MessageID: 1, TurnID: 1, RoundNo: 1, Role: "user", Content: "文本"}}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidateAnnotationBatch(AnnotationBatchInput{TriggerTurnID: 1, TriggerRoundNo: 1, Sources: tc.sources}); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("应拒绝白名单: %v", err)
			}
		})
	}
	if _, err := ValidateAnnotationBatch(AnnotationBatchInput{TriggerRoundNo: 1, Sources: sources(1)}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("缺失触发轮 ID 应拒绝")
	}
	if _, err := ValidateAnnotationBatch(AnnotationBatchInput{TriggerTurnID: 1, Sources: sources(1)}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("缺失触发轮号应拒绝")
	}
}

func TestAnnotationCategoriesAreFixed(t *testing.T) {
	for _, name := range []string{AnnotationStrength, AnnotationPossibleLogicGap, AnnotationInsufficientSupport, AnnotationUnclearScope} {
		if !IsAnnotationCategory(name) {
			t.Fatalf("缺少固定类别 %q", name)
		}
	}
	for _, name := range []string{"", "fact_error", "STRENGTH", "possible logic gap"} {
		if IsAnnotationCategory(name) {
			t.Fatalf("非固定类别不应通过 %q", name)
		}
	}
}
