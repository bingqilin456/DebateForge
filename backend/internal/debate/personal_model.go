package debate

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const personalModelLifetime = 48 * time.Hour
const personalModelBaseURL = "https://api.deepseek.com"

var errPersonalKeyRequired = &Failure{Status: 409, Code: "model_key_required"}

type personalModelRow struct {
	UserID     uint64 `gorm:"primaryKey"`
	Ciphertext []byte
	ExpiresAt  time.Time
	Provider   string
	BaseURL    string
	Model      string
	Alias      string
	Verified   bool
	Used       bool
}

func (personalModelRow) TableName() string { return "user_model_settings" }

type personalModelStatus struct {
	Enabled    bool       `json:"enabled"`
	Configured bool       `json:"configured"`
	Provider   string     `json:"provider,omitempty"`
	BaseURL    string     `json:"base_url,omitempty"`
	Model      string     `json:"model,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	Alias      string     `json:"alias,omitempty"`
	Verified   bool       `json:"verified"`
	Used       bool       `json:"used"`
}

type personalModelInput struct {
	APIKey   string `json:"api_key"`
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
	Alias    string `json:"alias"`
}

func normalizePersonalModel(in personalModelInput) (personalModelInput, error) {
	endpoints := map[string]string{"deepseek": personalModelBaseURL, "openai": "https://api.openai.com/v1", "doubao": "https://ark.cn-beijing.volces.com/api/v3"}
	switch in.Provider {
	case "deepseek", "openai", "doubao":
		in.BaseURL = endpoints[in.Provider]
	case "custom":
		target, err := url.Parse(in.BaseURL)
		if err != nil || target.Scheme != "https" || target.Hostname() == "" || target.Port() != "" || target.User != nil || target.Fragment != "" || target.RawQuery != "" || target.ForceQuery || len(in.BaseURL) > 512 {
			return in, ErrInvalidRequest
		}
		approved := false
		for _, host := range strings.Split(os.Getenv("BYOK_CUSTOM_HOSTS"), ",") {
			approved = approved || strings.EqualFold(strings.TrimSpace(host), target.Hostname())
		}
		if !approved {
			return in, ErrInvalidRequest
		}
	default:
		return in, ErrInvalidRequest
	}
	in.BaseURL = strings.TrimRight(in.BaseURL, "/")
	in.Model, in.Alias = strings.TrimSpace(in.Model), strings.TrimSpace(in.Alias)
	if !validPersonalKey(in.APIKey) || len(in.Model) == 0 || len(in.Model) > 128 || len(in.Alias) == 0 || len(in.Alias) > 128 {
		return in, ErrInvalidRequest
	}
	return in, nil
}

type PersonalModel struct {
	db     *gorm.DB
	vault  cipher.AEAD
	client *http.Client
	fake   *ai.Fake
}

func NewPersonalModel(db *gorm.DB, key []byte) (*PersonalModel, error) {
	vault, err := newPersonalModelCipher(key)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialPublicPersonalModel
	return &PersonalModel{db: db, vault: vault, fake: ai.NewFake(), client: &http.Client{
		Transport: transport, Timeout: ai.DefaultModelTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func dialPublicPersonalModel(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port != "443" {
		return nil, errors.New("个人模型仅允许 HTTPS")
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range addresses {
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.As4()[0] == 0 ||
			(ip.As4()[0] == 100 && ip.As4()[1]&192 == 64) || ip.As4()[0] >= 224 {
			continue
		}
		conn, err := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("个人模型无可连接的公网地址")
}

func newPersonalModelCipher(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("个人模型加密密钥须为 32 字节")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}

func personalKeyOwner(owner uint64) []byte { return binary.BigEndian.AppendUint64(nil, owner) }

func sealPersonalKey(vault cipher.AEAD, owner uint64, key string) []byte {
	return vault.Seal(nil, nil, []byte(key), personalKeyOwner(owner))
}

func openPersonalKey(vault cipher.AEAD, owner uint64, ciphertext []byte) (string, error) {
	key, err := vault.Open(nil, nil, ciphertext, personalKeyOwner(owner))
	return string(key), err
}

func validPersonalKey(key string) bool {
	if len(key) < 16 || len(key) > 256 {
		return false
	}
	return strings.IndexFunc(key, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) == -1
}

func (p *PersonalModel) status(ctx context.Context, uid uint64) (personalModelStatus, error) {
	if p == nil {
		return personalModelStatus{}, nil
	}
	out := personalModelStatus{Enabled: true}
	var row personalModelRow
	err := p.db.WithContext(ctx).Select("expires_at", "provider", "base_url", "model", "alias", "verified", "used").Where("user_id = ?", uid).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return out, nil
	}
	if err != nil {
		return out, dependency(err, false)
	}
	return row.status(), nil
}

func (row personalModelRow) status() personalModelStatus {
	expiry := row.ExpiresAt.UTC()
	return personalModelStatus{Enabled: true, Configured: expiry.After(time.Now()), Provider: row.Provider, BaseURL: row.BaseURL, Model: row.Model, Alias: row.Alias, ExpiresAt: &expiry, Verified: row.Verified, Used: row.Used}
}

func (p *PersonalModel) save(ctx context.Context, uid uint64, in personalModelInput) (personalModelStatus, error) {
	in, err := normalizePersonalModel(in)
	if err != nil {
		return personalModelStatus{}, err
	}
	expiry := time.Now().UTC().Add(personalModelLifetime)
	row := personalModelRow{UserID: uid, Ciphertext: sealPersonalKey(p.vault, uid, in.APIKey), ExpiresAt: expiry, Provider: in.Provider, BaseURL: in.BaseURL, Model: in.Model, Alias: in.Alias}
	if err := p.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}}, DoUpdates: clause.AssignmentColumns([]string{"ciphertext", "expires_at", "provider", "base_url", "model", "alias", "verified", "used"}),
	}).Create(&row).Error; err != nil {
		return personalModelStatus{}, dependency(err, false)
	}
	return row.status(), nil
}

func (p *PersonalModel) remove(ctx context.Context, uid uint64) error {
	if err := p.db.WithContext(ctx).Where("user_id = ?", uid).Delete(&personalModelRow{}).Error; err != nil {
		return dependency(err, false)
	}
	return nil
}

func (p *PersonalModel) key(ctx context.Context, uid uint64) (personalModelRow, string, error) {
	var row personalModelRow
	err := p.db.WithContext(ctx).Where("user_id = ?", uid).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, "", errPersonalKeyRequired
	}
	if err != nil {
		return row, "", dependency(err, false)
	}
	if !row.ExpiresAt.After(time.Now()) {
		return row, "", errPersonalKeyRequired
	}
	key, err := openPersonalKey(p.vault, uid, row.Ciphertext)
	if err != nil {
		return row, "", dependency(err, false)
	}
	return row, key, nil
}

// testConnection 只检查认证和供应商连通性，GET /models 不请求文本生成。
func (p *PersonalModel) testConnection(ctx context.Context, uid uint64) error {
	row, key, err := p.key(ctx, uid)
	if err != nil {
		return err
	}
	if err := p.testProvider(ctx, row.BaseURL, key); err != nil {
		return err
	}
	if err := p.mark(ctx, row, "verified"); err != nil {
		return dependency(err, false)
	}
	return nil
}

func (p *PersonalModel) mark(ctx context.Context, row personalModelRow, column string) error {
	return p.db.WithContext(ctx).Model(&personalModelRow{}).Where("user_id = ? AND ciphertext = ?", row.UserID, row.Ciphertext).UpdateColumn(column, true).Error
}

func (p *PersonalModel) testProvider(ctx context.Context, baseURL, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return dependency(err, false)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := p.client.Do(req)
	if err != nil {
		var timeout net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
			return &Failure{Status: 504, Code: "model_timeout"}
		}
		return &Failure{Status: 503, Code: "model_unavailable"}
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return &Failure{Status: 422, Code: "model_key_rejected"}
	default:
		return &Failure{Status: 503, Code: "model_unavailable"}
	}
}
