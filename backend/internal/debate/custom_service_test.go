package debate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// 本文件验证私有自定义辩题在完整会话链路中的行为：创建、四轮对辩、总结，
// 以及题目原文只出现在 user 数据段、system 不含用户文本、跨账号不可见。
// 使用内存 Store + ai.Fake，不连接数据库，也不产生任何真实模型调用。

const customTitle = "是否应把「远程办公」设为默认？"
const customBackground = "背景：讨论混合办公的协作成本与人才半径。"

func newCustomDebate(t *testing.T, service *Service) Debate {
	t.Helper()
	d, err := service.Create(context.Background(), 1, CreateInput{
		CustomTopic: &CustomTopic{Title: customTitle, Background: customBackground},
		UserStance:  "pro",
		AIStyle:     "logical",
	})
	if err != nil {
		t.Fatalf("创建自定义会话失败: %v", err)
	}
	return d
}

func TestCustomDebateFullRoundTripKeepsTopicOutOfSystem(t *testing.T) {
	clock := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	store := newMemoryStore(func() time.Time { return clock })
	fake := ai.NewFake()
	service := NewService(store, fake, NewGate(func() time.Time { return clock }))

	created := newCustomDebate(t, service)
	if created.TopicID != nil {
		t.Fatal("自定义会话的 topic_id 必须为空")
	}
	if created.Custom() != true {
		t.Fatal("Custom() 必须由空 topic_id 派生")
	}
	if fake.Calls() != 0 {
		t.Fatal("创建会话不得调用模型")
	}

	// 四轮标准赛：每轮都重新构建完整历史，题目数据段固定挂在第一条 user 消息上。
	for round := 1; round <= 4; round++ {
		content := "第 " + string(rune('0'+round)) + " 轮用户发言"
		data, status, err := service.Submit(context.Background(), 1, created.ID, SubmitInput{
			RoundNo:         round,
			Content:         content,
			ClientRequestID: uuidForRound(round),
		})
		if err != nil || status != 201 || data.CompletedRounds != round {
			t.Fatalf("第 %d 轮失败: status=%d err=%v", round, status, err)
		}
		records := fake.Records()
		if len(records) != round {
			t.Fatalf("第 %d 轮应有 %d 次模型调用，实际 %d", round, round, len(records))
		}
		record := records[len(records)-1]
		if !record.Request.Custom {
			t.Fatalf("第 %d 轮未标记为自定义辩题", round)
		}
		if record.Prompt.Version != ai.CustomPromptVersion {
			t.Fatalf("第 %d 轮应使用自定义 Prompt 版本，实际 %q", round, record.Prompt.Version)
		}
		system := record.Prompt.Messages[0]
		if system.Role != ai.RoleSystem {
			t.Fatalf("第 %d 轮首条消息必须是 system", round)
		}
		if strings.Contains(system.Content, customTitle) || strings.Contains(system.Content, customBackground) {
			t.Fatalf("第 %d 轮的 system 不得包含用户题目原文", round)
		}
		// 数据段只在整场第一条 user 消息上出现一次。
		userMessages := 0
		dataBlocks := 0
		for i, message := range record.Prompt.Messages {
			if message.Role != ai.RoleUser {
				continue
			}
			if strings.Contains(message.Content, customTitle) && strings.Contains(message.Content, customBackground) {
				dataBlocks++
			}
			if i == 1 && !strings.Contains(message.Content, customTitle) {
				t.Fatalf("第 %d 轮的第一条 user 消息应带题目数据段", round)
			}
			userMessages++
		}
		if userMessages != round {
			t.Fatalf("第 %d 轮应有 %d 条 user 消息，实际 %d", round, round, userMessages)
		}
		if dataBlocks != 1 {
			t.Fatalf("第 %d 轮题目数据段应只出现一次，实际 %d", round, dataBlocks)
		}
		last := record.Prompt.Messages[len(record.Prompt.Messages)-1]
		if last.Role != ai.RoleUser || !strings.Contains(last.Content, content) {
			t.Fatalf("第 %d 轮最后一条必须含本轮用户原文", round)
		}
	}

	detail, err := service.Detail(context.Background(), 1, created.ID)
	if err != nil || detail.Debate.Status != "completed" || detail.Debate.CompletedRounds != 4 {
		t.Fatalf("四轮后会话应完成: %+v err=%v", detail.Debate, err)
	}
	// 历史重读仍是同一份快照原文，不受后续任何输入影响。
	dto := detail.DTO()
	if dto.Debate.Topic.ID != nil || dto.Debate.Topic.Title != customTitle || dto.Debate.Topic.Description != customBackground {
		t.Fatalf("历史快照与创建时不一致: %+v", dto.Debate.Topic)
	}

	// 总结同样不把题目放进 system，并使用独立版本。
	summaryStatus, status, err := service.Summary(context.Background(), 1, created.ID, SummaryInput{})
	if err != nil || status != 201 || summaryStatus.Status != "completed" {
		t.Fatalf("总结失败: status=%d err=%v", status, err)
	}
	records := fake.SummaryRecords()
	if len(records) != 1 {
		t.Fatalf("总结应只调用一次模型，实际 %d", len(records))
	}
	if !records[0].Request.Custom || records[0].Prompt.Version != ai.CustomSummaryPromptVersion {
		t.Fatalf("总结未使用自定义结构: %+v", records[0].Request.Custom)
	}
	if strings.Contains(records[0].Prompt.Messages[0].Content, customTitle) {
		t.Fatal("总结 system 不得包含用户题目原文")
	}
	if !strings.Contains(records[0].Prompt.Messages[1].Content, customTitle) {
		t.Fatal("总结第一条 user 消息应带题目数据段")
	}

	// 跨账号不可见：详情与总结都表现为不存在，而不是返回他人内容。
	if _, err := service.Detail(context.Background(), 2, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("其他账号读取详情应得到 ErrNotFound，实际 %v", err)
	}
	if _, err := service.GetSummary(context.Background(), 2, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("其他账号读取总结应得到 ErrNotFound，实际 %v", err)
	}

	// 历史列表只包含本人的自定义会话，且快照可读。
	list, err := service.List(context.Background(), 1, 1, 20)
	if err != nil || list.Total != 1 || len(list.Items) != 1 || list.Items[0].TopicTitleSnapshot != customTitle {
		t.Fatalf("历史列表应包含本人自定义会话: %+v err=%v", list, err)
	}
}

// 预设分支保持旧结构：system 含题目快照、使用 debate-v1，且 topic_id 仍是字符串。
func TestPresetDebateKeepsLegacyPromptStructure(t *testing.T) {
	clock := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	store := newMemoryStore(func() time.Time { return clock })
	fake := ai.NewFake()
	service := NewService(store, fake, NewGate(func() time.Time { return clock }))

	created, err := service.Create(context.Background(), 1, CreateInput{TopicID: topicID(1), UserStance: "pro", AIStyle: "logical"})
	if err != nil || created.Custom() {
		t.Fatalf("预设会话创建失败或误标为自定义: %v", err)
	}
	if _, status, err := service.Submit(context.Background(), 1, created.ID, SubmitInput{
		RoundNo: 1, Content: "首轮立论", ClientRequestID: uuidForRound(1),
	}); err != nil || status != 201 {
		t.Fatalf("预设会话首轮失败: %d %v", status, err)
	}
	record := fake.Records()[0]
	if record.Request.Custom || record.Prompt.Version != ai.PromptVersion {
		t.Fatalf("预设会话必须保持旧 Prompt 版本，实际 %q", record.Prompt.Version)
	}
	if !strings.Contains(record.Prompt.Messages[0].Content, created.TopicTitleSnapshot) {
		t.Fatal("预设会话的 system 应包含题目快照")
	}
	if dto := created.DTO(); dto.Topic.ID == nil || *dto.Topic.ID != "1" {
		t.Fatalf("预设会话 topic.id 必须仍是字符串，实际 %+v", dto.Topic.ID)
	}
}

// uuidForRound 生成轮次内唯一的合法 UUID，避免测试共享可变状态。
func uuidForRound(round int) string {
	return "550e8400-e29b-41d4-a716-44665544" + string(rune('0'+round/10)) + string(rune('0'+round%10)) + "00"
}
