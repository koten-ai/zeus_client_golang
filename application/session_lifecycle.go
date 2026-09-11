// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/koten-ai/zeus_client_golang/adapters/zeushttp"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
)

const sessionLifeComponent = "application.session_lifecycle"

// CommitResult is POST /v2/session/{id}/turn outcome (Python CommitResult).
type CommitResult struct {
	OK        bool
	Handle    domain.SessionHandle
	TurnReqID string
	Error     string
	Status    int
}

// SetupOptions is SessionLifecycle.setup kwargs.
type SetupOptions struct {
	ChatRequest       map[string]any
	UserMessage       string
	Prior             *domain.SessionHandle
	ContractID        string
	BoundContractHash string
	Mode              string
	ChatID            string
	EnableSessions    bool
	Headers           map[string]string
	TurnID            string
	ForceTrace        bool
	Rewind            bool
	BriefSha12        string
	MiniSha12         string
}

// CommitOptions is SessionLifecycle.commit kwargs.
type CommitOptions struct {
	ChatRequest   map[string]any
	ProducedDelta []any
	Mode          string
	Headers       map[string]string
	TurnID        string
	ForceTrace    bool
	Rewind        bool
	BriefSha12    string
	MiniSha12     string
}

// SessionLifecycle is create / rehydrate / dead-sid recovery / commit turn.
// Server mints session ids. Session-trace projector is application/projectors (ZCG-16).
type SessionLifecycle struct {
	Client *zeushttp.SessionClient
	Target config.DataTarget
	Log    func(level, msg string, attrs map[string]any)
	logs   []string
}

func (l *SessionLifecycle) emit(level, msg string, attrs map[string]any) {
	if l == nil {
		return
	}
	line := level + " " + msg
	l.logs = append(l.logs, line)
	if l.Log != nil {
		l.Log(level, msg, attrs)
	}
}

// Logs is the testable session log blob.
func (l *SessionLifecycle) Logs() string {
	if l == nil {
		return ""
	}
	return strings.Join(l.logs, "\n")
}

func sessionHopHeaders(chatID, turnID string, forceTrace bool, extra map[string]string, chatSessionID, brief, mini string) map[string]string {
	base := zeushttp.CorrelationHeaders(zeushttp.Correlation{
		ChatID:        chatID,
		TurnID:        turnID,
		ForceTrace:    forceTrace,
		TraceClass:    zeushttp.TraceClassSession,
		ChatSessionID: chatSessionID,
		BriefSha12:    brief,
		MiniSha12:     mini,
	})
	return zeushttp.MergeHeaders(base, extra)
}

func sessionHashFor(chatReq map[string]any, bound string) string {
	stamped := domain.ExtractStampedHash(chatReq)
	payload := ""
	if len(chatReq) > 0 {
		payload = domain.ComputeContractHash(chatReq)
	}
	return domain.ResolveSessionContractHash(bound, stamped, payload).Hash
}

// Setup creates or rehydrates a session. Dead sid → recreate same turn.
// Mode switch = new session. Does not invent session_id.
func (l *SessionLifecycle) Setup(ctx context.Context, opts SetupOptions) (domain.SessionHandle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		if de := domain.FromContext(err); de != nil {
			return domain.SessionHandle{}, de
		}
		return domain.SessionHandle{}, err
	}
	chatReq := opts.ChatRequest
	if chatReq == nil {
		chatReq = map[string]any{}
	}
	cid := strings.TrimSpace(opts.ContractID)
	sessionHash := sessionHashFor(chatReq, opts.BoundContractHash)
	mode := opts.Mode
	if mode == "" {
		mode = "analytics"
	}
	priorSID := ""
	priorMode := ""
	priorChat := ""
	priorRound := 0
	if opts.Prior != nil {
		priorSID = strings.TrimSpace(opts.Prior.SessionID)
		priorMode = strings.TrimSpace(opts.Prior.Mode)
		priorChat = opts.Prior.ChatID
		priorRound = opts.Prior.Round
	}
	hopHeaders := sessionHopHeaders(opts.ChatID, opts.TurnID, opts.ForceTrace, opts.Headers, priorSID, opts.BriefSha12, opts.MiniSha12)

	if !opts.EnableSessions {
		cst := "none"
		if cid != "" {
			cst = "match"
		}
		return domain.SessionHandle{
			SessionID:      "",
			Round:          1,
			ChatID:         opts.ChatID,
			ContractID:     cid,
			ContractHash:   sessionHash,
			ContractStatus: cst,
			Enabled:        false,
			Mode:           mode,
		}, nil
	}
	if l == nil || l.Client == nil {
		return domain.SessionHandle{}, domain.NewSession(domain.CodeSessionCreateFailed, sessionLifeComponent,
			domain.WithMessage("session client not wired"))
	}

	if priorSID != "" && priorMode != "" && priorMode != strings.TrimSpace(mode) {
		return l.create(ctx, chatReq, opts.UserMessage, cid, sessionHash, mode, firstNonEmpty(opts.ChatID, priorChat), hopHeaders, "", opts.Rewind)
	}

	if priorSID != "" {
		reh, err := l.Client.Rehydrate(ctx, priorSID, 6, mode, hopHeaders, l.Target)
		if err != nil {
			return domain.SessionHandle{}, err
		}
		if reh != nil {
			rehRound := jsonInt(reh["round"], priorRound)
			thisRound := 1
			if rehRound > 0 {
				thisRound = rehRound + 1
			}
			cst := domain.ContractStatusFromRehydrate(cid, sessionHash, reh)
			reqID := asStr(reh["_req_id"])
			if reqID == "" {
				reqID = l.Client.LastReqID()
			}
			l.emit("info", "zeus_client.session.rehydrated", map[string]any{
				"session.id":      priorSID,
				"req_id":          reqID,
				"zeus.round":      thisRound,
				"contract_status": cst,
				"result":          "ok",
			})
			return domain.SessionHandle{
				SessionID:      priorSID,
				Round:          thisRound,
				ChatID:         firstNonEmpty(opts.ChatID, priorChat),
				ContractID:     cid,
				ContractHash:   sessionHash,
				ContractStatus: cst,
				Created:        false,
				Rehydrated:     true,
				Enabled:        true,
				Mode:           mode,
			}, nil
		}
		return l.create(ctx, chatReq, opts.UserMessage, cid, sessionHash, mode, firstNonEmpty(opts.ChatID, priorChat), hopHeaders, priorSID, opts.Rewind)
	}

	return l.create(ctx, chatReq, opts.UserMessage, cid, sessionHash, mode, opts.ChatID, hopHeaders, "", opts.Rewind)
}

func (l *SessionLifecycle) create(ctx context.Context, chatReq map[string]any, userMessage, contractID, contractHash, mode, chatID string, headers map[string]string, recoveredFrom string, rewind bool) (domain.SessionHandle, error) {
	var conv []any
	if userMessage != "" {
		conv = []any{map[string]any{"role": "user", "content": userMessage}}
	} else {
		conv = []any{}
	}
	result, err := l.Client.Create(ctx, zeushttp.SessionCreateRequest{
		ContractID:   contractID,
		ContractHash: contractHash,
		ChatRequest:  chatReq,
		Conversation: conv,
		Headers:      headers,
		Mode:         mode,
		Target:       l.Target,
		Rewind:       rewind,
	})
	if err != nil {
		return domain.SessionHandle{}, err
	}
	if result.OK {
		body := result.Body
		if body == nil {
			body = map[string]any{}
		}
		sid := strings.TrimSpace(asStr(body["session_id"]))
		rnd := jsonInt(body["round"], 1)
		if rnd <= 0 {
			rnd = 1
		}
		cst := domain.NormalizeContractStatus(asStr(body["contract_status"]))
		scope := ""
		if l.Target.Bucket != "" && l.Target.Scope != "" {
			scope = l.Target.Bucket + "/" + l.Target.Scope
		}
		l.emit("info", "zeus_client.session.created", map[string]any{
			"session.id":      sid,
			"req_id":          result.ReqID,
			"scope":           scope,
			"contract_status": cst,
			"result":          "ok",
		})
		return domain.SessionHandle{
			SessionID:      sid,
			Round:          rnd,
			ChatID:         chatID,
			ContractID:     contractID,
			ContractHash:   contractHash,
			ContractStatus: cst,
			Created:        true,
			Rehydrated:     false,
			RecoveredFrom:  recoveredFrom,
			Enabled:        true,
			CreateReqID:    result.ReqID,
			Mode:           mode,
		}, nil
	}
	errMsg := result.Error
	if errMsg == "" {
		errMsg = fmt.Sprintf("HTTP %d", result.StatusCode)
	}
	l.emit("error", "zeus_client.session.failed", map[string]any{
		"req_id":           result.ReqID,
		"http.status_code": result.StatusCode,
		"error.message":    errMsg,
		"result":           "error",
	})
	h := domain.SessionHandle{
		SessionID:      "",
		Round:          1,
		ChatID:         chatID,
		ContractID:     contractID,
		ContractHash:   contractHash,
		ContractStatus: "none",
		Created:        false,
		RecoveredFrom:  recoveredFrom,
		Enabled:        true,
		CreateReqID:    result.ReqID,
		Error:          errMsg,
		Mode:           mode,
	}
	code := domain.CodeSessionCreateFailed
	if result.StatusCode == 409 {
		code = domain.CodeZeusContractRequired
	}
	details := map[string]any{"http.status_code": result.StatusCode}
	if result.ReqID != "" {
		details["req_id"] = result.ReqID
	}
	if code == domain.CodeZeusContractRequired {
		return h, domain.NewZeusTransport(code, sessionLifeComponent,
			domain.WithMessage(errMsg), domain.WithDetails(details))
	}
	return h, domain.NewSession(code, sessionLifeComponent,
		domain.WithMessage(errMsg), domain.WithDetails(details))
}

// Commit POSTs /v2/session/{id}/turn for this turn's delta.
// Just-created: round = create_round+1 and user messages stripped from new_turns.
func (l *SessionLifecycle) Commit(ctx context.Context, handle domain.SessionHandle, opts CommitOptions) (CommitResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		if de := domain.FromContext(err); de != nil {
			return CommitResult{Handle: handle}, de
		}
		return CommitResult{Handle: handle}, err
	}
	if !handle.Enabled || handle.SessionID == "" {
		return CommitResult{OK: true, Handle: handle}, nil
	}
	if l == nil || l.Client == nil {
		return CommitResult{Handle: handle}, domain.NewSession(domain.CodeSessionCommitFailed, sessionLifeComponent,
			domain.WithMessage("session client not wired"))
	}
	justCreated := handle.Created
	turnRound := handle.Round
	turnTurns := opts.ProducedDelta
	if justCreated {
		turnRound = handle.Round + 1
		filtered := make([]any, 0, len(opts.ProducedDelta))
		for _, m := range opts.ProducedDelta {
			mm, ok := m.(map[string]any)
			if !ok {
				filtered = append(filtered, m)
				continue
			}
			if asStr(mm["role"]) == "user" {
				continue
			}
			filtered = append(filtered, m)
		}
		turnTurns = filtered
	}
	if len(turnTurns) == 0 {
		created := false
		return CommitResult{OK: true, Handle: handle.WithUpdates(domain.SessionHandleUpdates{
			Round:   &turnRound,
			Created: &created,
		})}, nil
	}
	mode := opts.Mode
	if mode == "" {
		mode = "analytics"
	}
	result, err := l.Client.ContinueTurn(ctx, zeushttp.SessionContinueRequest{
		SessionID:   handle.SessionID,
		ClientRound: turnRound,
		ChatRequest: opts.ChatRequest,
		NewTurns:    turnTurns,
		Headers:     sessionHopHeaders(handle.ChatID, opts.TurnID, opts.ForceTrace, opts.Headers, handle.SessionID, opts.BriefSha12, opts.MiniSha12),
		Mode:        mode,
		Target:      l.Target,
		Rewind:      opts.Rewind,
	})
	if err != nil {
		return CommitResult{Handle: handle, TurnReqID: result.ReqID, Status: result.StatusCode}, err
	}
	if result.OK {
		created := false
		reh := false
		empty := ""
		l.emit("info", "zeus_client.session.committed", map[string]any{
			"session.id": handle.SessionID,
			"round":      turnRound,
			"req_id":     result.ReqID,
			"result":     "ok",
		})
		return CommitResult{
			OK:        true,
			Handle:    handle.WithUpdates(domain.SessionHandleUpdates{Round: &turnRound, Created: &created, Rehydrated: &reh, Error: &empty}),
			TurnReqID: result.ReqID,
			Status:    result.StatusCode,
		}, nil
	}
	errMsg := result.Error
	if errMsg == "" {
		errMsg = fmt.Sprintf("HTTP %d", result.StatusCode)
	}
	l.emit("error", "zeus_client.session.failed", map[string]any{
		"session.id":    handle.SessionID,
		"req_id":        result.ReqID,
		"error.message": errMsg,
		"result":        "error",
	})
	code := domain.CodeSessionContinueFailed
	if result.StatusCode == 409 {
		code = domain.CodeZeusContractRequired
	}
	details := map[string]any{"http.status_code": result.StatusCode}
	if result.ReqID != "" {
		details["req_id"] = result.ReqID
	}
	out := CommitResult{
		OK:        false,
		Handle:    handle.WithUpdates(domain.SessionHandleUpdates{Error: &errMsg}),
		TurnReqID: result.ReqID,
		Error:     errMsg,
		Status:    result.StatusCode,
	}
	if code == domain.CodeZeusContractRequired {
		return out, domain.NewZeusTransport(code, sessionLifeComponent,
			domain.WithMessage(errMsg), domain.WithDetails(details))
	}
	return out, domain.NewSession(code, sessionLifeComponent,
		domain.WithMessage(errMsg), domain.WithDetails(details))
}

func jsonInt(v any, def int) int {
	switch x := v.(type) {
	case int:
		if x == 0 {
			return def
		}
		return x
	case int64:
		if x == 0 {
			return def
		}
		return int(x)
	case float64:
		if x == 0 {
			return def
		}
		return int(x)
	case string:
		n := 0
		for _, ch := range x {
			if ch < '0' || ch > '9' {
				return def
			}
			n = n*10 + int(ch-'0')
		}
		if n == 0 {
			return def
		}
		return n
	default:
		return def
	}
}
