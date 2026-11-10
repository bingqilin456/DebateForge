package ai

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

const isolatedTrial = "isolated"
const publicTrial = "public"

var errTrialMonitor = errors.New("试运行告警采集不可用")
var errTrialLedger = errors.New("试运行账本不可用")

type trialApproval struct {
	BootID      string    `json:"boot_id"`
	MonitorAt   time.Time `json:"monitor_at"`
	WindowStart time.Time `json:"window_start"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Users       []uint64  `json:"users"`
}

type trialReservation struct {
	WindowStart     time.Time `json:"window_start"`
	At              time.Time `json:"at"`
	Calls           int       `json:"calls"`
	Input           int       `json:"input_tokens"`
	Output          int       `json:"output_tokens"`
	MaxCostMicroCNY int       `json:"max_cost_micro_cny"`
}

type trialResult struct {
	WindowStart      time.Time `json:"window_start"`
	Outcome          string    `json:"outcome"`
	Usage            Usage     `json:"usage"`
	EstimateMicroCNY *int      `json:"peak_uncached_estimate_micro_cny"`
	Settlement       *int      `json:"verified_settlement_micro_cny"`
	Balance          *int      `json:"observed_balance_micro_cny"`
}

// 两套部署挂载同一账本。两枚不可重置的排他记录各预留一次；
// 公网槽还要求隔离槽已记录明确成功，并由负责人单独批准。
type trialLedger struct {
	root, stage, bootID string
	window              trialWindow
	alert               io.Writer
	now                 func() time.Time
	halted              atomic.Bool
	controlMu           sync.Mutex
	observedApproval    *trialApproval
	monitorMu           sync.Mutex
	lastHeartbeat       time.Time
}

func NewTrialGenerator(ctx context.Context, cfg OpenAIConfig, root, stage string, alert io.Writer) (Generator, error) {
	if runtime.GOOS != "linux" || !filepath.IsAbs(root) || alert == nil ||
		(stage != isolatedTrial && stage != publicTrial) ||
		cfg.HTTPClient != nil || cfg.MaxOutputTokens != trialOutputTokens ||
		cfg.Model != "deepseek-flash" || cfg.BaseURL != "https://api.deepseek.com" {
		return nil, errors.New("真实试运行配置无效")
	}
	window, err := readTrialRecord[trialWindow](filepath.Join(root, "window.json"))
	if err != nil || !window.valid() {
		return nil, errors.New("真实试运行窗口未冻结")
	}
	ledger := &trialLedger{root: root, stage: stage, bootID: rand.Text(), window: window, alert: alert, now: time.Now}
	// TLS 仍验证供应商域名，TCP 只经内部中继。中继不可用时拒绝，不回退直连。
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	base := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if network != "tcp" || addr != "api.deepseek.com:443" {
				return nil, errors.New("试运行中继目标无效")
			}
			return dialer.DialContext(ctx, "tcp", "trial-relay:8443")
		},
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: 5 * time.Second,
		Protocols:           new(http.Protocols),
	}
	base.Protocols.SetHTTP1(true)
	cfg.HTTPClient = &http.Client{Timeout: DefaultModelTimeout, Transport: trialTransport{base: base, reserve: ledger.reserve}}
	adapter, err := NewOpenAIGenerator(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := ledger.notify("awaiting_approval", "boot_binding_required"); err != nil {
		return nil, errors.New("试运行告警输出不可用")
	}
	go ledger.watchMonitor(ctx)
	return trialGenerator{adapter: adapter, ledger: ledger}, nil
}

type trialCallKey struct{}
type trialCall struct {
	user     uint64
	reserved bool
}

type trialGenerator struct {
	adapter Generator
	ledger  *trialLedger
}

func (g trialGenerator) Generate(ctx context.Context, req Request) (Result, error) {
	call := &trialCall{user: req.UserID}
	result, err := g.adapter.Generate(context.WithValue(ctx, trialCallKey{}, call), req)
	if !call.reserved {
		return result, err
	}
	usage := result.Usage
	var failure *Error
	if errors.As(err, &failure) {
		usage = failure.Usage
	}
	record := trialResult{WindowStart: g.ledger.window.Start, Outcome: "halted", Usage: usage}
	if saneTrialUsage(usage) {
		estimate := usage.PromptTokens.Value*2 + usage.CompletionTokens.Value*8
		record.EstimateMicroCNY = &estimate
		if err == nil && ctx.Err() == nil {
			record.Outcome = "success"
		}
	}
	var controlErr error
	if record.Outcome == "success" {
		controlErr = g.ledger.checkControl(call.user, 0)
		if controlErr != nil {
			record.Outcome = "halted"
		}
	}
	if writeErr := writeTrialRecord(g.ledger.root, g.ledger.stage+"-result.json", record); writeErr != nil {
		return Result{}, g.ledger.resultFailure("result_persistence_unavailable", usage, errors.Join(err, controlErr, writeErr))
	}
	if record.Outcome != "success" {
		g.ledger.halt()
		_ = g.ledger.notify("halted", "reservation_retained")
		if err != nil {
			return Result{}, err
		}
		return Result{}, (&Error{Code: CodeModelUnavailable, Message: "试运行结果待核对", Usage: usage}).WithCause(controlErr)
	}
	// 结果已同步。确认终端能接收结果告警后，才发布跨环境放行标记。
	// 该事件明确表示放行标记仍待确认，标记写入失败另发停用事件。
	if err := g.ledger.notify("result_persisted", "completion_pending"); err != nil {
		return Result{}, g.ledger.resultFailure("alert_unavailable", usage, err)
	}
	if err := publishTrialCompletion(g.ledger.root, g.ledger.stage, g.ledger.window.Start); err != nil {
		return Result{}, g.ledger.resultFailure("completion_persistence_unavailable", usage, err)
	}
	return result, nil
}

func (l *trialLedger) resultFailure(reason string, usage Usage, cause error) *Error {
	l.halt()
	_ = l.notify("halted", reason)
	return (&Error{Code: CodeModelUnavailable, Message: "试运行结果待核对", Usage: usage}).WithCause(cause)
}

func saneTrialUsage(usage Usage) bool {
	in, out, total := usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens
	return in.Known && out.Known && total.Known && in.Value > 0 && out.Value > 0 &&
		in.Value <= trialInputTokens && out.Value <= trialOutputTokens && total.Value == in.Value+out.Value
}

func (l *trialLedger) reserve(ctx context.Context) error {
	call, ok := ctx.Value(trialCallKey{}).(*trialCall)
	if !ok || call.reserved {
		return errors.New("试运行请求缺少单次身份上下文")
	}
	now := l.now()
	if err := l.checkControl(call.user, DefaultModelTimeout); err != nil {
		reason := "not_approved"
		if errors.Is(err, errTrialMonitor) {
			reason = "monitor_unavailable"
		}
		if errors.Is(err, errTrialLedger) {
			reason = "ledger_unavailable"
		}
		_ = l.notify("denied", reason)
		return err
	}
	// 先让终端看到预留动作。无法发出告警时停在出网之前。
	if err := l.notify("reserving", "one_call_16384_1024_tokens"); err != nil {
		l.halt()
		return err
	}
	record := trialReservation{WindowStart: l.window.Start, At: now.UTC(), Calls: 1,
		Input: trialInputTokens, Output: trialOutputTokens, MaxCostMicroCNY: 40960}
	if err := writeTrialRecord(l.root, l.stage+"-reserved.json", record); err != nil {
		if errors.Is(err, os.ErrExist) {
			_ = l.notify("denied", "quota_spent")
			return err
		}
		l.halt()
		_ = l.notify("halted", "reservation_persistence_unavailable")
		return err
	}
	call.reserved = true
	// 写盘期间收到停用或已到期时，保留全额预留并拒绝发出请求。
	return l.checkControl(call.user, DefaultModelTimeout)
}

func (l *trialLedger) notify(event, reason string) error {
	// 只允许调用点写入固定事件和原因，启动标识只用于绑定人工批准。
	return json.NewEncoder(l.alert).Encode(map[string]string{
		"event": "t07_trial_" + event, "reason": reason, "stage": l.stage,
		"boot_id": l.bootID, "time": l.now().UTC().Format(time.RFC3339Nano),
	})
}

// 即使账本写入失败也锁定当前进程。重启会换 boot ID，任何旧批准均无效。
func (l *trialLedger) halt() {
	if l.halted.Swap(true) {
		return
	}
	if err := writeTrialRecord(l.root, "stop", struct{}{}); err != nil && !errors.Is(err, os.ErrExist) {
		_ = l.notify("halted", "stop_persistence_unavailable")
	}
}

const trialMonitorLease = 15 * time.Second

// 轮询与请求准入使用同一连续性观察，避免在刷新后的下一次轮询前放行。
func (l *trialLedger) monitorAvailable(baseline time.Time) bool {
	l.monitorMu.Lock()
	defer l.monitorMu.Unlock()
	monitor, err := os.Stat(filepath.Join(l.root, "monitor"))
	if err != nil || !monitor.Mode().IsRegular() {
		return false
	}
	at, now := monitor.ModTime(), l.now()
	previous := l.lastHeartbeat
	if previous.IsZero() {
		previous = baseline
	}
	continuous := !previous.IsZero() && !at.Before(previous) && at.Sub(previous) <= trialMonitorLease
	if !continuous || now.Before(at) || now.Sub(at) > trialMonitorLease {
		return false
	}
	l.lastHeartbeat = at
	return true
}

// 调用方持有 controlMu。尚未收到本次启动的批准时可以等待，
// 已观测批准后任何读取故障、移除或替换都要求停用。
func (l *trialLedger) approval() (trialApproval, error) {
	approval, err := readTrialRecord[trialApproval](filepath.Join(l.root, l.stage+"-approval.json"))
	observed := l.observedApproval
	if errors.Is(err, os.ErrNotExist) && observed == nil {
		return trialApproval{}, nil
	}
	if err != nil {
		return trialApproval{}, errors.Join(errTrialLedger, err)
	}
	if observed != nil && (approval.BootID != observed.BootID || !approval.WindowStart.Equal(observed.WindowStart) ||
		!approval.Start.Equal(observed.Start) || !approval.End.Equal(observed.End) ||
		!approval.MonitorAt.Equal(observed.MonitorAt) || !slices.Equal(approval.Users, observed.Users)) {
		return trialApproval{}, errTrialLedger
	}
	if observed == nil && approval.BootID == l.bootID && l.bootID != "" {
		l.observedApproval = &approval
	}
	return approval, nil
}

func (l *trialLedger) watchMonitor(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !l.checkMonitor() {
				return
			}
		}
	}
}

func (l *trialLedger) checkMonitor() bool {
	l.controlMu.Lock()
	defer l.controlMu.Unlock()
	if l.halted.Load() {
		return false
	}
	approval, err := l.approval()
	if err != nil {
		l.halt()
		_ = l.notify("halted", "approval_unavailable")
		return false
	}
	now := l.now()
	if approval.BootID != l.bootID || now.Before(approval.Start) {
		return true
	}
	if _, err := os.Lstat(filepath.Join(l.root, "stop")); !errors.Is(err, os.ErrNotExist) {
		l.halt()
		return false
	}
	if !now.Before(approval.End) || !l.monitorAvailable(approval.MonitorAt) {
		l.halt()
		_ = l.notify("halted", "approval_or_monitor_unavailable")
		return false
	}
	return true
}

func (l *trialLedger) checkControl(user uint64, lead time.Duration) error {
	l.controlMu.Lock()
	defer l.controlMu.Unlock()
	err := l.check(l.now(), user, lead)
	if errors.Is(err, errTrialMonitor) || errors.Is(err, errTrialLedger) {
		l.halt()
	}
	if err == nil && l.halted.Load() {
		return errTrialLedger
	}
	return err
}

func (l *trialLedger) check(now time.Time, user uint64, lead time.Duration) error {
	deny := errors.New("试运行窗口、身份或控制不可用")
	if l.halted.Load() {
		return deny
	}
	if _, err := os.Lstat(filepath.Join(l.root, "stop")); err == nil {
		l.halted.Store(true)
		return deny
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.Join(errTrialLedger, err)
	}
	current, err := readTrialRecord[trialWindow](filepath.Join(l.root, "window.json"))
	if err != nil || !current.valid() || !current.Start.Equal(l.window.Start) ||
		!current.End.Equal(l.window.End) || current.Users[0] != l.window.Users[0] ||
		current.Users[1] != l.window.Users[1] {
		return errors.Join(errTrialLedger, err)
	}
	if now.Before(current.Start) || !now.Before(current.End) {
		return deny
	}
	approval, err := l.approval()
	if err != nil {
		return err
	}
	if l.bootID == "" || approval.BootID != l.bootID || !approval.WindowStart.Equal(current.Start) || approval.Start.Before(current.Start) ||
		approval.End.After(current.End) || now.Before(approval.Start) || !now.Add(lead).Before(approval.End) ||
		len(approval.Users) != 2 || approval.Users[0] == 0 || approval.Users[1] == 0 ||
		approval.Users[0] == approval.Users[1] || (user != approval.Users[0] && user != approval.Users[1]) {
		return deny
	}
	if approval.MonitorAt.Before(approval.Start.Add(-trialMonitorLease)) || approval.MonitorAt.After(approval.Start) ||
		!l.monitorAvailable(approval.MonitorAt) {
		return errTrialMonitor
	}
	// 隔离槽保留两账号账本，实际生成只授予有序名单的首位账号。
	if l.stage == publicTrial {
		prior, resultErr := readTrialRecord[trialResult](filepath.Join(l.root, "isolated-result.json"))
		completed, completedErr := readTrialCompletion(l.root, isolatedTrial)
		reserved, reservedErr := readTrialRecord[trialReservation](filepath.Join(l.root, "isolated-reserved.json"))
		if approval.End.Sub(approval.Start) > 30*time.Minute {
			return deny
		}
		// 公网批准必须在隔离证据就绪后发布。已绑定批准下缺少任一
		// 记录或读到不一致，按账本故障锁定，禁止修好文件后自动恢复。
		if resultErr != nil || completedErr != nil || reservedErr != nil ||
			!completed.Equal(current.Start) || !prior.WindowStart.Equal(current.Start) || !reserved.WindowStart.Equal(current.Start) ||
			reserved.Calls != 1 || reserved.Input != trialInputTokens || reserved.Output != trialOutputTokens ||
			prior.Outcome != "success" || !saneTrialUsage(prior.Usage) {
			return errors.Join(errTrialLedger, resultErr, completedErr, reservedErr)
		}
	} else if approval.Users[0] != current.Users[0] || approval.Users[1] != current.Users[1] || user != current.Users[0] {
		return deny
	}
	return nil
}

func readTrialRecord[T any](path string) (record T, err error) {
	file, err := os.Open(path)
	if err != nil {
		return record, err
	}
	defer file.Close()
	const maxRecord = 8192
	raw, err := io.ReadAll(io.LimitReader(file, maxRecord+1))
	if err != nil {
		return record, err
	}
	if len(raw) > maxRecord {
		return record, errors.New("试运行记录超出长度限制")
	}
	err = json.Unmarshal(raw, &record)
	return record, err
}

// 先同步准备文件及目录，再原子创建同一 inode 的发布链接。读取者只认
// 两个名字指向同一文件的记录。链接在断电后若丢失，只会拒绝公网准入。
func publishTrialCompletion(root, stage string, start time.Time) error {
	prepared := stage + "-completion-prepared.json"
	if err := writeTrialRecord(root, prepared, start); err != nil {
		return err
	}
	return os.Link(filepath.Join(root, prepared), filepath.Join(root, stage+"-completed.json"))
}

func readTrialCompletion(root, stage string) (time.Time, error) {
	path := filepath.Join(root, stage+"-completed.json")
	prepared, err := os.Lstat(filepath.Join(root, stage+"-completion-prepared.json"))
	if err != nil {
		return time.Time{}, err
	}
	completed, err := os.Lstat(path)
	if err != nil {
		return time.Time{}, err
	}
	if !completed.Mode().IsRegular() || !os.SameFile(prepared, completed) {
		return time.Time{}, errors.New("试运行完成记录没有发布证明")
	}
	return readTrialRecord[time.Time](path)
}

func writeTrialRecord(root, name string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	file, err := os.OpenFile(filepath.Join(root, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	// 任一写入或同步结果不明时保留标记。绝不删除或重新分配。
	_, writeErr := file.Write(append(raw, '\n'))
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	return dir.Sync()
}
