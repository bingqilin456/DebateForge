package ai

import (
	"context"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// Fake 必须与真实入口实现同一个 Generator 接口，编译期即可校验。
func TestFakeImplementsGenerator(t *testing.T) {
	var generator Generator = NewFake()
	if generator == nil {
		t.Fatal("NewFake 不得返回 nil")
	}
}

func TestFakeRecordsDeepCopiedRequestAndPrompt(t *testing.T) {
	fake := NewFake(FakeStep{Result: Result{Content: "AI 回复"}})
	req := validRequest()
	req.RoundNo = 2
	req.CompletedTurns = []CompletedTurn{{RoundNo: 1, UserContent: "历史用户", AssistantContent: "历史 AI"}}

	if _, err := fake.Generate(context.Background(), req); err != nil {
		t.Fatalf("Fake 合法请求不应报错: %v", err)
	}

	// 调用方事后修改原始切片不能污染 Fake 的记录。
	req.CompletedTurns[0].UserContent = "被篡改"
	req.UserContent = "被篡改"

	records := fake.Records()
	if len(records) != 1 {
		t.Fatalf("应记录 1 次调用，实际 %d", len(records))
	}
	if records[0].Request.CompletedTurns[0].UserContent != "历史用户" {
		t.Fatalf("记录的历史内容被调用方后续修改污染: %q", records[0].Request.CompletedTurns[0].UserContent)
	}
	if records[0].Request.UserContent != "这一轮的用户原文" {
		// validRequest 的原文在本测试被覆盖前是固定的，这里直接核对未被篡改。
		if !strings.HasPrefix(records[0].Request.UserContent, "开放式考核") {
			t.Fatalf("记录的本轮原文被污染: %q", records[0].Request.UserContent)
		}
	}

	// 记录的消息必须是同一套 Prompt 构建逻辑的真实产物，而不是另写一份。
	if records[0].Prompt.Version != PromptVersion {
		t.Fatalf("记录 Prompt 版本应为 %q，实际 %q", PromptVersion, records[0].Prompt.Version)
	}
	if len(records[0].Prompt.Messages) != 4 {
		t.Fatalf("第二轮应记录 4 条消息（system+1 轮历史 2 条+本轮 1 条），实际 %d", len(records[0].Prompt.Messages))
	}
	if records[0].Prompt.Messages[0].Role != RoleSystem {
		t.Fatalf("记录的首条消息应为 system，实际 %q", records[0].Prompt.Messages[0].Role)
	}

	// 返回给调用方的副本同样不能被记录内部共享。
	records[0].Request.CompletedTurns[0].UserContent = "再次篡改"
	if fake.Records()[0].Request.CompletedTurns[0].UserContent != "历史用户" {
		t.Fatal("Records() 必须返回深拷贝，不能暴露内部记录")
	}
}

func TestFakeReturnsConfiguredFailureWithoutSuccessContent(t *testing.T) {
	fake := NewFake(FakeStep{Err: &Error{Code: CodeModelTimeout, Message: "回复生成超时"}})
	result, err := fake.Generate(context.Background(), validRequest())
	if err == nil {
		t.Fatal("配置失败步骤时必须返回错误")
	}
	assertCode(t, err, CodeModelTimeout)
	if result.Content != "" {
		t.Fatalf("失败时不得返回可用回复，实际 %q", result.Content)
	}
	if fake.Calls() != 1 {
		t.Fatalf("应记录 1 次调用，实际 %d", fake.Calls())
	}
}

func TestFakeRejectsInvalidRequestBeforeRecordingModelCall(t *testing.T) {
	cases := map[string]func(*Request){
		"空白发言": func(r *Request) { r.UserContent = "  " },
		"超长发言": func(r *Request) { r.UserContent = strings.Repeat("a", 2001) },
		"伪造历史": func(r *Request) { r.RoundNo = 2 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			fake := NewFake()
			req := validRequest()
			mutate(&req)
			if _, err := fake.Generate(context.Background(), req); err == nil {
				t.Fatal("非法请求必须被拒绝")
			} else {
				assertCode(t, err, CodeInvalidInput)
			}
			if fake.Calls() != 0 {
				t.Fatalf("非法请求不得记为模型调用，实际 %d 次", fake.Calls())
			}
		})
	}
}

func TestFakeConcurrentSessionsAreIsolated(t *testing.T) {
	fake := NewFake()
	const sessions = 12
	var wg sync.WaitGroup
	results := make([]Result, sessions)
	errs := make([]error, sessions)
	for i := 0; i < sessions; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := validRequest()
			req.UserContent = "并发会话-" + itoa(i)
			results[i], errs[i] = fake.Generate(context.Background(), req)
		}(i)
	}
	wg.Wait()

	if fake.Calls() != sessions {
		t.Fatalf("应记录 %d 次调用，实际 %d", sessions, fake.Calls())
	}
	seen := make(map[string]bool, sessions)
	for _, record := range fake.Records() {
		if seen[record.Request.UserContent] {
			t.Fatalf("记录中出现重复会话: %q", record.Request.UserContent)
		}
		seen[record.Request.UserContent] = true
	}
	for i := 0; i < sessions; i++ {
		if errs[i] != nil {
			t.Fatalf("会话 %d 不应报错: %v", i, errs[i])
		}
		// 默认 Fake 由请求内容派生回复，可验证每个并发调用拿到自己的结果。
		if results[i].Content != "回声:"+"并发会话-"+itoa(i) {
			t.Fatalf("会话 %d 的回复串台: %q", i, results[i].Content)
		}
	}
}

func TestFakeDefaultResultCarriesUnknownUsageAndMetadata(t *testing.T) {
	fake := NewFake()
	result, err := fake.Generate(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("默认 Fake 不应报错: %v", err)
	}
	if result.Usage.PromptTokens.Known || result.Usage.CompletionTokens.Known || result.Usage.TotalTokens.Known {
		t.Fatal("Fake 默认用量必须是未知，不能用 0 伪装已知")
	}
	if result.Metadata.PromptVersion != PromptVersion {
		t.Fatalf("Fake 元数据应带 Prompt 版本 %q，实际 %q", PromptVersion, result.Metadata.PromptVersion)
	}
}

// 该用例捕获：Fake 默认回声在 2000 码点输入上生成超长回复，绕过与真实适配器相同的长度上限。
func TestFakeDefaultReplyRespectsReplyLimitAtMaxInput(t *testing.T) {
	fake := NewFake()
	req := validRequest()
	req.UserContent = strings.Repeat("𠮷", MaxUserContentCodepoints)
	result, err := fake.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("边界内输入应成功: %v", err)
	}
	if count := utf8.RuneCountInString(result.Content); count > MaxUserContentCodepoints {
		t.Fatalf("Fake 回复 %d 码点超过上限 %d", count, MaxUserContentCodepoints)
	}
	if err := ValidateReply(result.Content); err != nil {
		t.Fatalf("Fake 默认回复必须通过同一回复校验: %v", err)
	}
}

// 该用例捕获：脚本给出的空回复被静默替换为回声，掩盖真实脚本意图。
func TestFakeScriptedEmptyReplyFailsInsteadOfEcho(t *testing.T) {
	fake := NewFake(FakeStep{Result: Result{Content: ""}})
	result, err := fake.Generate(context.Background(), validRequest())
	if err == nil {
		t.Fatalf("空回复脚本必须受控失败，实际返回 %q", result.Content)
	}
	assertCode(t, err, CodeModelError)
	if result.Content != "" {
		t.Fatalf("失败时不得返回内容，实际 %q", result.Content)
	}
}

// 该用例捕获：脚本超长回复被截断或原样返回，绕过回复上限。
func TestFakeScriptedOversizedReplyFails(t *testing.T) {
	fake := NewFake(FakeStep{Result: Result{Content: strings.Repeat("a", MaxUserContentCodepoints+1)}})
	_, err := fake.Generate(context.Background(), validRequest())
	assertCode(t, err, CodeModelError)
}

func TestFakeCanceledContextDoesNotRecordCall(t *testing.T) {
	fake := NewFake()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := fake.Generate(ctx, validRequest())
	assertCode(t, err, CodeModelCanceled)
	if fake.Calls() != 0 {
		t.Fatalf("已取消的请求不得记录模型调用，实际 %d 次", fake.Calls())
	}
}
