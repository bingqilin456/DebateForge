package debate

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// V2-06/V2-07 争点卡的 Fake 契约测试。
//
// 覆盖：完赛四轮场赛后整理、稳定卡 ID、5 张上限、重复触发与重试幂等、
// 人工改名/合并/移除的版本保护、晚到分析只补兼容内容、失败保留成功结果、
// 归属 404 与未完成/quick_3 隔离、歧义与失效锚点降级为待核对。

func issueCardPtr[T any](value T) *T { return &value }

// issueCardCompletedDebate 构造一场已完赛的四轮练习：每轮一对双方发言。
// 第 2 轮 AI 发言里“可靠口径”出现两次，作为重复歧义夹具。
func issueCardCompletedDebate() (Debate, []Turn) {
	debate := Debate{
		ID: 11, UserID: 1,
		TopicTitleSnapshot: "题目", TopicDescriptionSnapshot: "说明",
		UserStance: "pro", Mode: string(ai.ModeStandard4), AIStyle: string(ai.StyleLogical),
		Status: "completed", CompletedRounds: 4,
	}
	pairs := [][2]string{
		{"第一轮用户发言：标准应当可检验，否则无法比较。", "第一轮回应：可检验不等于可量化，量化成本也要计入。"},
		{"第二轮用户发言：量化成本可以通过抽样控制。", "第二轮回应：抽样本身仍需要可靠口径，可靠口径之外还要说明抽样范围。"},
		{"第三轮用户发言：口径可以由双方事先约定。", "第三轮回应：约定口径不等于约定权重。"},
		{"第四轮用户发言：权重差异可以在结辩前澄清。", "第四轮回应：澄清过晚会影响整场比较。"},
	}
	turns := make([]Turn, 0, len(pairs))
	messageID := uint64(100)
	turnID := uint64(200)
	for index, pair := range pairs {
		turnID++
		messageID++
		user := Message{ID: messageID, TurnID: turnID, Role: "user", Content: pair[0]}
		messageID++
		assistant := Message{ID: messageID, TurnID: turnID, Role: "assistant", Content: pair[1]}
		turns = append(turns, Turn{
			ID: turnID, DebateID: debate.ID, RoundNo: index + 1,
			ClientRequestID: "550e8400-e29b-41d4-a716-44665544000" + string(rune('0'+index)),
			Status:          "completed", AttemptVersion: 1,
			Messages: []Message{user, assistant},
		})
	}
	return debate, turns
}

func issueCardCompletedQuick3() (Debate, []Turn) {
	debate, turns := issueCardCompletedDebate()
	debate.ID = 12
	debate.Mode = string(ai.ModeQuick3)
	debate.CompletedRounds = 3
	turns = turns[:3]
	for i := range turns {
		turns[i].DebateID = debate.ID
	}
	return debate, turns
}

// issueCardFindMessage 在夹具里按原文找消息，返回 message_id / 轮号 / 角色。
func issueCardFindMessage(t *testing.T, turns []Turn, content string) (uint64, int, string) {
	t.Helper()
	for _, turn := range turns {
		for _, message := range turn.Messages {
			if message.Content == content {
				return message.ID, turn.RoundNo, message.Role
			}
		}
	}
	t.Fatalf("夹具中找不到原文：%s", content)
	return 0, 0, ""
}

// issueCardCandidate 用夹具原文构造一条候选；message_id 由夹具反查。
func issueCardCandidate(t *testing.T, turns []Turn, originQuote string, responseQuotes ...string) IssueCardCandidate {
	t.Helper()
	messageID, roundNo, role := issueCardFindMessage(t, turns, originQuote)
	candidate := IssueCardCandidate{
		Title:       "争点：" + firstIssueCardSentence(originQuote),
		Explanation: "双方围绕该判断形成交锋：" + originQuote + " 与 " + strings.Join(responseQuotes, "；") + "。判断是否成立仍需本人核对原文。",
		Origin: IssueCardCitation{
			Anchor:  Anchor{MessageID: formatID(messageID), Quote: originQuote},
			Speaker: role, TurnNo: roundNo, Note: "提出位置",
		},
		Pending: []string{"仍待澄清：该判断的前提是否被双方共同接受？"},
	}
	for _, quote := range responseQuotes {
		responseID, responseRound, responseRole := issueCardFindMessage(t, turns, quote)
		candidate.Responses = append(candidate.Responses, IssueCardCitation{
			Anchor:  Anchor{MessageID: formatID(responseID), Quote: quote},
			Speaker: responseRole, TurnNo: responseRound, Note: "相关回应是否切中问题，需本人核对。",
		})
	}
	return candidate
}

func firstIssueCardSentence(value string) string {
	runes := []rune(value)
	limit := 12
	if len(runes) < limit {
		limit = len(runes)
	}
	return string(runes[:limit])
}

func issueCardFixture(t *testing.T) (*MemoryIssueCardStore, *FakeIssueCardAnalyzer, *IssueCardService, Debate, []Turn) {
	t.Helper()
	clock := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	store := NewMemoryIssueCardStore(func() time.Time { return clock })
	debate, turns := issueCardCompletedDebate()
	store.SeedDebate(debate, turns)
	fake := NewFakeIssueCardAnalyzer()
	service := NewIssueCardService(store, fake)
	return store, fake, service, debate, turns
}

func issueCardIDs(items []IssueCardDTO) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ID)
	}
	return out
}

func issueCardByTitle(items []IssueCardDTO, title string) *IssueCardDTO {
	for index := range items {
		if items[index].Title == title {
			return &items[index]
		}
	}
	return nil
}

func issueCardAssertUniqueAnchors(t *testing.T, card IssueCardDTO) {
	t.Helper()
	seen := make(map[string]bool)
	citations := append([]IssueCardCitation{card.Origin}, card.Responses...)
	for _, citation := range citations {
		key := issueCardAnchorKey(citation.Anchor)
		if seen[key] {
			t.Fatalf("同一张卡出现重复引用：%s", key)
		}
		seen[key] = true
	}
}

func TestIssueCardAnalysisIsIdempotentAndCapped(t *testing.T) {
	_, fake, service, debate, _ := issueCardFixture(t)
	ctx := context.Background()

	list, status, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil {
		t.Fatal(err)
	}
	if status != 201 {
		t.Fatalf("首次整理应为 201，实际 %d", status)
	}
	if list.State != IssueCardStateReady {
		t.Fatalf("完赛四轮场应为 ready，实际 %q", list.State)
	}
	if list.DisplayCap != MaxIssueCardsDisplayed {
		t.Fatalf("展示上限应为 %d，实际 %d", MaxIssueCardsDisplayed, list.DisplayCap)
	}
	if len(list.Items) == 0 || len(list.Items) > MaxIssueCardsDisplayed {
		t.Fatalf("卡片数量应在 1..%d，实际 %d", MaxIssueCardsDisplayed, len(list.Items))
	}
	for _, card := range list.Items {
		if card.ID == "" || card.Title == "" || card.Origin.Anchor.Quote == "" {
			t.Fatalf("卡片缺少稳定身份或标题/提出原文：%+v", card)
		}
		issueCardAssertUniqueAnchors(t, card)
	}

	calls := fake.Calls()
	again, status, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil {
		t.Fatal(err)
	}
	if status != 200 {
		t.Fatalf("重复触发应返回已保存结果，实际 %d", status)
	}
	if fake.Calls() != calls {
		t.Fatal("相同来源重复触发不得再次调用分析")
	}
	if strings.Join(issueCardIDs(list.Items), ",") != strings.Join(issueCardIDs(again.Items), ",") {
		t.Fatal("重复触发不得产生新卡或改变卡 ID")
	}
	for index := range list.Items {
		if list.Items[index].Version != again.Items[index].Version {
			t.Fatal("重复触发不得改变卡片版本")
		}
		issueCardAssertUniqueAnchors(t, again.Items[index])
	}
}

func TestIssueCardAnalysisCapsToFiveCards(t *testing.T) {
	_, fake, service, debate, turns := issueCardFixture(t)
	candidates := make([]IssueCardCandidate, 0, 7)
	for _, turn := range turns {
		for _, message := range turn.Messages {
			if message.Role != "user" {
				continue
			}
			candidates = append(candidates, issueCardCandidate(t, turns, message.Content))
		}
	}
	// 再补三条同场其他候选，制造超过 5 张的输入。
	for _, quote := range []string{
		turns[0].Messages[1].Content, turns[1].Messages[1].Content, turns[2].Messages[1].Content,
	} {
		candidates = append(candidates, issueCardCandidate(t, turns, quote))
	}
	fake.SetCandidates(candidates)
	list, status, err := service.Analyze(context.Background(), 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil || status != 201 {
		t.Fatalf("整理失败: %v %d", err, status)
	}
	if len(list.Items) != MaxIssueCardsDisplayed {
		t.Fatalf("默认最多 %d 张，实际 %d", MaxIssueCardsDisplayed, len(list.Items))
	}
}

func TestIssueCardLateAnalysisKeepsManualTitleAndAppendsOnly(t *testing.T) {
	_, fake, service, debate, turns := issueCardFixture(t)
	ctx := context.Background()
	origin := turns[0].Messages[0].Content
	firstResponse := turns[0].Messages[1].Content
	fake.SetCandidates([]IssueCardCandidate{issueCardCandidate(t, turns, origin, firstResponse)})

	list, _, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("首次整理失败: %v len=%d", err, len(list.Items))
	}
	original := list.Items[0]
	renamed, _, err := service.Edit(ctx, 1, debate.ID, original.ID, IssueCardEditInput{
		ExpectedVersion: original.Version, Action: IssueCardActionRename, Title: issueCardPtr("我的人工标题"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Title != "我的人工标题" || renamed.TitleSource != IssueCardTitleSourceUser {
		t.Fatalf("人工改名未生效：%+v", renamed)
	}

	// 晚到分析：同一提出位置给出不同标题、额外回应与待澄清。
	late := issueCardCandidate(t, turns, origin, firstResponse, turns[1].Messages[1].Content)
	late.Title = "AI 的新标题"
	late.Pending = append(late.Pending, "补充待澄清：抽样口径是否可复核？")
	fake.SetCandidates([]IssueCardCandidate{late})

	after, status, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{Retry: true})
	if err != nil || status != 201 {
		t.Fatalf("重试整理失败: %v %d", err, status)
	}
	if len(after.Items) != 1 {
		t.Fatalf("同一提出位置不得重复建卡，实际 %d 张", len(after.Items))
	}
	card := after.Items[0]
	if card.ID != original.ID {
		t.Fatal("晚到分析不得改变稳定卡 ID")
	}
	if card.Title != "我的人工标题" || card.TitleSource != IssueCardTitleSourceUser {
		t.Fatalf("晚到分析覆盖了人工标题：%+v", card)
	}
	if len(card.Responses) != 2 {
		t.Fatalf("兼容内容应追加相关回应，实际 %d 条", len(card.Responses))
	}
	if len(card.Pending) != 2 {
		t.Fatalf("兼容内容应追加待澄清，实际 %d 条", len(card.Pending))
	}
	if len(card.Notices) == 0 {
		t.Fatal("人工标题与晚到分析冲突须可见")
	}
	if card.Version <= renamed.Version {
		t.Fatal("追加内容后版本应递增，供后续编辑做 CAS")
	}
	issueCardAssertUniqueAnchors(t, card)
}

func TestIssueCardLateAnalysisDoesNotReviveRemovedOrSplitMerged(t *testing.T) {
	_, fake, service, debate, turns := issueCardFixture(t)
	ctx := context.Background()
	originA := turns[0].Messages[0].Content
	originB := turns[1].Messages[0].Content
	originC := turns[2].Messages[0].Content
	fake.SetCandidates([]IssueCardCandidate{
		issueCardCandidate(t, turns, originA), issueCardCandidate(t, turns, originB), issueCardCandidate(t, turns, originC),
	})
	list, _, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil || len(list.Items) != 3 {
		t.Fatalf("首次整理失败: %v len=%d", err, len(list.Items))
	}
	cardA := issueCardByTitle(list.Items, "争点："+firstIssueCardSentence(originA))
	cardB := issueCardByTitle(list.Items, "争点："+firstIssueCardSentence(originB))
	cardC := issueCardByTitle(list.Items, "争点："+firstIssueCardSentence(originC))
	if cardA == nil || cardB == nil || cardC == nil {
		t.Fatalf("缺少夹具卡片: %+v", issueCardIDs(list.Items))
	}

	if _, _, err := service.Edit(ctx, 1, debate.ID, cardA.ID, IssueCardEditInput{
		ExpectedVersion: cardA.Version, Action: IssueCardActionRemove,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Edit(ctx, 1, debate.ID, cardB.ID, IssueCardEditInput{
		ExpectedVersion: cardB.Version, Action: IssueCardActionMerge, TargetCardID: issueCardPtr(cardC.ID),
	}); err != nil {
		t.Fatal(err)
	}

	// 晚到分析仍带着被移除/被合并的来源：不得复活，也不得拆回已合并卡。
	late := issueCardCandidate(t, turns, originB, turns[1].Messages[1].Content)
	fake.SetCandidates([]IssueCardCandidate{
		issueCardCandidate(t, turns, originA),
		late,
		issueCardCandidate(t, turns, originC),
	})
	after, status, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{Retry: true})
	if err != nil || status != 201 {
		t.Fatalf("重试整理失败: %v %d", err, status)
	}
	if len(after.Items) != 1 {
		t.Fatalf("移除与合并后应只剩目标卡，实际 %d 张", len(after.Items))
	}
	target := after.Items[0]
	if target.ID != cardC.ID {
		t.Fatalf("合并目标卡身份应稳定，实际 %q", target.ID)
	}
	if len(target.Responses) == 0 {
		t.Fatal("已合并来源的兼容内容应补充到合并目标卡")
	}
	for _, card := range after.Items {
		if card.Origin.Anchor.Quote == originA || card.Origin.Anchor.Quote == originB {
			t.Fatalf("被移除或被合并的提出位置不得作为独立卡返回：%+v", card)
		}
	}
	issueCardAssertUniqueAnchors(t, target)
}

func TestIssueCardEditVersionConflictKeepsState(t *testing.T) {
	_, fake, service, debate, turns := issueCardFixture(t)
	ctx := context.Background()
	fake.SetCandidates([]IssueCardCandidate{issueCardCandidate(t, turns, turns[0].Messages[0].Content)})
	list, _, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil {
		t.Fatal(err)
	}
	card := list.Items[0]
	updated, _, err := service.Edit(ctx, 1, debate.ID, card.ID, IssueCardEditInput{
		ExpectedVersion: card.Version, Action: IssueCardActionRename, Title: issueCardPtr("第一次改名"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// 旧版本并发编辑：必须 409，且不得覆盖已保存的人工标题。
	_, _, err = service.Edit(ctx, 1, debate.ID, card.ID, IssueCardEditInput{
		ExpectedVersion: card.Version, Action: IssueCardActionRename, Title: issueCardPtr("过期改名"),
	})
	if !errors.Is(err, ErrIssueCardVersionConflict) {
		t.Fatalf("过期版本应为版本冲突，实际 %v", err)
	}
	current, err := service.Get(ctx, 1, debate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Items[0].Title != "第一次改名" || current.Items[0].Version != updated.Version {
		t.Fatalf("版本冲突后状态被改写：%+v", current.Items[0])
	}
}

func TestIssueCardConcurrentEditsUseSingleCASWinner(t *testing.T) {
	_, fake, service, debate, turns := issueCardFixture(t)
	ctx := context.Background()
	fake.SetCandidates([]IssueCardCandidate{issueCardCandidate(t, turns, turns[0].Messages[0].Content)})
	list, _, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil {
		t.Fatal(err)
	}
	card := list.Items[0]
	var wg sync.WaitGroup
	results := make([]error, 2)
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			_, _, err := service.Edit(ctx, 1, debate.ID, card.ID, IssueCardEditInput{
				ExpectedVersion: card.Version, Action: IssueCardActionRename,
				Title: issueCardPtr("并发标题 " + string(rune('A'+slot))),
			})
			results[slot] = err
		}(index)
	}
	wg.Wait()
	success := 0
	conflict := 0
	for _, err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrIssueCardVersionConflict):
			conflict++
		default:
			t.Fatalf("并发编辑出现意外错误: %v", err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("并发编辑应只有一个成功，实际 success=%d conflict=%d", success, conflict)
	}
}

func TestIssueCardAnalysisFailureKeepsExistingCards(t *testing.T) {
	_, fake, service, debate, turns := issueCardFixture(t)
	ctx := context.Background()
	fake.SetCandidates([]IssueCardCandidate{issueCardCandidate(t, turns, turns[0].Messages[0].Content)})
	list, _, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("首次整理失败: %v", err)
	}
	fake.SetError(&ai.Error{Code: ai.CodeModelError})
	if _, _, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{Retry: true}); err == nil {
		t.Fatal("分析失败应返回受控错误")
	}
	failed, err := service.Get(ctx, 1, debate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed.Items) != 1 {
		t.Fatal("分析失败不得清空已成功卡片")
	}
	if failed.Analysis.Status != IssueCardAnalysisFailed || failed.Analysis.ErrorCode == nil {
		t.Fatalf("分析状态应为 failed 且带受控错误码：%+v", failed.Analysis)
	}
	fake.SetError(nil)
	retried, status, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{Retry: true})
	if err != nil || status != 201 {
		t.Fatalf("失败后重试应成功: %v %d", err, status)
	}
	if len(retried.Items) != 1 || retried.Items[0].ID != list.Items[0].ID {
		t.Fatal("重试不得重复建卡或改变卡 ID")
	}
	if retried.Analysis.Status != IssueCardAnalysisCompleted {
		t.Fatalf("重试成功后状态应回到 completed，实际 %q", retried.Analysis.Status)
	}
}

func TestIssueCardOwnershipIs404(t *testing.T) {
	_, fake, service, debate, turns := issueCardFixture(t)
	ctx := context.Background()
	fake.SetCandidates([]IssueCardCandidate{issueCardCandidate(t, turns, turns[0].Messages[0].Content)})
	list, _, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil {
		t.Fatal(err)
	}
	card := list.Items[0]
	if _, err := service.Get(ctx, 2, debate.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("越权读取应为 not_found，实际 %v", err)
	}
	if _, _, err := service.Analyze(ctx, 2, debate.ID, IssueCardAnalyzeInput{Retry: true}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("越权整理应为 not_found，实际 %v", err)
	}
	if _, _, err := service.Edit(ctx, 2, debate.ID, card.ID, IssueCardEditInput{
		ExpectedVersion: card.Version, Action: IssueCardActionRemove,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("越权编辑应为 not_found，实际 %v", err)
	}
}

func TestIssueCardIsolationForUnfinishedAndHistoricalQuick3(t *testing.T) {
	store, fake, service, debate, _ := issueCardFixture(t)
	ctx := context.Background()

	// 未完赛：读取返回空列表与 not_ready，整理与编辑被拒绝。
	active := debate
	active.Status = "active"
	active.CompletedRounds = 2
	store.SeedDebate(active, issueCardCompletedDebateTurns(active.ID)[:2])
	list, err := service.Get(ctx, 1, active.ID)
	if err != nil {
		t.Fatal(err)
	}
	if list.State != IssueCardStateNotReady || len(list.Items) != 0 {
		t.Fatalf("未完赛场应隔离：%+v", list)
	}
	if _, _, err := service.Analyze(ctx, 1, active.ID, IssueCardAnalyzeInput{}); !errors.Is(err, ErrIssueCardNotReady) {
		t.Fatalf("未完赛整理应拒绝，实际 %v", err)
	}
	if fake.Calls() != 0 {
		t.Fatal("未完赛场不得调用分析")
	}

	// 历史 quick_3：即使已完赛也隔离，不产生卡片。
	quick, quickTurns := issueCardCompletedQuick3()
	store.SeedDebate(quick, quickTurns)
	quickList, err := service.Get(ctx, 1, quick.ID)
	if err != nil {
		t.Fatal(err)
	}
	if quickList.State != IssueCardStateUnsupported || len(quickList.Items) != 0 {
		t.Fatalf("历史 quick_3 应隔离：%+v", quickList)
	}
	if _, _, err := service.Analyze(ctx, 1, quick.ID, IssueCardAnalyzeInput{}); !errors.Is(err, ErrIssueCardModeUnsupported) {
		t.Fatalf("quick_3 整理应拒绝，实际 %v", err)
	}
	if fake.Calls() != 0 {
		t.Fatal("quick_3 不得调用分析")
	}
}

func issueCardCompletedDebateTurns(debateID uint64) []Turn {
	_, turns := issueCardCompletedDebate()
	for i := range turns {
		turns[i].DebateID = debateID
	}
	return turns
}

func TestIssueCardAmbiguousAndStaleAnchorsDegradeToPending(t *testing.T) {
	store, fake, service, debate, turns := issueCardFixture(t)
	ctx := context.Background()
	ambiguousMessageID, ambiguousRound, ambiguousRole := issueCardFindMessage(t, turns, turns[1].Messages[1].Content)
	fake.SetCandidates([]IssueCardCandidate{
		{
			Title: "歧义争点", Explanation: "引用出现多次，只显示待核对。",
			Origin: IssueCardCitation{
				Anchor:  Anchor{MessageID: formatID(ambiguousMessageID), Quote: "可靠口径"},
				Speaker: ambiguousRole, TurnNo: ambiguousRound, Note: "提出位置",
			},
		},
		issueCardCandidate(t, turns, turns[0].Messages[0].Content),
	})
	list, _, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil {
		t.Fatal(err)
	}
	ambiguous := issueCardByTitle(list.Items, "歧义争点")
	if ambiguous == nil {
		t.Fatalf("歧义引用仍应保留卡片：%+v", issueCardIDs(list.Items))
	}
	if ambiguous.Origin.Anchor.Resolved || ambiguous.Origin.Anchor.StartCP != nil || ambiguous.Origin.Anchor.EndCP != nil {
		t.Fatalf("重复歧义不得猜测坐标：%+v", ambiguous.Origin.Anchor)
	}
	if ambiguous.Origin.Anchor.Quote != "可靠口径" {
		t.Fatal("待核对卡片仍应保留卡片内容")
	}

	// 失效：已保存锚点对应的权威原文发生变化，读取只降级，不改库也不清空卡片。
	messageID, _, _ := issueCardFindMessage(t, turns, turns[0].Messages[0].Content)
	store.SetMessageContent(debate.ID, messageID, "原文已在夹具中被替换。")
	after, err := service.Get(ctx, 1, debate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Items) != 2 {
		t.Fatal("失效锚点不得删除卡片")
	}
	for _, card := range after.Items {
		if card.Origin.Anchor.MessageID == formatID(messageID) {
			if card.Origin.Anchor.Resolved || card.Origin.Anchor.StartCP != nil || card.Origin.Anchor.EndCP != nil {
				t.Fatalf("失效锚点应降级为待核对：%+v", card.Origin.Anchor)
			}
			if card.Origin.Anchor.Quote == "" || card.Title == "" {
				t.Fatal("待核对卡片仍应可读")
			}
		}
	}
}

func TestIssueCardEditValidation(t *testing.T) {
	_, fake, service, debate, turns := issueCardFixture(t)
	ctx := context.Background()
	fake.SetCandidates([]IssueCardCandidate{
		issueCardCandidate(t, turns, turns[0].Messages[0].Content),
		issueCardCandidate(t, turns, turns[1].Messages[0].Content),
	})
	list, _, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil || len(list.Items) != 2 {
		t.Fatalf("整理失败: %v", err)
	}
	first, second := list.Items[0], list.Items[1]

	cases := []struct {
		name string
		card string
		in   IssueCardEditInput
	}{
		{"空标题", first.ID, IssueCardEditInput{ExpectedVersion: first.Version, Action: IssueCardActionRename, Title: issueCardPtr("   ")}},
		{"超长标题", first.ID, IssueCardEditInput{ExpectedVersion: first.Version, Action: IssueCardActionRename, Title: issueCardPtr(strings.Repeat("长", MaxIssueCardTitleCodepoints+1))}},
		{"缺标题", first.ID, IssueCardEditInput{ExpectedVersion: first.Version, Action: IssueCardActionRename}},
		{"未知动作", first.ID, IssueCardEditInput{ExpectedVersion: first.Version, Action: "archive"}},
		{"合并到自己", first.ID, IssueCardEditInput{ExpectedVersion: first.Version, Action: IssueCardActionMerge, TargetCardID: issueCardPtr(first.ID)}},
		{"合并目标不存在", first.ID, IssueCardEditInput{ExpectedVersion: first.Version, Action: IssueCardActionMerge, TargetCardID: issueCardPtr("9999")}},
		{"缺合并目标", first.ID, IssueCardEditInput{ExpectedVersion: first.Version, Action: IssueCardActionMerge}},
	}
	for _, test := range cases {
		if _, _, err := service.Edit(ctx, 1, debate.ID, test.card, test.in); !errors.Is(err, ErrInvalidRequest) && !errors.Is(err, ErrIssueCardNotFound) {
			t.Fatalf("%s 应被拒绝，实际 %v", test.name, err)
		}
	}
	// 合法合并后，被合并卡不能再作为合并目标或再次编辑。
	if _, _, err := service.Edit(ctx, 1, debate.ID, first.ID, IssueCardEditInput{
		ExpectedVersion: first.Version, Action: IssueCardActionMerge, TargetCardID: issueCardPtr(second.ID),
	}); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(ctx, 1, debate.ID)
	if err != nil {
		t.Fatal(err)
	}
	var mergedVersion uint64
	for _, card := range current.Items {
		if card.ID == first.ID {
			t.Fatal("被合并卡不应出现在活动卡片里")
		}
		if card.ID == second.ID {
			mergedVersion = card.Version
		}
	}
	if _, _, err := service.Edit(ctx, 1, debate.ID, first.ID, IssueCardEditInput{
		ExpectedVersion: mergedVersion, Action: IssueCardActionRename, Title: issueCardPtr("拆回已合并卡"),
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("已合并卡不得被拆回或编辑，实际 %v", err)
	}
}

func TestIssueCardAnalyzeSlotBusyReturns202(t *testing.T) {
	_, fake, service, debate, turns := issueCardFixture(t)
	gate := make(chan struct{})
	fake.SetGate(gate)
	fake.SetCandidates([]IssueCardCandidate{issueCardCandidate(t, turns, turns[0].Messages[0].Content)})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = service.Analyze(context.Background(), 1, debate.ID, IssueCardAnalyzeInput{})
	}()
	select {
	case <-fake.Entered():
	case <-time.After(2 * time.Second):
		t.Fatal("分析未进入执行阶段")
	}
	busy, status, err := service.Analyze(context.Background(), 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil {
		t.Fatal(err)
	}
	if status != 202 {
		t.Fatalf("分析槽忙时应返回 202，实际 %d", status)
	}
	if len(busy.Items) != 0 {
		t.Fatal("忙时应只返回当前状态，不排队")
	}
	close(gate)
	<-done
	if fake.Calls() != 1 {
		t.Fatalf("忙时不得启动第二次分析，实际调用 %d 次", fake.Calls())
	}
}

func TestIssueCardFifthCardIsNotReplacedByLaterCandidate(t *testing.T) {
	_, fake, service, debate, turns := issueCardFixture(t)
	// 前 5 条候选占满名额，后面第 6 条不得挤掉已存在的卡（稳定身份优先）。
	candidates := make([]IssueCardCandidate, 0, 6)
	for _, turn := range turns {
		candidates = append(candidates, issueCardCandidate(t, turns, turn.Messages[0].Content))
	}
	candidates = append(candidates, issueCardCandidate(t, turns, turns[0].Messages[1].Content))
	fake.SetCandidates(candidates)
	list, _, err := service.Analyze(context.Background(), 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != MaxIssueCardsDisplayed {
		t.Fatalf("应为 %d 张，实际 %d", MaxIssueCardsDisplayed, len(list.Items))
	}
	firstIDs := issueCardIDs(list.Items)
	fake.SetCandidates(candidates)
	after, _, err := service.Analyze(context.Background(), 1, debate.ID, IssueCardAnalyzeInput{Retry: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(firstIDs, ",") != strings.Join(issueCardIDs(after.Items), ",") {
		t.Fatal("重复整理不得替换或重排既有卡片")
	}
}
