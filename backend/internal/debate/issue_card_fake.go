package debate

import (
	"context"
	"sync"
)

// FakeIssueCardAnalyzer 是争点卡分析的确定性替身，仅供 Fake / 内存断言使用。
//
// 它不代表真实模型的输出质量：这里只验证行为契约（幂等、降级、合并、槽位），
// 绝不宣称语义质量已被验证。默认返回空候选集；SetCandidates 用于构造夹具。
type FakeIssueCardAnalyzer struct {
	mu         sync.Mutex
	candidates []IssueCardCandidate
	explicit   bool
	err        error
	calls      int
	gate       chan struct{}
	entered    chan struct{}
	enteredOne sync.Once
}

// NewFakeIssueCardAnalyzer 构造一个默认返回空候选集的替身。
// 空候选集是合法结果：分析成功但没有值得整理的主要争点。
func NewFakeIssueCardAnalyzer() *FakeIssueCardAnalyzer {
	return &FakeIssueCardAnalyzer{entered: make(chan struct{})}
}

// SetCandidates 设置下一次分析返回的候选集；未显式设置时使用默认候选。
func (f *FakeIssueCardAnalyzer) SetCandidates(candidates []IssueCardCandidate) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.candidates = append([]IssueCardCandidate(nil), candidates...)
	f.explicit = true
}

// SetError 注入分析错误；传 nil 清除。
func (f *FakeIssueCardAnalyzer) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// SetGate 让分析阻塞到 gate 关闭，用于验证分析槽忙时的非阻塞行为。
func (f *FakeIssueCardAnalyzer) SetGate(gate chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gate = gate
}

// Calls 返回已执行的分析次数。
func (f *FakeIssueCardAnalyzer) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// Entered 在首次分析真正开始时关闭，供测试等待。
func (f *FakeIssueCardAnalyzer) Entered() <-chan struct{} { return f.entered }

// AnalyzeIssueCards 返回预设候选；未显式设置时按已保存发言生成确定性的默认候选。
// 不访问数据库、不调用真实模型。
func (f *FakeIssueCardAnalyzer) AnalyzeIssueCards(ctx context.Context, req IssueCardRequest) (IssueCardResult, error) {
	if ctx != nil && ctx.Err() != nil {
		return IssueCardResult{}, ctx.Err()
	}
	f.mu.Lock()
	f.calls++
	gate := f.gate
	err := f.err
	candidates := append([]IssueCardCandidate(nil), f.candidates...)
	explicit := f.explicit
	f.mu.Unlock()
	f.enteredOne.Do(func() { close(f.entered) })
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return IssueCardResult{}, ctx.Err()
		}
	}
	if err != nil {
		return IssueCardResult{}, err
	}
	if !explicit {
		candidates = NewDefaultFakeIssueCardCandidates(req.Sources)
	}
	return IssueCardResult{Candidates: candidates}, nil
}

var _ IssueCardAnalyzer = (*FakeIssueCardAnalyzer)(nil)

// NewDefaultFakeIssueCardCandidates 依据已保存发言生成一组可复核的确定性候选，
// 供不逐字构造夹具的联调与前端 Mock 使用：每个用户发言取一条提出位置，
// 该轮对位 AI 回应作为相关回应。文本只是占位样例，不代表真实模型判断。
func NewDefaultFakeIssueCardCandidates(sources []IssueCardSource) []IssueCardCandidate {
	ordered := issueCardSortedSources(sources)
	byRound := make(map[int][]IssueCardSource)
	for _, source := range ordered {
		byRound[source.RoundNo] = append(byRound[source.RoundNo], source)
	}
	candidates := make([]IssueCardCandidate, 0, MaxIssueCardsDisplayed)
	for _, round := range orderedRounds(byRound) {
		messages := byRound[round]
		var origin *IssueCardSource
		var responses []IssueCardSource
		for index := range messages {
			if origin == nil && messages[index].Role == "user" {
				origin = &messages[index]
				continue
			}
			if origin != nil {
				responses = append(responses, messages[index])
				break
			}
		}
		if origin == nil {
			continue
		}
		candidate := IssueCardCandidate{
			Title:       issueCardTrimTitle("争点：" + issueCardQuotePrefix(origin.Content, 16)),
			Explanation: "双方围绕「" + issueCardQuotePrefix(origin.Content, 24) + "」形成交锋，回应是否切中问题需要本人核对双方原句。",
			Origin: IssueCardCitation{
				Anchor:  Anchor{MessageID: formatID(origin.MessageID), Quote: issueCardQuotePrefix(origin.Content, MaxIssueCardQuoteCodepoints)},
				Speaker: origin.Role, TurnNo: origin.RoundNo, Note: "提出位置",
			},
			Pending: []string{"仍待澄清：该主张的前提与适用条件是否被双方共同接受？"},
		}
		for _, response := range responses {
			candidate.Responses = append(candidate.Responses, IssueCardCitation{
				Anchor:  Anchor{MessageID: formatID(response.MessageID), Quote: issueCardQuotePrefix(response.Content, MaxIssueCardQuoteCodepoints)},
				Speaker: response.Role, TurnNo: response.RoundNo, Note: "相关回应是否切中问题，需本人核对。",
			})
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func orderedRounds(byRound map[int][]IssueCardSource) []int {
	rounds := make([]int, 0, len(byRound))
	for round := range byRound {
		rounds = append(rounds, round)
	}
	// 轮次数量很少，直接插入排序保证确定性，不引入额外依赖。
	for i := 1; i < len(rounds); i++ {
		for j := i; j > 0 && rounds[j] < rounds[j-1]; j-- {
			rounds[j], rounds[j-1] = rounds[j-1], rounds[j]
		}
	}
	return rounds
}
