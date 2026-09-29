// Package protocol implements the QuotaNoa client protocol v2 wire types.
//
// All messages are JSON envelopes. The client must treat the server as
// untrusted: capabilities reported here originate exclusively from the local
// configuration.
package protocol

import (
	"encoding/json"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/model"
)

// Version is the wire protocol version.
const Version = 2

// Actions accepted from the server.
const (
	ActionQuotaQuery   = "quota.query"
	ActionCodexRefresh = "codex.refresh"
)

// Message types.
const (
	TypeRequest  = "request"
	TypeResponse = "response"
	TypeHello    = "hello"
	TypeError    = "error"
)

// Limits mirrored from the server.
const (
	MaxAccounts = 200
	MaxWindows  = 32
)

// Envelope is the common wire message.
type Envelope struct {
	Version int             `json:"version"`
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Action  string          `json:"action,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	OK      *bool           `json:"ok,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// Capabilities describes the local abilities advertised in the client hello.
type Capabilities struct {
	Refresh  bool     `json:"refresh"`
	Channels []string `json:"channels"`
}

// HelloPayload is the client -> server hello.
type HelloPayload struct {
	ClientName      string       `json:"client_name"`
	AgentVersion    string       `json:"agent_version"`
	ProtocolVersion int          `json:"protocol_version"`
	OS              string       `json:"os"`
	Capabilities    Capabilities `json:"capabilities"`
}

// ServerHelloPayload is the server -> client hello.
type ServerHelloPayload struct {
	ServerName      string `json:"server_name"`
	ServerVersion   string `json:"server_version"`
	ProtocolVersion int    `json:"protocol_version"`
	SessionID       string `json:"session_id"`
}

// QuotaQueryPayload is the quota.query request payload.
type QuotaQueryPayload struct {
	Platform *string `json:"platform"`
	Account  *string `json:"account"`
	Fresh    bool    `json:"fresh"`
}

// QuotaWindowDTO is a serialized quota window.
type QuotaWindowDTO struct {
	ID               string   `json:"id"`
	Label            string   `json:"label"`
	UsedPercent      *float64 `json:"used_percent,omitempty"`
	RemainingPercent *float64 `json:"remaining_percent,omitempty"`
	Remaining        *float64 `json:"remaining,omitempty"`
	Limit            *float64 `json:"limit,omitempty"`
	ResetLabel       string   `json:"reset_label"`
	ResetAt          *float64 `json:"reset_at,omitempty"`
	ResetNote        string   `json:"reset_note,omitempty"`
	Direction        string   `json:"direction,omitempty"`
}

// AccountQuotaDTO is a serialized account quota card. auth_index is never sent.
type AccountQuotaDTO struct {
	Platform                 string           `json:"platform"`
	Name                     string           `json:"name"`
	Plan                     string           `json:"plan"`
	Status                   string           `json:"status"`
	Error                    string           `json:"error"`
	Windows                  []QuotaWindowDTO `json:"windows"`
	Disabled                 bool             `json:"disabled"`
	Cooling                  bool             `json:"cooling"`
	ClientName               string           `json:"client_name"`
	SubscriptionExpiresAt    *float64         `json:"subscription_expires_at,omitempty"`
	SubscriptionExpiresLabel string           `json:"subscription_expires_label"`
	ResetCredits             *int             `json:"reset_credits,omitempty"`
	PlanBadges               [][2]string      `json:"plan_badges"`
	SubscriptionBadges       [][2]string      `json:"subscription_badges"`
}

// QuotaQueryResult is the quota.query response payload.
type QuotaQueryResult struct {
	ClientName string            `json:"client_name"`
	QueriedAt  string            `json:"queried_at"`
	Cached     bool              `json:"cached"`
	Accounts   []AccountQuotaDTO `json:"accounts"`
}

// CodexRefreshPayload is the codex.refresh request payload.
type CodexRefreshPayload struct {
	Account string `json:"account"`
}

// CodexRefreshResult is the codex.refresh response payload.
type CodexRefreshResult struct {
	Message          string `json:"message"`
	RemainingCredits *int   `json:"remaining_credits,omitempty"`
}

// AccountDTOFromModel converts an internal account to its wire DTO.
func AccountDTOFromModel(a model.AccountQuota) AccountQuotaDTO {
	windows := make([]QuotaWindowDTO, 0, len(a.Windows))
	for _, w := range a.Windows {
		windows = append(windows, QuotaWindowDTO{
			ID:               w.ID,
			Label:            w.Label,
			UsedPercent:      w.UsedPercent,
			RemainingPercent: w.RemainingPercent,
			Remaining:        w.Remaining,
			Limit:            w.Limit,
			ResetLabel:       w.ResetLabel,
			ResetAt:          w.ResetAt,
			ResetNote:        w.ResetNote,
			Direction:        w.Direction,
		})
		if len(windows) >= MaxWindows {
			break
		}
	}
	planBadges := a.PlanBadges
	if planBadges == nil {
		planBadges = [][2]string{}
	}
	subBadges := a.SubscriptionBadges
	if subBadges == nil {
		subBadges = [][2]string{}
	}
	return AccountQuotaDTO{
		Platform:                 a.Platform,
		Name:                     a.Name,
		Plan:                     a.Plan,
		Status:                   a.Status,
		Error:                    a.Error,
		Windows:                  windows,
		Disabled:                 a.Disabled,
		Cooling:                  a.Cooling,
		ClientName:               a.Instance,
		SubscriptionExpiresAt:    a.SubscriptionExpiresAt,
		SubscriptionExpiresLabel: a.SubscriptionExpiresLabel,
		ResetCredits:             a.ResetCredits,
		PlanBadges:               planBadges,
		SubscriptionBadges:       subBadges,
	}
}

// Marshaling is a small named type alias for callers that build JSON bodies.
type Marshaling = json.RawMessage
