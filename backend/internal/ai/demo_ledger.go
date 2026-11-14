package ai

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"time"
)

// DemoApproval 必须由负责人单独批准；价格为冻结的每百万 Token 微元人民币，不能作为供应商结算。
// 无默认授权值。最坏预留永不退回，换进程仍读取同一账本。
type DemoApproval struct {
	ID                       string    `json:"id"`
	Model                    string    `json:"model"`
	Users                    []uint64  `json:"users"`
	Start                    time.Time `json:"start"`
	End                      time.Time `json:"end"`
	MaxCalls                 int       `json:"max_calls"`
	MaxInputTokens           int64     `json:"max_input_tokens"`
	MaxOutputTokens          int64     `json:"max_output_tokens"`
	MaxCostMicroCNY          int64     `json:"max_cost_micro_cny"`
	InputMicroCNYPerMillion  int64     `json:"input_micro_cny_per_million"`
	OutputMicroCNYPerMillion int64     `json:"output_micro_cny_per_million"`
	PerCallInputTokens       int64     `json:"per_call_input_tokens"`
	PerCallOutputTokens      int64     `json:"per_call_output_tokens"`
}

func (a DemoApproval) valid() bool {
	_, s := a.Start.Zone()
	_, e := a.End.Zone()
	return a.ID != "" && len(a.ID) <= 128 && a.Model == "deepseek-flash" &&
		len(a.Users) == 2 && a.Users[0] > 0 && a.Users[1] > 0 && a.Users[0] != a.Users[1] &&
		s == 0 && e == 0 && a.End.After(a.Start) && a.End.Sub(a.Start) <= 72*time.Hour &&
		a.MaxCalls > 0 && a.MaxCalls <= 1000 &&
		a.MaxInputTokens > 0 && a.MaxInputTokens <= 1000000000 &&
		a.MaxOutputTokens > 0 && a.MaxOutputTokens <= 10000000 &&
		a.MaxCostMicroCNY > 0 && a.MaxCostMicroCNY <= 1000000000000 &&
		a.InputMicroCNYPerMillion > 0 && a.InputMicroCNYPerMillion <= 1000000000 &&
		a.OutputMicroCNYPerMillion > 0 && a.OutputMicroCNYPerMillion <= 1000000000 &&
		a.PerCallInputTokens > 1024 && a.PerCallInputTokens <= 200000 &&
		a.PerCallOutputTokens >= 1024 && a.PerCallOutputTokens <= 8192
}

type demoReservation struct {
	User   uint64    `json:"user"`
	At     time.Time `json:"at"`
	Input  int64     `json:"input_tokens"`
	Output int64     `json:"output_tokens"`
	Cost   int64     `json:"max_cost_micro_cny"`
}
type demoResult struct {
	Outcome string    `json:"outcome"`
	Usage   demoUsage `json:"usage"`
}

type demoHead struct {
	Calls  int    `json:"reserved_calls"`
	Digest string `json:"reservations_sha256"`
}

func (l *demoLedger) head() demoHead {
	raw, _ := json.Marshal(l.reservations)
	return demoHead{Calls: len(l.reservations), Digest: fmt.Sprintf("%x", sha256.Sum256(raw))}
}

// 独立头记录可发现尾部计数文件缺失；先同步预留再原子替换头，崩溃中间态拒绝放行。
// 它不对持有宿主机权限的人提供防篡改保证，操作者不得改写或替换账本卷。
func (l *demoLedger) persistHead(initial bool) error {
	if initial {
		return writeTrialRecord(l.root, "head.json", l.head())
	}
	if err := writeTrialRecord(l.root, "pending-head.json", l.head()); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(l.root, "pending-head.json"), filepath.Join(l.root, "head.json")); err != nil {
		return err
	}
	dir, err := os.Open(l.root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// 账本用 null 区分未知，不把 Go 零值写成已核实的用量；字段遵守 snake_case。
type demoUsage struct {
	Input  *int `json:"input_tokens"`
	Output *int `json:"output_tokens"`
	Total  *int `json:"total_tokens"`
}

func recordedDemoUsage(u Usage) demoUsage {
	value := func(c TokenCount) *int {
		if !c.Known {
			return nil
		}
		return &c.Value
	}
	return demoUsage{Input: value(u.PromptTokens), Output: value(u.CompletionTokens), Total: value(u.TotalTokens)}
}
func (u demoUsage) value() Usage {
	count := func(p *int) TokenCount {
		if p == nil {
			return TokenCount{}
		}
		return KnownCount(*p)
	}
	return Usage{PromptTokens: count(u.Input), CompletionTokens: count(u.Output), TotalTokens: count(u.Total)}
}

type demoCall struct {
	user        uint64
	slot        int
	messages    int
	finished    bool
	reservation demoReservation
}
type demoCallKey struct{}

type demoLedger struct {
	root, bootID string
	approval     DemoApproval
	now          func() time.Time
	mu           sync.Mutex
	lock         *os.File
	active       map[uint64]bool
	reservations []demoReservation
	halted       bool
}

var errDemoDenied = errors.New("本地 Demo 授权、账本或预算不可用")

func openDemoLedger(root string, a DemoApproval) (*demoLedger, error) {
	if !filepath.IsAbs(root) || !a.valid() {
		return nil, errDemoDenied
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errDemoDenied
	}
	lock, err := lockDemo(root)
	if err != nil {
		return nil, errDemoDenied
	}
	l := &demoLedger{root: root, approval: a, bootID: rand.Text(), now: time.Now, lock: lock, active: map[uint64]bool{}}
	fail := func() (*demoLedger, error) { l.stopLocked(); _ = l.close(); return nil, errDemoDenied }
	entries, err := os.ReadDir(root)
	if err != nil {
		return fail()
	}
	initialized := false
	for _, entry := range entries {
		if entry.Name() != "lock" && entry.Name() != "approval.json" {
			initialized = true
		}
	}
	for _, name := range []string{"approval.json", "manifest.json"} {
		if _, err := os.Lstat(filepath.Join(root, name)); errors.Is(err, os.ErrNotExist) {
			if initialized {
				return fail()
			}
			if err := writeTrialRecord(root, name, a); err != nil {
				return fail()
			}
		}
		current, err := readDemoRecord[DemoApproval](root, name)
		if err != nil || !reflect.DeepEqual(current, a) {
			return fail()
		}
	}
	if !initialized {
		if err := l.persistHead(true); err != nil {
			return fail()
		}
	}
	entries, err = os.ReadDir(root)
	if err != nil {
		return fail()
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "approval.json" || name == "manifest.json" || name == "head.json" || name == "lock" || name == "enable.json" || name == "stop" || name == "boot.json" {
			continue
		}
		slot := len(l.reservations) + 1
		if name != fmt.Sprintf("reserve-%06d.json", slot) {
			// ReadDir 按名字排序，结果在预留前；仅在对应预留存在时接受结果名。
			var resultSlot int
			if _, err := fmt.Sscanf(name, "result-%06d.json", &resultSlot); err == nil && resultSlot > 0 && name == fmt.Sprintf("result-%06d.json", resultSlot) {
				continue
			}
			return fail()
		}
		if slot > a.MaxCalls {
			return fail()
		}
		r, err := readDemoRecord[demoReservation](root, name)
		if err != nil || !slices.Contains(a.Users, r.User) || r.At.Before(a.Start) || !r.At.Before(a.End) || r.Input <= 1024 || r.Input > a.PerCallInputTokens || r.Output != a.PerCallOutputTokens || r.Cost != l.cost(r.Input, r.Output) {
			return fail()
		}
		l.reservations = append(l.reservations, r)
		result, err := readDemoRecord[demoResult](root, fmt.Sprintf("result-%06d.json", slot))
		if err != nil || result.Outcome != "success" || !demoUsageValid(result.Usage.value(), r) {
			l.stopLocked()
		}
	}
	// 拒绝孤立结果、额外预留与超预算；不能通过删一份结果恢复费用额度。
	for _, entry := range entries {
		var slot int
		if _, err := fmt.Sscanf(entry.Name(), "result-%06d.json", &slot); err == nil && (slot <= 0 || slot > len(l.reservations)) {
			return fail()
		}
	}
	if head, err := readDemoRecord[demoHead](root, "head.json"); err != nil || head != l.head() {
		return fail()
	}
	var input, output, cost int64
	for _, r := range l.reservations {
		input += r.Input
		output += r.Output
		cost += r.Cost
	}
	if input > a.MaxInputTokens || output > a.MaxOutputTokens || cost > a.MaxCostMicroCNY {
		return fail()
	}
	// boot 只用于人工绑定当前进程，不是计数文件；覆盖不影响任何预算记录。
	if info, err := os.Lstat(filepath.Join(root, "boot.json")); err == nil && !info.Mode().IsRegular() {
		return fail()
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail()
	}
	raw, _ := json.Marshal(map[string]string{"boot_id": l.bootID})
	if err := os.WriteFile(filepath.Join(root, "boot.json"), raw, 0600); err != nil {
		return fail()
	}
	return l, nil
}

func (l *demoLedger) close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lock == nil {
		return nil
	}
	err := l.lock.Close()
	l.lock = nil
	return err
}
func (l *demoLedger) cost(input, output int64) int64 {
	return (input*l.approval.InputMicroCNYPerMillion+999999)/1000000 + (output*l.approval.OutputMicroCNYPerMillion+999999)/1000000
}

func readDemoRecord[T any](root, name string) (record T, err error) {
	info, err := os.Lstat(filepath.Join(root, name))
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return record, errDemoDenied
	}
	file, err := os.Open(filepath.Join(root, name))
	if err != nil {
		return record, errDemoDenied
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil || len(raw) > 8192 {
		return record, errDemoDenied
	}
	// 禁止未知、重复或大小写不符字段，账本变形一律停用。
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil {
		return record, errDemoDenied
	}
	want := make([]string, 0, len(keys))
	for k := range keys {
		want = append(want, k)
	}
	if _, err := decodeStrictJSONObject(raw, want); err != nil {
		return record, errDemoDenied
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return record, err
	}
	canonical, _ := json.Marshal(record)
	var canonicalKeys map[string]json.RawMessage
	_ = json.Unmarshal(canonical, &canonicalKeys)
	if len(keys) != len(canonicalKeys) {
		return record, errDemoDenied
	}
	for k := range keys {
		if _, ok := canonicalKeys[k]; !ok {
			return record, errDemoDenied
		}
	}
	return record, nil
}

func (l *demoLedger) checkLocked(user uint64, lead time.Duration) error {
	if l.lock == nil || l.halted || !slices.Contains(l.approval.Users, user) {
		return errDemoDenied
	}
	if _, err := os.Lstat(filepath.Join(l.root, "stop")); !errors.Is(err, os.ErrNotExist) {
		l.halted = true
		return errDemoDenied
	}
	for _, name := range []string{"approval.json", "manifest.json"} {
		a, err := readDemoRecord[DemoApproval](l.root, name)
		if err != nil || !reflect.DeepEqual(a, l.approval) {
			l.stopLocked()
			return errDemoDenied
		}
	}
	if head, err := readDemoRecord[demoHead](l.root, "head.json"); err != nil || head != l.head() {
		l.stopLocked()
		return errDemoDenied
	}
	approval, err := readDemoRecord[struct {
		BootID string `json:"boot_id"`
	}](l.root, "enable.json")
	now := l.now()
	if err != nil || approval.BootID != l.bootID || now.Before(l.approval.Start) || !now.Add(lead).Before(l.approval.End) {
		return errDemoDenied
	}
	return nil
}

func (l *demoLedger) reserve(ctx context.Context, c *demoCall, input int64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ctx.Err() != nil || c.slot != 0 || input <= 1024 || input > l.approval.PerCallInputTokens || l.checkLocked(c.user, DefaultModelTimeout) != nil {
		return errDemoDenied
	}
	if l.active[c.user] || len(l.active) >= 2 {
		return errDemoDenied
	}
	var in, out, cost int64
	minute, day := 0, 0
	now := l.now().UTC()
	for _, r := range l.reservations {
		if r.At.After(now) {
			l.stopLocked()
			return errDemoDenied
		}
		in += r.Input
		out += r.Output
		cost += r.Cost
		if r.User == c.user {
			if r.At.After(now.Add(-time.Minute)) {
				minute++
			}
			if r.At.UTC().Format(time.DateOnly) == now.Format(time.DateOnly) {
				day++
			}
		}
	}
	r := demoReservation{User: c.user, At: now, Input: input, Output: l.approval.PerCallOutputTokens}
	r.Cost = l.cost(r.Input, r.Output)
	if len(l.reservations) >= l.approval.MaxCalls || in+r.Input > l.approval.MaxInputTokens || out+r.Output > l.approval.MaxOutputTokens || cost+r.Cost > l.approval.MaxCostMicroCNY || minute >= 6 || day >= 20 {
		return errDemoDenied
	}
	c.slot = len(l.reservations) + 1
	c.reservation = r
	if err := writeTrialRecord(l.root, fmt.Sprintf("reserve-%06d.json", c.slot), r); err != nil {
		l.stopLocked()
		return errDemoDenied
	}
	l.reservations = append(l.reservations, r)
	if err := l.persistHead(false); err != nil {
		l.stopLocked()
		return errDemoDenied
	}
	l.active[c.user] = true
	if ctx.Err() != nil || l.checkLocked(c.user, DefaultModelTimeout) != nil {
		l.stopLocked()
		return errDemoDenied
	}
	return nil
}

func demoUsageValid(u Usage, r demoReservation) bool {
	return u.PromptTokens.Known && u.CompletionTokens.Known && u.TotalTokens.Known && u.PromptTokens.Value > 0 && u.CompletionTokens.Value > 0 && int64(u.PromptTokens.Value) <= r.Input && int64(u.CompletionTokens.Value) <= r.Output && u.TotalTokens.Value == u.PromptTokens.Value+u.CompletionTokens.Value
}

func (l *demoLedger) finish(c *demoCall, usage Usage, failure error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if c.slot == 0 {
		return failure
	}
	c.finished = true
	delete(l.active, c.user)
	result := demoResult{Outcome: "halted", Usage: recordedDemoUsage(usage)}
	if failure == nil && demoUsageValid(usage, c.reservation) && l.checkLocked(c.user, 0) == nil {
		result.Outcome = "success"
	}
	if err := writeTrialRecord(l.root, fmt.Sprintf("result-%06d.json", c.slot), result); err != nil {
		l.stopLocked()
		return errDemoDenied
	}
	if result.Outcome != "success" {
		l.stopLocked()
		return errDemoDenied
	}
	return nil
}

func (l *demoLedger) stopLocked() {
	l.halted = true
	_ = writeTrialRecord(l.root, "stop", struct {
		Reason string `json:"reason"`
	}{"reservation_retained"})
}
