// Package service dispatches protocol requests to the local quota engine.
// It enforces the local refresh gate before any network I/O.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/channels"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/config"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/model"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/protocol"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/quota"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/version"
)

// Service handles one client's protocol requests.
type Service struct {
	cfg   *config.Config
	cpa   *quota.Collector
	local *channels.Collector
}

// New builds a service.
func New(cfg *config.Config) *Service {
	return &Service{cfg: cfg, cpa: quota.New(cfg), local: channels.New(cfg)}
}

// Capabilities reports local abilities (authoritative).
func (s *Service) Capabilities() protocol.Capabilities {
	list := append([]string{}, quota.CPAChannels...)
	list = append(list, channels.Supported()...)
	return protocol.Capabilities{Refresh: s.cfg.Refresh.Enabled, Channels: list}
}

// Hello builds the client hello payload.
func (s *Service) Hello() protocol.HelloPayload {
	return protocol.HelloPayload{
		ClientName:      s.cfg.Client.Name,
		AgentVersion:    version.Version,
		ProtocolVersion: protocol.Version,
		OS:              runtime.GOOS + "/" + runtime.GOARCH,
		Capabilities:    s.Capabilities(),
	}
}

// Handle processes a raw request envelope and returns a raw response (if any).
func (s *Service) Handle(ctx context.Context, raw []byte) ([]byte, bool) {
	var env protocol.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, false
	}
	if env.Type != protocol.TypeRequest {
		return nil, false
	}
	if env.Version != protocol.Version {
		return s.errorResponse(env.ID, "不支持的协议版本"), true
	}
	switch env.Action {
	case protocol.ActionQuotaQuery:
		result, err := s.handleQuotaQuery(ctx, env.Payload)
		if err != nil {
			return s.errorResponse(env.ID, err.Error()), true
		}
		return s.okResponse(env.ID, result), true
	case protocol.ActionCodexRefresh:
		result, err := s.handleCodexRefresh(ctx, env.Payload)
		if err != nil {
			return s.errorResponse(env.ID, err.Error()), true
		}
		return s.okResponse(env.ID, result), true
	default:
		return s.errorResponse(env.ID, "未知的 action"), true
	}
}

func (s *Service) handleQuotaQuery(ctx context.Context, payload json.RawMessage) (protocol.QuotaQueryResult, error) {
	var req protocol.QuotaQueryPayload
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return protocol.QuotaQueryResult{}, fmt.Errorf("payload 无法解析")
		}
	}
	platform := ""
	if req.Platform != nil {
		platform = quota.NormalizePlatform(*req.Platform)
		if platform == "" {
			platform = strings.TrimSpace(*req.Platform)
		}
	}
	account := req.Account
	hasAccount := account != nil && strings.TrimSpace(*account) != ""

	var (
		accounts  []model.AccountQuota
		oldest    time.Time
		anyCached bool
	)
	track := func(list []model.AccountQuota, fetchedAt time.Time, cached bool) {
		if len(list) == 0 {
			return
		}
		accounts = append(accounts, list...)
		if cached {
			anyCached = true
		}
		if !fetchedAt.IsZero() && (oldest.IsZero() || fetchedAt.Before(oldest)) {
			oldest = fetchedAt
		}
	}

	switch {
	case platform == "" && hasAccount:
		result, err := s.cpa.CollectCPA(ctx, nil, account, req.Fresh)
		if err != nil {
			return protocol.QuotaQueryResult{}, err
		}
		track(result.Accounts, result.FetchedAt, result.Cached)
	case platform == "":
		result, err := s.cpa.CollectCPA(ctx, nil, nil, req.Fresh)
		if err != nil {
			return protocol.QuotaQueryResult{}, err
		}
		track(result.Accounts, result.FetchedAt, result.Cached)
		for _, channel := range channels.Supported() {
			list, fetchedAt, cached, lerr := s.local.Collect(ctx, channel, channels.Options{Fresh: req.Fresh})
			if lerr != nil {
				continue
			}
			track(list, fetchedAt, cached)
		}
	case quota.IsLocalChannel(platform):
		list, fetchedAt, cached, err := s.local.Collect(ctx, platform, channels.Options{Fresh: req.Fresh})
		if err != nil {
			return protocol.QuotaQueryResult{}, err
		}
		if hasAccount {
			list = filterAccounts(list, *account)
			if len(list) == 0 {
				return protocol.QuotaQueryResult{}, fmt.Errorf("没有找到 %s 账号：%s", platform, *account)
			}
		}
		track(list, fetchedAt, cached)
	default:
		result, err := s.cpa.CollectCPA(ctx, &platform, account, req.Fresh)
		if err != nil {
			return protocol.QuotaQueryResult{}, err
		}
		track(result.Accounts, result.FetchedAt, result.Cached)
	}

	dtos := make([]protocol.AccountQuotaDTO, 0, len(accounts))
	for _, item := range accounts {
		item.Instance = s.cfg.Client.Name
		dtos = append(dtos, protocol.AccountDTOFromModel(item))
	}
	if len(dtos) > protocol.MaxAccounts {
		dtos = dtos[:protocol.MaxAccounts]
	}

	now := time.Now()
	queriedAt := now
	if !oldest.IsZero() {
		queriedAt = oldest
	}
	cacheAge := now.Sub(queriedAt).Seconds()
	if cacheAge < 0 {
		cacheAge = 0
	}
	return protocol.QuotaQueryResult{
		ClientName: s.cfg.Client.Name,
		QueriedAt:  queriedAt.UTC().Format(time.RFC3339),
		Cached:     anyCached,
		CacheAge:   cacheAge,
		CacheTTL:   s.effectiveTTL(platform),
		Accounts:   dtos,
	}, nil
}

// effectiveTTL reports the cache TTL (seconds) used for the query scope.
func (s *Service) effectiveTTL(platform string) float64 {
	if platform == "" {
		return s.cfg.RefreshCache.Default
	}
	if quota.IsLocalChannel(platform) {
		return s.local.TTL(platform)
	}
	return s.cpa.TTL(platform)
}

func (s *Service) handleCodexRefresh(ctx context.Context, payload json.RawMessage) (protocol.CodexRefreshResult, error) {
	// Authoritative local gate: reject before any network side effect.
	if !s.cfg.Refresh.Enabled {
		return protocol.CodexRefreshResult{}, fmt.Errorf("客户端未启用 Codex 额度刷新功能（refresh.enabled=false）")
	}
	var req protocol.CodexRefreshPayload
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return protocol.CodexRefreshResult{}, fmt.Errorf("payload 无法解析")
		}
	}
	if strings.TrimSpace(req.Account) == "" {
		return protocol.CodexRefreshResult{}, fmt.Errorf("account 不能为空")
	}
	message, remaining, err := s.cpa.RefreshCodex(ctx, req.Account)
	if err != nil {
		return protocol.CodexRefreshResult{}, err
	}
	return protocol.CodexRefreshResult{Message: message, RemainingCredits: remaining}, nil
}

func (s *Service) okResponse(id string, result any) []byte {
	raw, _ := json.Marshal(result)
	ok := true
	env := protocol.Envelope{Version: protocol.Version, Type: protocol.TypeResponse, ID: id, OK: &ok, Result: raw}
	out, _ := json.Marshal(env)
	return out
}

func (s *Service) errorResponse(id, message string) []byte {
	ok := false
	env := protocol.Envelope{Version: protocol.Version, Type: protocol.TypeResponse, ID: id, OK: &ok, Error: message}
	out, _ := json.Marshal(env)
	return out
}

func filterAccounts(accounts []model.AccountQuota, query string) []model.AccountQuota {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return accounts
	}
	out := make([]model.AccountQuota, 0)
	for _, account := range accounts {
		name := strings.ToLower(account.Name)
		auth := strings.ToLower(account.AuthIndex)
		if strings.Contains(name, needle) || name == needle || strings.HasPrefix(name, needle) ||
			strings.Contains(auth, needle) {
			out = append(out, account)
		}
	}
	return out
}
