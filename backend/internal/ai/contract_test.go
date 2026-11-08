package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// 每个用例说明它要捕获的错误：即哪种生产代码改动会让该用例失败。
func TestValidateRequestAcceptsValidRequest(t *testing.T) {
	if err := ValidateRequest(validRequest()); err != nil {
		t.Fatalf("合法请求不应报错，实际: %v", err)
	}
}

func TestValidateRequestUserContentCodepointBounds(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantErr bool
	}{
		{name: "单字符", content: "好", wantErr: false},
		{name: "2000 个 ASCII", content: strings.Repeat("a", 2000), wantErr: false},
		{name: "2001 个 ASCII", content: strings.Repeat("a", 2001), wantErr: true},
		// 四字节字符必须按码点计数，而不是按字节或 UTF-16 单元。
		{name: "2000 个四字节字符", content: strings.Repeat("𠮷", 2000), wantErr: false},
		{name: "2001 个四字节字符", content: strings.Repeat("𠮷", 2001), wantErr: true},
		{name: "2000 个 emoji 组合", content: strings.Repeat("🙂", 2000), wantErr: false},
		{name: "2001 个 emoji 组合", content: strings.Repeat("🙂", 2001), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest()
			req.UserContent = tc.content
			err := ValidateRequest(req)
			if tc.wantErr {
				if err == nil {
					t.Fatal("超长输入必须被拒绝，不能截断后继续")
				}
				assertCode(t, err, CodeInvalidInput)
				return
			}
			if err != nil {
				t.Fatalf("边界内输入不应报错，实际: %v", err)
			}
		})
	}
}

func TestValidateRequestRejectsBlankAndInvalidUTF8(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "空字符串", content: ""},
		{name: "半角空白", content: "   "},
		{name: "含换行制表符", content: " \n\t "},
		{name: "全角空格", content: "\u3000\u3000"},
		{name: "非法 UTF-8 单字节", content: string([]byte{0xff})},
		{name: "非法 UTF-8 截断序列", content: "正常" + string([]byte{0xe4, 0xbd})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest()
			req.UserContent = tc.content
			err := ValidateRequest(req)
			if err == nil {
				t.Fatal("空白或非法 UTF-8 必须在调用模型前被拒绝")
			}
			assertCode(t, err, CodeInvalidInput)
		})
	}
}

func TestValidateRequestStance(t *testing.T) {
	cases := []struct {
		name    string
		stance  Stance
		wantErr bool
	}{
		{name: "pro", stance: StancePro},
		{name: "con", stance: StanceCon},
		{name: "空值", stance: "", wantErr: true},
		{name: "大写", stance: "PRO", wantErr: true},
		{name: "带空格", stance: " pro", wantErr: true},
		{name: "其他语言", stance: "正方", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest()
			req.UserStance = tc.stance
			err := ValidateRequest(req)
			if tc.wantErr {
				assertCode(t, err, CodeInvalidInput)
				return
			}
			if err != nil {
				t.Fatalf("合法立场不应报错，实际: %v", err)
			}
		})
	}
}

func TestValidateRequestRoundAndHistory(t *testing.T) {
	turn := func(round int) CompletedTurn {
		return CompletedTurn{RoundNo: round, UserContent: "用户观点", AssistantContent: "AI 反驳"}
	}
	cases := []struct {
		name    string
		round   int
		history []CompletedTurn
		wantErr bool
	}{
		{name: "首轮无历史", round: 1},
		{name: "第二轮一条历史", round: 2, history: []CompletedTurn{turn(1)}},
		{name: "第三轮两条历史", round: 3, history: []CompletedTurn{turn(1), turn(2)}},
		{name: "轮次为 0", round: 0, wantErr: true},
		{name: "轮次为 4", round: 4, wantErr: true},
		{name: "第二轮却无历史", round: 2, wantErr: true},
		{name: "第三轮只有一条历史", round: 3, history: []CompletedTurn{turn(1)}, wantErr: true},
		{name: "历史多出未来轮", round: 1, history: []CompletedTurn{turn(1)}, wantErr: true},
		{name: "历史轮号乱序", round: 3, history: []CompletedTurn{turn(2), turn(1)}, wantErr: true},
		{name: "历史轮号重复", round: 3, history: []CompletedTurn{turn(1), turn(1)}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest()
			req.RoundNo = tc.round
			req.CompletedTurns = tc.history
			err := ValidateRequest(req)
			if tc.wantErr {
				assertCode(t, err, CodeInvalidInput)
				return
			}
			if err != nil {
				t.Fatalf("合法轮次与历史不应报错，实际: %v", err)
			}
		})
	}
}

func TestValidateRequestHistoryContentBounds(t *testing.T) {
	base := func() Request {
		req := validRequest()
		req.RoundNo = 2
		req.CompletedTurns = []CompletedTurn{{RoundNo: 1, UserContent: "用户观点", AssistantContent: "AI 反驳"}}
		return req
	}

	t.Run("历史用户发言超长", func(t *testing.T) {
		req := base()
		req.CompletedTurns[0].UserContent = strings.Repeat("a", 2001)
		assertCode(t, ValidateRequest(req), CodeInvalidInput)
	})
	t.Run("历史 AI 回复超长", func(t *testing.T) {
		req := base()
		req.CompletedTurns[0].AssistantContent = strings.Repeat("a", 2001)
		assertCode(t, ValidateRequest(req), CodeInvalidInput)
	})
	t.Run("历史 AI 回复空白", func(t *testing.T) {
		req := base()
		req.CompletedTurns[0].AssistantContent = "  "
		assertCode(t, ValidateRequest(req), CodeInvalidInput)
	})
	t.Run("历史用户发言非法 UTF-8", func(t *testing.T) {
		req := base()
		req.CompletedTurns[0].UserContent = string([]byte{0xff})
		assertCode(t, ValidateRequest(req), CodeInvalidInput)
	})
}

func TestValidateRequestTopicSnapshot(t *testing.T) {
	cases := []struct {
		name  string
		topic Topic
	}{
		{name: "标题为空", topic: Topic{Title: "", Description: "描述"}},
		{name: "标题全空白", topic: Topic{Title: "   ", Description: "描述"}},
		{name: "描述为空", topic: Topic{Title: "标题", Description: ""}},
		{name: "描述非法 UTF-8", topic: Topic{Title: "标题", Description: string([]byte{0xfe})}},
		{name: "标题过长", topic: Topic{Title: strings.Repeat("题", 201), Description: "描述"}},
		{name: "描述过长", topic: Topic{Title: "标题", Description: strings.Repeat("述", 2001)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest()
			req.Topic = tc.topic
			assertCode(t, ValidateRequest(req), CodeInvalidInput)
		})
	}
}

func TestStanceOppositeIsServerDerived(t *testing.T) {
	opposite, err := StancePro.Opposite()
	if err != nil {
		t.Fatalf("pro 的反方应可推导: %v", err)
	}
	if opposite != StanceCon {
		t.Fatalf("pro 的反方应为 con，实际 %q", opposite)
	}
	opposite, err = StanceCon.Opposite()
	if err != nil {
		t.Fatalf("con 的反方应可推导: %v", err)
	}
	if opposite != StancePro {
		t.Fatalf("con 的反方应为 pro，实际 %q", opposite)
	}
	if _, err := Stance("unknown").Opposite(); err == nil {
		t.Fatal("未知立场必须报错，不能默认返回某一方")
	}
}

func TestValidateReply(t *testing.T) {
	cases := []struct {
		name    string
		reply   string
		wantErr bool
	}{
		{name: "正常中文", reply: "统一考核有助于横向比较。"},
		{name: "2000 个四字节字符", reply: strings.Repeat("𠮷", 2000)},
		{name: "2001 个 ASCII", reply: strings.Repeat("a", 2001), wantErr: true},
		{name: "2001 个 emoji", reply: strings.Repeat("🙂", 2001), wantErr: true},
		{name: "空回复", reply: "", wantErr: true},
		{name: "全空白回复", reply: " \n\t ", wantErr: true},
		{name: "非法 UTF-8", reply: string([]byte{0xff}), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateReply(tc.reply)
			if tc.wantErr {
				assertCode(t, err, CodeModelError)
				return
			}
			if err != nil {
				t.Fatalf("合法回复不应报错，实际: %v", err)
			}
		})
	}
}

func assertCode(t *testing.T, err error, want ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望受控错误 %q，实际为 nil", want)
	}
	var controlled *Error
	if !errors.As(err, &controlled) {
		t.Fatalf("期望 *ai.Error，实际 %T: %v", err, err)
	}
	if controlled.Code != want {
		t.Fatalf("期望错误码 %q，实际 %q", want, controlled.Code)
	}
}

func TestKnownCountMarksValueAsKnown(t *testing.T) {
	count := KnownCount(0)
	if !count.Known || count.Value != 0 {
		t.Fatalf("KnownCount(0) 应表示已知的 0，实际 %+v", count)
	}
	if zero := (TokenCount{}); zero.Known {
		t.Fatal("零值 TokenCount 必须表示未知")
	}
}

func TestErrorUnwrapsCauseButHidesItFromMessage(t *testing.T) {
	const secret = "cause-with-secret"
	controlled := &Error{Code: CodeModelUnavailable, Message: "模型供应商暂时不可用"}
	wrapped := controlled.WithCause(errors.New(secret))
	if !errors.Is(wrapped, wrapped.cause) {
		t.Fatal("WithCause 后必须可通过 errors.Is 命中内部 cause")
	}
	if strings.Contains(wrapped.Error(), secret) {
		t.Fatalf("受控错误文本不得包含内部 cause: %s", wrapped.Error())
	}
	if wrapped.Code != controlled.Code || wrapped.Message != controlled.Message {
		t.Fatal("WithCause 不得改变受控错误码与消息")
	}
}

func TestIsCanceledRecognizesCanceledChain(t *testing.T) {
	controlled := (&Error{Code: CodeModelCanceled, Message: "生成请求已取消"}).WithCause(context.Canceled)
	if !IsCanceled(controlled) {
		t.Fatal("取消错误链应被 IsCanceled 识别")
	}
	if IsCanceled(&Error{Code: CodeModelTimeout, Message: "模型生成超时"}) {
		t.Fatal("超时不得被识别为取消")
	}
}
