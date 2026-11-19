package debate

import (
	"errors"
	"unicode"
	"unicode/utf8"
)

// 本文件只放纯函数：锚点定位与批注结果校验。
// 所有权、会话归属、未来轮消息的排除都由调用方在加载白名单时完成，
// 这里只信任“已经授权并加载的正式消息”，不做任何数据库访问。

const (
	// MaxAnnotationsPerBatch 是每次生成允许的批注条数上限（PRD §5.5 的 0–3 条）。
	MaxAnnotationsPerBatch = 3
	// MaxAnnotationLabelCodepoints 是类别文字标签的上限。
	MaxAnnotationLabelCodepoints = 40
	// MaxAnnotationTextCodepoints 是理由与改进方向的上限。
	MaxAnnotationTextCodepoints = 300
	// MaxAnnotationQuoteCodepoints 是逐字引用的上限，与总结单段文本一致。
	MaxAnnotationQuoteCodepoints = 300
)

// 批注类别固定四种；颜色与线型只是展示层派生，不是第二事实源。
const (
	AnnotationStrength            = "strength"
	AnnotationPossibleLogicGap    = "possible_logic_gap"
	AnnotationInsufficientSupport = "insufficient_support"
	AnnotationUnclearScope        = "unclear_scope"
)

// ErrAnchorNotLocated 表示引用本身非法、不在白名单消息内或在该消息中不存在。
// 这类失败不得把引用回显给客户端。
var ErrAnchorNotLocated = errors.New("annotation_anchor_not_located")

// Anchor 是一个批注锚点。Resolved 为 false 时坐标缺失，前端只显示“原文待核对”，不高亮。
// 坐标是从 0 开始的 Unicode 码点半开区间，按服务端保存的原始消息文本计数。
type Anchor struct {
	MessageID string `json:"message_id"`
	Quote     string `json:"quote"`
	StartCP   *int   `json:"start_cp"`
	EndCP     *int   `json:"end_cp"`
	Resolved  bool   `json:"resolved"`
}

// matchStarts 返回 quote 在 content 中出现的码点起始下标，最多收集 limit 个。
//
// 逐个下标前进，不做“匹配后跳过整段”的步进：那样会把重叠出现算漏。
// 例如原文“哈哈哈”与引用“哈哈”有两处重叠出现，跳过整段只会看到一处，
// 从而把歧义误判成唯一定位。调用方只需要区分“零处、一处、多处”，
// 因此 limit 取 2 即可提前结束，不必遍历全文。
func matchStarts(contract, quote []rune, limit int) []int {
	if limit < 1 || len(quote) == 0 || len(quote) > len(contract) {
		return nil
	}
	var out []int
	for i := 0; i+len(quote) <= len(contract); i++ {
		if contract[i] != quote[0] {
			continue
		}
		match := true
		for j := 1; j < len(quote); j++ {
			if contract[i+j] != quote[j] {
				match = false
				break
			}
		}
		if match {
			out = append(out, i)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

// isBlankText 与其余输入判空口径一致：Unicode White_Space 加上 U+FEFF。
func isBlankText(value string) bool { return trimUnicodeSpace(value) == "" }

func trimUnicodeSpace(value string) string {
	start, end := 0, len(value)
	for start < end {
		r, size := utf8.DecodeRuneInString(value[start:])
		if !unicode.IsSpace(r) && r != '\ufeff' {
			break
		}
		start += size
	}
	for end > start {
		r, size := utf8.DecodeLastRuneInString(value[:end])
		if !unicode.IsSpace(r) && r != '\ufeff' {
			break
		}
		end -= size
	}
	return value[start:end]
}

// ResolveAnchor 在一条已授权、已加载的正式消息里定位逐字引用。
//
// 语义（与设计文档一致）：
//   - 引用必须逐字相等，不 trim、不归一化、不做大小写折叠。
//   - 原句唯一时直接按已保存原文重算坐标。
//   - 原句重复时仅采信能逐字复核的提示偏移；其余保留待核对。
//   - 空引用、全空白、非法 UTF-8 或引用不存在均返回 ErrAnchorNotLocated。
func ResolveAnchor(messageID uint64, content, quote string, hint *int) (Anchor, error) {
	anchor := Anchor{MessageID: formatID(messageID), Quote: quote}
	if messageID == 0 || !utf8.ValidString(content) || !utf8.ValidString(quote) {
		return Anchor{}, ErrAnchorNotLocated
	}
	if quote == "" || isBlankText(quote) {
		return Anchor{}, ErrAnchorNotLocated
	}
	quoteRunes := []rune(quote)
	if len(quoteRunes) > MaxAnnotationQuoteCodepoints {
		return Anchor{}, ErrAnchorNotLocated
	}
	contract := []rune(content)
	found := matchStarts(contract, quoteRunes, 2)
	if len(found) == 0 {
		// 引用不在这条消息里：既无法定位，也没有资格作为“待核对原文”展示。
		return Anchor{}, ErrAnchorNotLocated
	}
	if len(found) == 1 {
		return resolvedAnchor(anchor, found[0], found[0]+len(quoteRunes)), nil
	}
	if hint != nil {
		start := *hint
		// 先做减法再比较：start+len(quote) 在 start 取 MaxInt 时会溢出为负数，
		// 绕过边界判断后直接切片 panic。越界偏移属于坐标失配，
		// 引用本身确实在这条已授权消息里，因此降级为待核对而不是整份拒绝。
		if start < 0 || start > len(contract)-len(quoteRunes) {
			return anchor, nil
		}
		if string(contract[start:start+len(quoteRunes)]) != quote {
			// 坐标失配：不回退，降级为待核对。
			return anchor, nil
		}
		return resolvedAnchor(anchor, start, start+len(quoteRunes)), nil
	}
	return anchor, nil
}

func resolvedAnchor(anchor Anchor, start, end int) Anchor {
	anchor.StartCP = &start
	anchor.EndCP = &end
	anchor.Resolved = true
	return anchor
}

// VerifiedQuote 回读校验：用保存的坐标从原文重新切片，必须与保存的引用逐字相等。
// 原文保存后不再变化，因此该校验只能发现存储或切片错误，不能替代所有权检查。
// 前端回读时必须先按 Unicode 码点拆分（Array.from），不能用 UTF-16 下标。
func VerifiedQuote(content string, start, end int, quote string) bool {
	if start < 0 || end < start {
		return false
	}
	contract := []rune(content)
	if end > len(contract) {
		return false
	}
	return string(contract[start:end]) == quote
}

// AnnotationSource 是一条允许被引用的正式消息：只含触发轮及以前已提交的内容。
type AnnotationSource struct {
	MessageID uint64
	TurnID    uint64
	RoundNo   int
	Role      string
	Content   string
}

// AnnotationAnchorInput 是生成器给出的候选锚点；StartCP 可省略。
type AnnotationAnchorInput struct {
	MessageID uint64
	Quote     string
	StartCP   *int
}

// AnnotationItemInput 是生成器给出的一条批注候选。
type AnnotationItemInput struct {
	Category    string
	Label       string
	Reason      string
	Improvement string
	Anchor      AnnotationAnchorInput
}

// AnnotationBatchInput 是一次批注生成的候选集与允许引用的消息白名单。
type AnnotationBatchInput struct {
	TriggerTurnID  uint64
	TriggerRoundNo int
	Sources        []AnnotationSource
	Items          []AnnotationItemInput
}

// AnnotationItem 是校验并规范化后的批注条目，可直接落库和返回。
type AnnotationItem struct {
	Category    string `json:"category"`
	Label       string `json:"label"`
	Reason      string `json:"reason"`
	Improvement string `json:"improvement"`
	Anchor      Anchor `json:"anchor"`
	// TurnNo 与 Role 是引用消息所在轮次与角色，便于前端按轮分组；不参与锚点坐标。
	TurnNo int    `json:"turn_no"`
	Role   string `json:"role"`
}

// annotationCategories 是固定类别集合。
var annotationCategories = map[string]bool{
	AnnotationStrength:            true,
	AnnotationPossibleLogicGap:    true,
	AnnotationInsufficientSupport: true,
	AnnotationUnclearScope:        true,
}

// IsAnnotationCategory 供 Handler 与前端契约测试复用。
func IsAnnotationCategory(value string) bool { return annotationCategories[value] }

// validAnnotationText 校验标签/理由/改进方向：非空纯文本、合法 UTF-8、码点上限。
func validAnnotationText(value string, maxCodepoints int) bool {
	if !utf8.ValidString(value) || isBlankText(value) {
		return false
	}
	return utf8.RuneCountInString(value) <= maxCodepoints
}

// ValidateAnnotationBatch 校验一次生成结果，返回可保存的条目。
//
// 拒绝整份结果的情形（返回同时匹配 ErrInvalidRequest 的错误，调用方映射为 model_error）：
//   - 超过 3 条、类别非法、文本为空或超长、完全重复条目；
//   - 引用消息不在本次白名单内（跨会话、未来轮、失败轮或非正式消息）；
//   - 引用为空、全空白、非法 UTF-8、越界偏移或在该消息中不存在。
//
// 歧义或坐标失配不是拒绝理由：条目以 Resolved=false 保存，前端显示“原文待核对”。
// 空集合是合法结果：没有批注不等于没有问题。
func ValidateAnnotationBatch(in AnnotationBatchInput) ([]AnnotationItem, error) {
	if in.TriggerTurnID == 0 || in.TriggerRoundNo < 1 || len(in.Sources) == 0 {
		return nil, ErrInvalidRequest
	}
	allowed := make(map[uint64]AnnotationSource, len(in.Sources))
	for _, source := range in.Sources {
		if source.MessageID == 0 || source.TurnID == 0 || source.RoundNo < 1 || source.RoundNo > in.TriggerRoundNo {
			return nil, ErrInvalidRequest
		}
		if source.Role != "user" && source.Role != "assistant" {
			return nil, ErrInvalidRequest
		}
		if _, duplicate := allowed[source.MessageID]; duplicate {
			return nil, ErrInvalidRequest
		}
		allowed[source.MessageID] = source
	}
	if len(in.Items) > MaxAnnotationsPerBatch {
		return nil, ErrInvalidRequest
	}
	items := make([]AnnotationItem, 0, len(in.Items))
	seen := make(map[string]bool, len(in.Items))
	for _, raw := range in.Items {
		item, err := validateAnnotationItem(raw, allowed)
		if err != nil {
			return nil, err
		}
		// 完全重复条目拒绝；同范围不同类别、不同理由都视为不同条目。
		key := item.Category + "\x00" + item.Label + "\x00" + item.Reason + "\x00" + item.Improvement +
			"\x00" + item.Anchor.MessageID + "\x00" + item.Anchor.Quote
		if seen[key] {
			return nil, ErrInvalidRequest
		}
		seen[key] = true
		items = append(items, item)
	}
	return items, nil
}

func validateAnnotationItem(raw AnnotationItemInput, allowed map[uint64]AnnotationSource) (AnnotationItem, error) {
	if !annotationCategories[raw.Category] {
		return AnnotationItem{}, ErrInvalidRequest
	}
	if !validAnnotationText(raw.Label, MaxAnnotationLabelCodepoints) ||
		!validAnnotationText(raw.Reason, MaxAnnotationTextCodepoints) ||
		!validAnnotationText(raw.Improvement, MaxAnnotationTextCodepoints) {
		return AnnotationItem{}, ErrInvalidRequest
	}
	source, ok := allowed[raw.Anchor.MessageID]
	if !ok {
		// 跨会话、未来轮或非正式消息：整份结果拒绝，不回显引用。
		return AnnotationItem{}, errors.Join(ErrInvalidRequest, ErrAnchorNotLocated)
	}
	anchor, err := ResolveAnchor(source.MessageID, source.Content, raw.Anchor.Quote, raw.Anchor.StartCP)
	if err != nil {
		return AnnotationItem{}, errors.Join(ErrInvalidRequest, err)
	}
	return AnnotationItem{
		Category:    raw.Category,
		Label:       raw.Label,
		Reason:      raw.Reason,
		Improvement: raw.Improvement,
		Anchor:      anchor,
		TurnNo:      source.RoundNo,
		Role:        source.Role,
	}, nil
}
