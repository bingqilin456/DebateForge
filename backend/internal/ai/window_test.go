package ai

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// EstimateTokens 必须是保守上界：不得低于字节数，因为 tokenizer 在
// 随机 ASCII 与罕见四字节字符上可能接近“每字节一 token”。
func TestEstimateTokensIsConservativeUpperBound(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{name: "空文本", text: ""},
		{name: "单个 ASCII", text: "a"},
		{name: "随机 ASCII 串", text: "aB3$xZ!09_q"},
		{name: "中文", text: "你好世界"},
		{name: "罕见四字节字符", text: "𠮷𠀀𪚥"},
		{name: "emoji", text: "🙂🙃"},
		{name: "混合", text: "辩论 debate-v1 🙂 𠮷"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EstimateTokens(tc.text)
			bytes := len(tc.text)
			runes := utf8.RuneCountInString(tc.text)
			if bytes == 0 {
				if got != 0 {
					t.Fatalf("空文本估算应为 0，实际 %d", got)
				}
				return
			}
			// 上界口径：不少于字节数，同时必然不少于字符数。
			if got < bytes {
				t.Fatalf("EstimateTokens(%q)=%d 低于字节数 %d，不是保守上界", tc.text, got, bytes)
			}
			if got < runes {
				t.Fatalf("EstimateTokens(%q)=%d 低于字符数 %d", tc.text, got, runes)
			}
		})
	}
}

func TestWorstCaseTurnPromptTokensCoversFullHistory(t *testing.T) {
	got := WorstCaseTurnPromptTokens()
	// 第三轮含 6 条消息、5 条正文（2 轮历史 4 条 + 本轮 1 条），每条最多 2000 码点。
	// 保守上界按 4 字节/码点计，正文部分不少于 5*2000 码点。
	if got < 5*MaxUserContentCodepoints {
		t.Fatalf("最坏一轮 Prompt 估算 %d 明显偏小，无法覆盖完整历史", got)
	}
	// 同时必须有界：system 含标题与描述上限，整体不应超过 100k。
	if got > 100_000 {
		t.Fatalf("最坏一轮 Prompt 估算 %d 过大，需重新核对估算口径", got)
	}
}

func TestWorstCaseSummaryPromptIncludesThirdAssistantReply(t *testing.T) {
	summary := WorstCaseSummaryPromptTokens()
	turn := WorstCaseTurnPromptTokens()
	// 总结需要完整六条消息（含第 3 轮 assistant），并带总结指令，不能小于一轮 Prompt。
	if summary <= turn {
		t.Fatalf("总结最坏输入 %d 应明显大于单轮 Prompt %d，说明未计入完整六条消息", summary, turn)
	}
}

func validWindow() WindowLimits {
	return WindowLimits{
		ContextTokens:              1_000_000,
		MaxOutputTokens:            512,
		SummaryPromptReserveTokens: 60_000,
		SummaryOutputReserveTokens: 4_096,
	}
}

func TestWindowLimitsValidateRejectsUnsetCapabilities(t *testing.T) {
	cases := map[string]func(*WindowLimits){
		"上下文窗口未配置":  func(w *WindowLimits) { w.ContextTokens = 0 },
		"输出上限未配置":   func(w *WindowLimits) { w.MaxOutputTokens = 0 },
		"总结提示预留未配置": func(w *WindowLimits) { w.SummaryPromptReserveTokens = 0 },
		"总结输出预留未配置": func(w *WindowLimits) { w.SummaryOutputReserveTokens = 0 },
		"输出上限超过上下文": func(w *WindowLimits) { w.MaxOutputTokens = w.ContextTokens + 1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			limits := validWindow()
			mutate(&limits)
			if err := limits.Validate(); err == nil {
				t.Fatal("未显式配置或自相矛盾的窗口能力必须被拒绝")
			}
		})
	}
}

func TestWindowLimitsValidateRejectsAbsurdCapabilities(t *testing.T) {
	limits := validWindow()
	limits.ContextTokens = 1 << 40 // 明显荒谬，累加会溢出 int
	if err := limits.Validate(); err == nil {
		t.Fatal("荒谬的窗口能力值必须被拒绝，不能参与溢出计算")
	}
}

func TestWindowLimitsValidateRejectsWindowThatOnlyFitsCurrentRound(t *testing.T) {
	limits := validWindow()
	// 仅够容纳最坏一轮输入加输出，但容纳不了未来总结输入与输出预留。
	limits.ContextTokens = WorstCaseTurnPromptTokens() + limits.MaxOutputTokens
	if err := limits.Validate(); err == nil {
		t.Fatal("窗口必须同时覆盖完整三轮与未来总结预留，不能只让第三轮 fit")
	}
}

func TestWindowLimitsValidateAcceptsConfiguredWindow(t *testing.T) {
	if err := validWindow().Validate(); err != nil {
		t.Fatalf("显式配置且足够大的窗口不应报错: %v", err)
	}
}

func TestValidatePromptReportsWindowExceededForSmallWindow(t *testing.T) {
	req := validRequest()
	req.RoundNo = 3
	req.UserContent = strings.Repeat("𠮷", 2000)
	req.CompletedTurns = []CompletedTurn{
		{RoundNo: 1, UserContent: strings.Repeat("𠮷", 2000), AssistantContent: strings.Repeat("𠮷", 2000)},
		{RoundNo: 2, UserContent: strings.Repeat("𠮷", 2000), AssistantContent: strings.Repeat("𠮷", 2000)},
	}
	prompt, err := BuildPrompt(req)
	if err != nil {
		t.Fatalf("边界内请求应可构建 Prompt: %v", err)
	}
	// 结构合法但明显过小的窗口，应报“窗口不足”而不是“输入非法”。
	limits := WindowLimits{
		ContextTokens:              EstimatePromptTokens(prompt) + 100,
		MaxOutputTokens:            512,
		SummaryPromptReserveTokens: 64,
		SummaryOutputReserveTokens: 64,
	}
	assertCode(t, limits.ValidatePrompt(prompt), CodeWindowExceeded)
}

func TestValidatePromptAcceptsFittingPrompt(t *testing.T) {
	prompt, err := BuildPrompt(validRequest())
	if err != nil {
		t.Fatalf("合法请求应可构建 Prompt: %v", err)
	}
	if err := validWindow().ValidatePrompt(prompt); err != nil {
		t.Fatalf("足够大的窗口应接受该 Prompt: %v", err)
	}
}

func TestRequestBudgetContextIsBoundedBySixtySeconds(t *testing.T) {
	ctx, cancel := NewRequestBudgetContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("请求预算 Context 必须带期限，避免生成请求无限等待")
	}
	remaining := time.Until(deadline)
	if remaining > DefaultRequestBudget || remaining < DefaultRequestBudget-2*time.Second {
		t.Fatalf("请求预算应约为 %v，实际剩余 %v", DefaultRequestBudget, remaining)
	}
}

func TestRequestBudgetContextKeepsShorterParentDeadline(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelParent()
	ctx, cancel := NewRequestBudgetContext(parent)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("派生 Context 必须带期限")
	}
	if remaining := time.Until(deadline); remaining > 5*time.Second {
		t.Fatalf("已有更短父期限时必须保留，实际剩余 %v", remaining)
	}
}

func TestRequestBudgetContextCancelPropagates(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	ctx, cancel := NewRequestBudgetContext(parent)
	defer cancel()
	cancelParent()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("父 Context 取消必须传播到请求预算 Context")
	}
}

func TestModelCallContextUses45SecondCap(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	ctx, cancel := newModelCallContext(parent)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("模型调用必须带 45 秒上限")
	}
	remaining := time.Until(deadline)
	if remaining > DefaultModelTimeout || remaining < DefaultModelTimeout-2*time.Second {
		t.Fatalf("模型预算应约为 %v，实际剩余 %v", DefaultModelTimeout, remaining)
	}
}
