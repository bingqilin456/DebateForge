package ai

import (
	"context"
	"strings"
	"sync"
	"unicode/utf8"
)

// EstimateTokens 返回文本提示 token 的保守上界：直接按 UTF-8 字节数计。
// 供应商 tokenizer 不会把一个 token 拆成多个 byte 之外更差的情形，但对随机
// ASCII 与罕见四字节字符，byte/3 之类的换算可能低估，因此不做比例压缩。
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(text)
}

// EstimatePromptTokens 累加所有消息的保守估算，并计入每条消息的固定开销。
func EstimatePromptTokens(prompt Prompt) int {
	total := 0
	for _, message := range prompt.Messages {
		total += EstimateTokens(message.Content) + messageOverheadTokens
	}
	return total
}

const messageOverheadTokens = 8

// WorstCaseTurnPromptTokens 用真实最坏输入构造 Prompt 再估算，避免手写常量低估。
// 轮数上限由赛制决定，因此取所有已知赛制中最坏的一个（四轮）：
// 三轮完整历史加本轮发言，全部取上限长度，正文用 4 字节字符堆满 2000 码点。
// 用四轮建模可覆盖 quick_3，因为 quick_3 的历史更短，其 Prompt 必然是四轮的子集。
func WorstCaseTurnPromptTokens() int {
	worst := strings.Repeat("𠮷", MaxUserContentCodepoints)
	rounds := MaxRounds()
	completed := make([]CompletedTurn, 0, rounds-1)
	for round := 1; round < rounds; round++ {
		completed = append(completed, CompletedTurn{RoundNo: round, UserContent: worst, AssistantContent: worst})
	}
	req := Request{
		Mode: ModeStandard4,
		Topic: Topic{
			Title:       strings.Repeat("𠮷", MaxTopicTitleCodepoints),
			Description: strings.Repeat("𠮷", MaxTopicDescriptionCodepoints),
		},
		UserStance:     StancePro,
		RoundNo:        rounds,
		UserContent:    worst,
		CompletedTurns: completed,
	}
	prompt, err := BuildPrompt(req)
	if err != nil {
		// 最坏输入必须始终是合法请求；失败说明上限常量或校验被改坏。
		panic("最坏输入无法构建 Prompt: " + err.Error())
	}
	return EstimatePromptTokens(prompt)
}

// MaxRounds 返回所有已知赛制中最大的轮数上限，用于按最坏情况估算预算。
func MaxRounds() int {
	max := 0
	for _, rounds := range roundsByMode {
		if rounds > max {
			max = rounds
		}
	}
	return max
}

// WorstCaseSummaryPromptTokens 估算总结输入的最坏规模：总结需要
// 最坏赛制下全部轮次的正式消息（每轮一 user 一 assistant），加辩题快照与总结指令。
func WorstCaseSummaryPromptTokens() int {
	worst := strings.Repeat("𠮷", MaxUserContentCodepoints)
	conversation := 2 * MaxRounds() * EstimateTokens(worst)
	snapshot := EstimateTokens(strings.Repeat("𠮷", MaxTopicTitleCodepoints)) +
		EstimateTokens(strings.Repeat("𠮷", MaxTopicDescriptionCodepoints))
	// 总结指令段落按 2 KiB 上界计入，避免依赖具体措辞。
	instructions := 2048
	return conversation + snapshot + instructions + messageOverheadTokens*(2*MaxRounds()+2)
}

// WindowLimits 必须由调用方显式配置，未核实的能力不得被默认值掩盖。
// ContextTokens 覆盖输入窗口；MaxOutputTokens 是实际发到传输层的输出上限；
// 两个 Summary* 字段为 T10 的总结提示与总结输出预留空间，本轮不实现总结业务。
type WindowLimits struct {
	ContextTokens              int
	MaxOutputTokens            int
	SummaryPromptReserveTokens int
	SummaryOutputReserveTokens int
}

// Validate 检查显式配置的能力值是否自洽，并确认窗口能覆盖完整三轮加未来总结预留。
// 全程使用 int64 累加，遇到荒谬的能力值直接拒绝，避免整数溢出掩盖窗口不足。
func (w WindowLimits) Validate() error {
	if w.ContextTokens <= 0 {
		return invalidInput("必须显式配置模型上下文窗口 token 数")
	}
	if w.MaxOutputTokens <= 0 {
		return invalidInput("必须显式配置模型输出上限 token 数")
	}
	if w.SummaryPromptReserveTokens <= 0 || w.SummaryOutputReserveTokens <= 0 {
		return invalidInput("必须显式配置未来总结的输入与输出预留")
	}
	// 能力值本身不得超出合理量级，否则后续累加会失真。
	const maxReasonableTokens = 10_000_000
	for name, value := range map[string]int{
		"上下文窗口":  w.ContextTokens,
		"输出上限":   w.MaxOutputTokens,
		"总结输入预留": w.SummaryPromptReserveTokens,
		"总结输出预留": w.SummaryOutputReserveTokens,
	} {
		if value > maxReasonableTokens {
			return invalidInput(name + " token 数超出合理量级")
		}
	}
	if w.MaxOutputTokens >= w.ContextTokens {
		return invalidInput("输出上限必须小于上下文窗口")
	}
	// 未来总结的空间取“调用方显式预留”与“本包最坏估算”的较大者，
	// 避免重复累加导致窗口被不必要地放大。
	summaryPrompt := int64(w.SummaryPromptReserveTokens)
	if worstSummary := int64(WorstCaseSummaryPromptTokens()); worstSummary > summaryPrompt {
		summaryPrompt = worstSummary
	}
	required := summaryPrompt +
		int64(WorstCaseTurnPromptTokens()) +
		int64(w.MaxOutputTokens) +
		int64(w.SummaryOutputReserveTokens)
	if int64(w.ContextTokens) < required {
		return windowExceeded("上下文窗口不足以覆盖完整三轮历史与后续总结输入输出预留")
	}
	return nil
}

// ValidatePrompt 在调用模型前预检：输入估算加上本次输出上限与未来总结预留必须落在窗口内。
func (w WindowLimits) ValidatePrompt(prompt Prompt) error {
	if err := w.Validate(); err != nil {
		return err
	}
	needed := EstimatePromptTokens(prompt) + w.MaxOutputTokens +
		w.SummaryPromptReserveTokens + w.SummaryOutputReserveTokens
	if needed > w.ContextTokens {
		return windowExceeded("上下文窗口不足以覆盖本轮输入、输出与总结预留")
	}
	return nil
}

// NewRequestBudgetContext 为单个生成请求创建 60 秒总预算，并保留更短的父期限。
// 这是纯 Context 辅助：端到端 HTTP 预算仍须由 T05 在请求入口创建并验证。
func NewRequestBudgetContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if deadline, ok := parent.Deadline(); ok {
		if timeUntilDeadline(deadline) <= DefaultRequestBudget {
			return parent, func() {}
		}
	}
	return context.WithTimeout(parent, DefaultRequestBudget)
}

// newModelCallContext 为单次模型调用施加 45 秒上限，同时尊重更短的父期限与取消。
func newModelCallContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if deadline, ok := parent.Deadline(); ok {
		if timeUntilDeadline(deadline) <= DefaultModelTimeout {
			return parent, func() {}
		}
	}
	return context.WithTimeout(parent, DefaultModelTimeout)
}

// Fake 是与真实适配器共用 Generator 接口的可记录实现：深拷贝记录、并发隔离、
// 可按脚本返回失败，用于 T05 等上层测试。
type Fake struct {
	mu             sync.Mutex
	steps          []FakeStep
	index          int
	records        []FakeRecord
	summaryRecords []FakeSummaryRecord
}

// FakeStep 是一次预设的调用结果；Err 非 nil 时返回该错误。
type FakeStep struct {
	Result Result
	Err    error
}

// FakeRecord 是可观察的调用记录，供测试断言服务端构建的真实消息。
type FakeRecord struct {
	Request Request
	Prompt  Prompt
}

// NewFake 创建 Fake；步骤按顺序消费，耗尽后使用确定性回声结果。
func NewFake(steps ...FakeStep) *Fake {
	return &Fake{steps: steps}
}

// Generate 实现 Generator，先跑同一套校验与 Prompt 构建，再记录并返回。
func (f *Fake) Generate(ctx context.Context, req Request) (Result, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return Result{}, &Error{Code: CodeModelCanceled, Message: "生成请求已取消", cause: ctx.Err()}
		default:
		}
	}
	prompt, err := BuildPrompt(req)
	if err != nil {
		return Result{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, FakeRecord{Request: cloneRequest(req), Prompt: clonePrompt(prompt)})

	step := FakeStep{}
	hasStep := false
	if f.index < len(f.steps) {
		step = f.steps[f.index]
		hasStep = true
		f.index++
	}
	if hasStep {
		if step.Err != nil {
			return Result{}, step.Err
		}
		// 脚本结果必须同样通过回复校验：空回复或超长回复不能被静默换成回声。
		result := step.Result
		if err := ValidateReply(result.Content); err != nil {
			return Result{}, err
		}
		if result.Metadata.PromptVersion == "" {
			result.Metadata.PromptVersion = PromptVersion
		}
		return result, nil
	}
	// 未配置脚本时用确定性回声，便于并发隔离断言；用量保持未知。
	return Result{
		Content:  defaultFakeReply(req.UserContent),
		Metadata: Metadata{PromptVersion: PromptVersion, FinishReason: "stop"},
	}, nil
}

// defaultFakeReply 生成默认回声，且必须与真实回复遵守同一长度上限：
// 用户发言已达 2000 码点时，回声不能再追加前缀，否则会超出 2000 码点上限。
func defaultFakeReply(userContent string) string {
	const prefix = "回声:"
	if utf8.RuneCountInString(prefix+userContent) <= MaxUserContentCodepoints {
		return prefix + userContent
	}
	return userContent
}

// Calls 返回已记录的调用次数。
func (f *Fake) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.records)
}

// Records 返回深拷贝记录，调用方修改返回值不会污染 Fake 内部状态。
func (f *Fake) Records() []FakeRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FakeRecord, len(f.records))
	for i, record := range f.records {
		out[i] = FakeRecord{Request: cloneRequest(record.Request), Prompt: clonePrompt(record.Prompt)}
	}
	return out
}

func cloneRequest(req Request) Request {
	clone := req
	if req.CompletedTurns != nil {
		clone.CompletedTurns = make([]CompletedTurn, len(req.CompletedTurns))
		copy(clone.CompletedTurns, req.CompletedTurns)
	}
	return clone
}

func clonePrompt(prompt Prompt) Prompt {
	clone := Prompt{Version: prompt.Version}
	if prompt.Messages != nil {
		clone.Messages = make([]PromptMessage, len(prompt.Messages))
		copy(clone.Messages, prompt.Messages)
	}
	return clone
}

// Compile-time 断言：Fake 必须始终满足业务接口。
var _ Generator = (*Fake)(nil)
