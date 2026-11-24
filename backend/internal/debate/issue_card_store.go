package debate

import (
	"context"
	"errors"
)

// ErrIssueCardNotCommitted 标记一次“确定未提交”的完成写回：CAS 失败、事务回滚或
// 明确的写入拒绝。服务层收到它（或 ErrIssueCardVersionConflict）时，才允许按同一
// attempt 版本落受控失败并让用户显式重试；未带该标记的错误一律按“提交结果不明”处理，
// 既不能宣布失败，也不能自动重跑。
var ErrIssueCardNotCommitted = errors.New("issue_card_not_committed")

// IssueCardStore 是争点卡的持久化边界。所有方法都必须：
//   - 校验会话归属：不属于当前用户一律 ErrNotFound（对外统一 404）；
//   - 在临界区内检查场次资格（已完赛的四轮基础练习），未完成与历史 quick_3 隔离；
//   - 人工编辑与晚到合并都按卡片版本做 CAS，失败返回 ErrIssueCardVersionConflict，
//     且绝不覆盖已保存的人工标题、已合并或被移除的状态。
//
// 生产实现需要把整个方法放进短事务（或等价的串行化边界），
// 内存 Fake 用一把互斥锁模拟同样的临界区语义。
type IssueCardStore interface {
	// LoadIssueCards 只读返回当前会话的争点卡、已保存发言与分析状态；不写库。
	LoadIssueCards(context.Context, uint64, uint64) (IssueCardSnapshot, error)
	// ClaimIssueCardAnalysis 认领一次赛后整理：返回 fresh=false 表示无需分析
	// （已完成/已失败/正在生成），调用方应读回当前状态而不是再次调用模型。
	ClaimIssueCardAnalysis(context.Context, uint64, uint64, bool, uint64) (IssueCardClaim, bool, error)
	// CompleteIssueCardAnalysis 把校验后的候选合并进已有卡片：同源卡按 SourceKey 归并，
	// 已移除卡不复活，已合并来源只补到合并目标卡，新卡总数不超过展示上限。
	//
	// 错误语义必须区分两种写回结果，生产实现不得混淆：
	//   - 确定未提交（事务回滚、写拒绝、CAS 失败）：返回 ErrIssueCardVersionConflict
	//     或 errors.Join(ErrIssueCardNotCommitted, cause)。服务层据此按同一 attempt 落
	//     受控失败，并允许用户显式重试；
	//   - 提交结果不明（提交后连接中断、超时等）：返回未标记的普通错误。服务层不会
	//     宣布失败，也不会自动重跑，只让调用方按状态查询恢复。
	//
	// 成功返回即代表已提交，此后任何失败清理都不得覆盖该完成状态。
	CompleteIssueCardAnalysis(context.Context, uint64, IssueCardClaim, []IssueCardDraft) (IssueCardSnapshot, error)
	// FailIssueCardAnalysis 只保存受控错误码，不清空已成功卡片与人工编辑。
	FailIssueCardAnalysis(context.Context, uint64, IssueCardClaim, string) (IssueCardAnalysis, error)
	// ApplyIssueCardEdit 应用一次人工编辑；版本过期为 ErrIssueCardVersionConflict。
	ApplyIssueCardEdit(context.Context, uint64, uint64, string, IssueCardEditInput) (IssueCardSnapshot, error)
}
