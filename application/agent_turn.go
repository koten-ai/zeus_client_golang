// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/koten-ai/zeus_client_golang/adapters/zeushttp"
	"github.com/koten-ai/zeus_client_golang/application/projectors"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const agentTurnComponent = "application.agent_turn"

const (
	// InsightAfterZeusInstruction is appended as a user message for the insight hop.
	InsightAfterZeusInstruction = "Zeus tool results (including row/data payloads) are already in this " +
		"conversation. Analyze that evidence and write a clear, useful answer for " +
		"the operator: what was found, notable names/counts/patterns, and any " +
		"caveats. Do not call tools. Do not re-run the same successful pipeline or " +
		"invent rows/fields not present in the tool results. Prefer concrete " +
		"details from the data over a one-line abstract summary."

	// CheapFinalStaticAnswer is used when cheap-final has no tool-arg summary.
	CheapFinalStaticAnswer = "Zeus returned data. See structured results in the UI."

	maxToolCallsPerRound = 8
	maxToolJSONBytes     = 64 << 10 // G5.4: no 64KB re-prompt
	defaultMaxRounds     = 8
	defaultSystemPrompt  = "You are a helpful Zeus data assistant."
)

var (
	terminateNames   = map[string]struct{}{"return": {}, "return_result": {}}
	orientationVerbs = map[string]struct{}{"describe": {}, "explain": {}}
)

// TurnStatus is the agent-turn outcome (Python TurnStatus).
type TurnStatus string

const (
	TurnOK        TurnStatus = "ok"
	TurnError     TurnStatus = "error"
	TurnRefused   TurnStatus = "refused"
	TurnClarify   TurnStatus = "clarify"
	TurnMaxRounds TurnStatus = "max_rounds"
)

// TurnRequest is one agent.run input (Python TurnRequest).
type TurnRequest struct {
	Message        string
	Target         config.DataTarget
	Settings       config.ClientSettings
	Session        *domain.SessionHandle
	PriorMessages  []map[string]any
	SystemPrompt   string
	Tools          []map[string]any
	ChatRequest    map[string]any
	BaseID         string
	PackSchema     map[string]any
	ChatID         string
	Model          string
	EnableSessions bool
}

// StructuredResult is Layer A + policy bind for the result envelope.
type StructuredResult struct {
	LayerA       map[string]any
	Policy       string
	Flags        map[string]bool
	UI           map[string]any
	Artifacts    map[string]any
	PolicyReason string
}

// DebugBundle is the turn debug plane — never the user answer.
type DebugBundle struct {
	TurnID              string
	ChatID              string
	SessionID           string
	Notes               []string
	Rounds              int
	AIProcessResult     bool
	AIProcessResultExit string
	PublicTrace         map[string]any
	Hops                []map[string]any
	JournalEventCount   int
	HooksJailbreakScore float64
	PreferredReqID      string
	ReqIDs              []string
	ZeusURL             string
	ClientVersion       string
	Target              map[string]any
	Catalog             map[string]any
	ContractStatus      string
	Tokens              map[string]any
	Stamp               map[string]any
	TraceID             string
}

// TurnResult is agent.run output (Python TurnResult).
type TurnResult struct {
	Answer     string
	Status     TurnStatus
	Structured *StructuredResult
	Session    *domain.SessionHandle
	Debug      DebugBundle
	Err        *domain.Error
	Messages   []map[string]any
	LayerA     *domain.LayerA
	Policy     *domain.PolicyDecision
	ToolTrail  []map[string]any
}

// RunAgentTurnOpts is run_agent_turn kwargs.
type RunAgentTurnOpts struct {
	LLM              ports.LlmPort
	Zeus             ports.ZeusPort
	Journal          journal.ExecutionJournal
	Middleware       *MiddlewareChain
	DefaultSettings  config.ClientSettings
	DebugPolicy      config.DebugPolicy
	SessionLifecycle *SessionLifecycle
	ZeusURL          string
	ClientFloor      string
	ExtraNotes       []string
	IDs              ports.IDFactory
	ClientIP         string
	Version          string
	Log              func(level, msg string, attrs map[string]any)
	ContextWindow    int
	ContextSoftLimit float64
}

// ToolRoundOutcome is one round of Zeus tool execution (Python ToolRoundOutcome).
type ToolRoundOutcome struct {
	ReturnSeen       bool
	TerminalSummary  string
	ToolArgSummary   string
	ToolsExecuted    int
	ToolsWithData    int
	ToolsWithPayload int
	ToolsEmpty       int
	Hops             []map[string]any
	Steps            []map[string]any
	ReturnArgs       map[string]any
	TerminateVia     string
}

// RunAgentTurn is one sequential Mode 1 turn (AGENT_LOOP_SPEC P0–P9).
// LLM↔Zeus rounds are serial; Bag C is not shared across goroutines.
// ctx cancel → 000006. G2 never in answer.
func RunAgentTurn(ctx context.Context, req TurnRequest, opts RunAgentTurnOpts) TurnResult {
	if ctx == nil {
		ctx = context.Background()
	}
	settings := mergeTurnSettings(opts.DefaultSettings, req.Settings)
	prepared, _, _ := PrepareSettings(settings)
	settings = prepared

	chatReq := cloneAnyMap(req.ChatRequest)
	if len(chatReq) > 0 {
		inj := InjectSettingsFromClient(settings)
		chatReq = ApplyControlPlaneInject(chatReq, inj)
		chatReq = ApplyToolPathInject(chatReq, settings.IgnoreUserToolPathHints)
		req.ChatRequest = chatReq
	}

	ids := opts.IDs
	if ids == nil {
		ids = ports.UUIDFactory{}
	}
	turnID := ids.TurnID(ctx)
	chatID := strings.TrimSpace(req.ChatID)
	if chatID == "" {
		chatID = ids.ChatID(ctx)
	}
	traceID := domain.NewTraceID()
	t0 := time.Now()
	j := opts.Journal
	if j == nil {
		j = journal.NewInMemoryJournal(nil)
	}
	mw := opts.Middleware
	if mw == nil {
		mw = DefaultMiddlewareChain()
	}
	floor := opts.ClientFloor
	if floor == "" {
		floor = "client-floor-5"
	}
	scope := ""
	if req.Target.Bucket != "" {
		scope = req.Target.Bucket + "/" + req.Target.Scope
	}
	stamp := domain.ProductStamp(domain.StampOptions{
		IPAddress: domain.ResolveClientIP(opts.ClientIP, map[string]string{}, false),
		Version:   opts.Version,
		Scope:     scope,
	})
	notes := append([]string{}, opts.ExtraNotes...)
	hops := []map[string]any{}
	steps := []map[string]any{}
	mwCtx := newMiddlewareContext(turnID, req.Message)
	sessionHandle := req.Session

	je := func(etype string, data map[string]any) {
		j.Append(journal.JournalEvent{
			EventID:   etype + "_" + domain.NewZeusReqID()[:10],
			TsMs:      time.Now().UnixMilli(),
			Type:      etype,
			Component: agentTurnComponent,
			TurnID:    turnID,
			Data:      data,
		})
	}
	logf := opts.Log
	emit := func(level, msg string, attrs map[string]any) {
		if logf != nil {
			logf(level, msg, attrs)
		}
	}

	je(journal.EventTurnStarted, map[string]any{
		"message_preview": clipStr(req.Message, 200),
		"base_id":         req.BaseID,
		"client_floor":    floor,
		"trace_id":        traceID,
		"chat_id":         chatID,
	})
	emit("info", "zeus_client.turn.started", map[string]any{
		"chat.id":  chatID,
		"turn.id":  turnID,
		"scope":    scope,
		"mode":     settings.Mode,
		"zeus.url": opts.ZeusURL,
		"trace_id": traceID,
	})

	finish := func(in turnFinish) TurnResult {
		in.je = je
		in.emit = emit
		in.t0 = t0
		in.turnID = turnID
		in.chatID = chatID
		in.traceID = traceID
		in.settings = settings
		in.journal = j
		in.stamp = stamp
		in.version = opts.Version
		in.zeusURL = opts.ZeusURL
		in.floor = floor
		in.baseID = req.BaseID
		in.target = req.Target
		in.tools = req.Tools
		in.chatRequest = req.ChatRequest
		return finishTurn(in)
	}

	if err := ctxErr(ctx); err != nil {
		return finish(turnFinish{
			answer:  "",
			status:  TurnError,
			notes:   notes,
			session: sessionHandle,
			err:     ctxErrAsDomain(err),
		})
	}

	if strings.TrimSpace(req.Message) == "" {
		return finish(turnFinish{
			answer:  "",
			status:  TurnError,
			notes:   append(notes, "empty message"),
			session: sessionHandle,
			err: domain.New(domain.CodeAgentMessageEmpty, agentTurnComponent,
				domain.WithMessage("agent message empty")),
		})
	}

	if opts.LLM == nil {
		return finish(turnFinish{
			answer:  "",
			status:  TurnError,
			notes:   notes,
			session: sessionHandle,
			err: domain.New(domain.CodeNotImplemented, agentTurnComponent,
				domain.WithMessage("LLM port not wired on runtime")),
		})
	}

	aiProcess := settings.AIProcessResult
	maxRounds := settings.MaxRounds
	if aiProcess && maxRounds < 2 {
		maxRounds = 2
		notes = append(notes, "ai_process_result=true: max_rounds raised to 2 for insight turn")
	}
	if aiProcess {
		notes = append(notes, "ai_process_result=true")
	} else {
		notes = append(notes, "ai_process_result=false")
	}

	system := systemFromRequest(req)
	tools := toolsFromRequest(req)
	messages := []map[string]any{{"role": "system", "content": system}}
	for _, pm := range req.PriorMessages {
		if pm != nil {
			messages = append(messages, cloneAnyMap(pm))
		}
	}
	messages = append(messages, map[string]any{"role": "user", "content": req.Message})

	mwCtx.Data["catalog_tool_names"] = catalogToolNames(tools)
	mwCtx.Data["prior_user_texts"] = priorUserTexts(req.PriorMessages)
	mw.OnTurnStart(mwCtx)
	notes = append(notes, mwCtx.takeNotes()...)
	notes = appendJailbreakHitNote(notes, mwCtx)

	var (
		answer           string
		exitKind         string
		lastReturnArgs   map[string]any
		lastTerminateVia = "client_terminate"
		roundsDone       int
		trail            []map[string]any
	)
	skipLoop := mwCtx.boolData("hooks_must_refuse")
	if skipLoop {
		notes = append(notes, "jailbreak.pre_llm_refuse")
		exitKind = "hooks_refuse"
	}

	sysNow := systemPromptOf(messages, req.SystemPrompt, req.ChatRequest)
	briefSha12, miniSha12 := injectSliceSha12s(sysNow)
	chatSessionID := ""
	if sessionHandle != nil && sessionHandle.Enabled && sessionHandle.SessionID != "" {
		chatSessionID = sessionHandle.SessionID
	}

	if err := ctxErr(ctx); err != nil {
		return finish(turnFinish{
			answer: answer, status: TurnError, messages: messages, notes: notes,
			hops: hops, steps: steps, session: sessionHandle, err: ctxErrAsDomain(err),
		})
	}

	loopErr := error(nil)
	if !skipLoop {
		for rnd := 1; rnd <= maxRounds; rnd++ {
			if err := ctxErr(ctx); err != nil {
				loopErr = err
				break
			}
			roundsDone = rnd
			mwCtx.Round = rnd
			remaining := maxRounds - rnd
			if remaining <= settings.ForceReturnRoundsLeft && rnd > 1 {
				notes = append(notes, "force_return: remaining="+itoa(remaining)+" <= "+itoa(settings.ForceReturnRoundsLeft))
				messages = append(messages, map[string]any{
					"role": "user",
					"content": "SYSTEM: Round budget nearly exhausted. " +
						"Terminate now with the return tool (required four fields).",
				})
			}
			if len(trail) > 0 && settings.ToolTrailEnabled && settings.ToolTrailInject {
				snippet := domain.RenderTrailInject(trail, settings.ToolTrailMaxEntries)
				asAny := mapsToAny(messages)
				domain.UpsertTrailOnSystem(asAny, snippet)
				messages = anyToMaps(asAny)
			}
			mw.BeforeLLM(mwCtx, messages)
			notes = append(notes, mwCtx.takeNotes()...)

			temp := 0.0
			llmResp, llmErr := opts.LLM.Complete(ctx, ports.LlmRequest{
				Messages:    cloneMessageSlice(messages),
				Model:       req.Model,
				Tools:       cloneToolSlice(tools),
				Temperature: &temp,
				ConvID:      chatID,
			})
			if llmErr != nil {
				derr := llmErrAsDomain(llmErr)
				notes = append(notes, "llm_error: "+string(derr.Code))
				steps = append(steps, map[string]any{
					"round": rnd, "type": "llm_error", "code": string(derr.Code),
				})
				return finish(turnFinish{
					answer: "LLM error: " + derr.Message, status: TurnError,
					messages: messages, notes: notes, rounds: roundsDone,
					hops: hops, steps: steps, session: sessionHandle, err: derr,
				})
			}
			mw.AfterLLM(mwCtx, map[string]any{
				"content":    llmResp.Content,
				"tool_calls": llmResp.ToolCalls,
				"usage":      llmResp.Usage,
			})
			notes = append(notes, mwCtx.takeNotes()...)

			toolCalls := append([]map[string]any{}, llmResp.ToolCalls...)
			if len(toolCalls) == 0 {
				if env := domain.ParsePipelineEnvelope(llmResp.Content); env != nil &&
					opts.Zeus != nil && catalogToolNames(tools)["pipeline"] &&
					!mwCtx.boolData("hooks_must_refuse") {
					toolCalls = []map[string]any{syntheticPipelineCall(env)}
					notes = append(notes, "recovered pipeline envelope from model content as pipeline tool call")
				}
			}
			names := make([]any, 0, len(toolCalls))
			for _, tc := range toolCalls {
				names = append(names, toolCallName(tc))
			}
			usage := map[string]any{}
			if llmResp.Usage != nil {
				usage = llmResp.Usage
			}
			steps = append(steps, map[string]any{
				"round": rnd, "type": "llm", "tool_calls": names, "usage": usage,
			})

			if len(toolCalls) == 0 {
				answer = strings.TrimSpace(llmResp.Content)
				if answer == "" {
					answer = "(model returned no content)"
				}
				messages = append(messages, map[string]any{"role": "assistant", "content": answer})
				exitKind = "direct"
				break
			}

			asst := map[string]any{
				"role":       "assistant",
				"content":    nil,
				"tool_calls": cloneToolSlice(toolCalls),
			}
			if strings.TrimSpace(llmResp.Content) != "" {
				asst["content"] = llmResp.Content
			}
			messages = append(messages, asst)

			if opts.Zeus == nil {
				notes = append(notes, "zeus port missing; cannot execute tools")
				answer = "Zeus port not configured for tool calls"
				exitKind = "error"
				break
			}

			outcome, execErr := executeToolCalls(ctx, executeToolOpts{
				toolCalls:     toolCalls,
				messages:      &messages,
				zeus:          opts.Zeus,
				target:        req.Target,
				mode:          settings.Mode,
				middleware:    mw,
				mwCtx:         mwCtx,
				roundN:        rnd,
				chatID:        chatID,
				turnID:        turnID,
				forceTrace:    settings.ForceTrace,
				rewind:        opts.DebugPolicy.Rewind,
				chatSessionID: chatSessionID,
				briefSha12:    briefSha12,
				miniSha12:     miniSha12,
				notes:         &notes,
			})
			if execErr != nil {
				loopErr = execErr
				hops = append(hops, outcome.Hops...)
				steps = append(steps, outcome.Steps...)
				break
			}
			hops = append(hops, outcome.Hops...)
			for _, hop := range outcome.Hops {
				empty := asBoolMap(hop["ok"]) && isEmptyJSONValue(hop["snippet"])
				trail = append(trail, domain.TrailEntryFromHop(hop, len(trail)+1, empty))
			}
			steps = append(steps, outcome.Steps...)
			notes = append(notes, mwCtx.takeNotes()...)
			notes = appendJailbreakHitNote(notes, mwCtx)
			if outcome.ReturnArgs != nil {
				lastReturnArgs = outcome.ReturnArgs
			}
			if outcome.TerminateVia != "" {
				lastTerminateVia = outcome.TerminateVia
			}

			if outcome.ReturnSeen {
				terminal := strings.TrimSpace(outcome.TerminalSummary)
				if aiProcess {
					synth, hopNotes := insightHop(ctx, opts.LLM, &messages, req.Model, chatID, &steps, rnd)
					notes = append(notes, hopNotes...)
					answer = strings.TrimSpace(synth)
					if answer == "" {
						answer = terminal
					}
					exitKind = "insight"
					notes = append(notes, "ai_process_result=true after terminating Zeus tool; insight synthesis")
				} else {
					answer = terminal
					if answer != "" && !recentAssistantHas(messages, answer) {
						messages = append(messages, map[string]any{"role": "assistant", "content": answer})
					}
					exitKind = "cheap_terminal"
					notes = append(notes, "ai_process_result=false after terminate; cheap terminal envelope")
				}
				break
			}
			if !aiProcess && outcome.ToolsWithPayload > 0 {
				candidate := strings.TrimSpace(outcome.ToolArgSummary)
				InspectJailbreak(mwCtx, candidate, "tool_arg_summary")
				if mwCtx.boolData("hooks_must_refuse") {
					notes = append(notes, "jailbreak.cheap_path_leak")
					answer = ""
					exitKind = "hooks_refuse"
					break
				}
				answer = candidate
				if answer == "" {
					answer = CheapFinalStaticAnswer
				}
				if !recentAssistantHas(messages, answer) {
					messages = append(messages, map[string]any{"role": "assistant", "content": answer})
				}
				exitKind = "cheap_final"
				lastTerminateVia = "cheap_final"
				notes = append(notes, "ai_process_result=false after Zeus data; thin final without second LLM hop")
				break
			}
			if mwCtx.boolData("hooks_must_refuse") {
				notes = append(notes, "jailbreak.abort_turn")
				exitKind = "hooks_refuse"
				break
			}
		}
		if loopErr == nil && exitKind == "" && !skipLoop {
			answer = "Ran out of tool rounds before the model produced a final answer."
			exitKind = "max_rounds"
			notes = append(notes, "max_rounds exceeded")
		}
	}

	if loopErr != nil {
		return finish(turnFinish{
			answer: answer, status: TurnError, messages: messages, notes: notes,
			rounds: roundsDone, hops: hops, steps: steps, session: sessionHandle,
			err: ctxErrAsDomain(loopErr),
		})
	}

	var layer *domain.LayerA
	if lastReturnArgs != nil {
		ruleIDs := make([]string, 0, len(settings.Rules))
		for k := range settings.Rules {
			ruleIDs = append(ruleIDs, k)
		}
		parsed := domain.ParseLayerA(lastReturnArgs, domain.ParseLayerAOpts{
			RuleIDs:          ruleIDs,
			OutputRequest:    settings.OutputRequest,
			AppOutputOnError: settings.AppOutputOnError,
		})
		parsed = domain.ApplyPackSchema(parsed, req.PackSchema)
		layer = &parsed
		InspectJailbreak(mwCtx, layer.Summary, "summary")
	}
	rawAnswer := answer
	prePolicy := rawAnswer
	if peeled, ok := domain.PeelLayerASummary(rawAnswer); ok {
		prePolicy = peeled
	} else {
		layerSummary := ""
		if layer != nil {
			layerSummary = layer.Summary
		}
		prePolicy = domain.UserFacingAnswer(rawAnswer, "", layerSummary)
	}
	InspectJailbreak(mwCtx, prePolicy, "answer")

	hooksScore := mwCtx.floatData("hooks_jailbreak_score")
	hooksRefuse := mwCtx.boolData("hooks_must_refuse")
	if hooksRefuse && layer == nil {
		l := domain.LayerA{Summary: "", Errors: []string{"hooks_must_refuse"}}
		layer = &l
	}
	var decision *domain.PolicyDecision
	if layer != nil {
		d := domain.DecidePolicy(*layer, domain.DecidePolicyOpts{
			Settings: domain.PolicySettings{
				Messages:                settings.Messages,
				StickyFlags:             settings.StickyFlags,
				SoftRequirePolicyAction: settings.SoftRequirePolicyAction,
			},
			HooksJailbreakScore: hooksScore,
			HooksMustRefuse:     hooksRefuse,
			BrandInjectPresent:  settings.CompanyContext != "",
		})
		decision = &d
		emit("info", "zeus_client.policy.applied", map[string]any{
			"policy_action": d.Policy, "result": "ok",
		})
	}

	finalAnswer := prePolicy
	if decision != nil {
		if (decision.Forced && (decision.Policy == domain.PolicyRefuse || decision.Policy == domain.PolicyError)) ||
			strings.TrimSpace(finalAnswer) == "" {
			if decision.UIText != "" {
				finalAnswer = decision.UIText
			}
		} else if decision.Policy == domain.PolicyClarify && decision.UIText != "" {
			finalAnswer = decision.UIText
		}
	}
	if strings.Contains(finalAnswer, "wish_i_knew") && layer != nil && layer.Summary != "" {
		finalAnswer = layer.Summary
	}
	mw.OnTurnEnd(mwCtx, finalAnswer)
	notes = append(notes, mwCtx.takeNotes()...)

	status := TurnOK
	switch {
	case exitKind == "max_rounds":
		status = TurnMaxRounds
	case decision != nil && decision.Policy == domain.PolicyRefuse:
		status = TurnRefused
	case decision != nil && decision.Policy == domain.PolicyClarify:
		status = TurnClarify
	case decision != nil && decision.Policy == domain.PolicyError:
		status = TurnError
	}

	return finish(turnFinish{
		answer: finalAnswer, status: status, messages: messages, notes: notes,
		rounds: roundsDone, hops: hops, steps: steps, session: sessionHandle,
		layer: layer, decision: decision, aiExit: exitKind,
		layerVia: lastTerminateVia, toolTrail: trail, hooksScore: hooksScore,
	})
}

type turnFinish struct {
	answer      string
	status      TurnStatus
	messages    []map[string]any
	notes       []string
	rounds      int
	hops        []map[string]any
	steps       []map[string]any
	session     *domain.SessionHandle
	layer       *domain.LayerA
	decision    *domain.PolicyDecision
	err         *domain.Error
	aiExit      string
	layerVia    string
	toolTrail   []map[string]any
	hooksScore  float64
	je          func(string, map[string]any)
	emit        func(string, string, map[string]any)
	t0          time.Time
	turnID      string
	chatID      string
	traceID     string
	settings    config.ClientSettings
	journal     journal.ExecutionJournal
	stamp       map[string]any
	version     string
	zeusURL     string
	floor       string
	baseID      string
	target      config.DataTarget
	tools       []map[string]any
	chatRequest map[string]any
}

func finishTurn(in turnFinish) TurnResult {
	answer := in.answer
	var structured *StructuredResult
	var layerMap map[string]any
	if in.layer != nil {
		via := in.layerVia
		if via == "" {
			via = "client_terminate"
		}
		layerMap = domain.CompactLayerA(*in.layer, via)
		if in.decision != nil {
			structured = &StructuredResult{
				LayerA:       layerMap,
				Policy:       in.decision.Policy,
				Flags:        in.decision.Flags,
				UI:           in.decision.UI(*in.layer),
				Artifacts:    in.decision.Artifacts(*in.layer),
				PolicyReason: in.decision.Reason,
			}
		} else {
			structured = &StructuredResult{LayerA: layerMap}
		}
	}
	reqIDs := collectReqIDs(in.hops)
	pref := ""
	if len(in.hops) > 0 {
		anyHops := make([]any, len(in.hops))
		for i, h := range in.hops {
			anyHops[i] = h
		}
		pref = projectors.SelectPrimaryReqID(anyHops)
	}
	sid := ""
	cst := ""
	chat := in.chatID
	if in.session != nil {
		sid = in.session.SessionID
		cst = in.session.ContractStatus
		if in.session.ChatID != "" {
			chat = in.session.ChatID
		}
	}
	stampMap := cloneAnyMap(in.stamp)
	if sid != "" {
		if _, ok := stampMap["session_id"]; !ok {
			stampMap["session_id"] = sid
		}
	}
	if strings.Contains(answer, "wish_i_knew") {
		if in.layer != nil && in.layer.Summary != "" {
			answer = in.layer.Summary
		} else if i := strings.Index(answer, "wish_i_knew"); i >= 0 {
			answer = strings.TrimSpace(answer[:i])
		}
	}
	sys := ""
	for _, m := range in.messages {
		if asString(m["role"]) == "system" {
			if c, ok := m["content"].(string); ok {
				sys = c
				break
			}
		}
	}
	flags := catalogFlagsOf(sys)
	catalogBlock := map[string]any{
		"has_scope_brief": flags["has_scope_brief"],
		"has_mini_schema": flags["has_mini_schema"],
		"tools_count":     len(in.tools),
		"source":          "none",
		"brief_sha12":     flags["brief_sha12"],
		"mini_sha12":      flags["mini_sha12"],
		"base_id":         nilIfEmpty(in.baseID),
		"client_floor":    in.floor,
	}
	if len(in.tools) > 0 {
		catalogBlock["source"] = "tools"
	} else if verbs, ok := in.chatRequest["verbs"]; ok && verbs != nil {
		catalogBlock["source"] = "verbs"
	}
	targetMap := map[string]any{}
	if in.target.Bucket != "" || in.target.Scope != "" {
		targetMap = map[string]any{
			"bucket":     in.target.Bucket,
			"scope":      in.target.Scope,
			"collection": in.target.Collection,
			"mode":       in.settings.Mode,
		}
	}
	var sessionBlock map[string]any
	if sid != "" || len(reqIDs) > 0 {
		sessionBlock = map[string]any{
			"id":               nilIfEmpty(sid),
			"req_ids":          reqIDs,
			"preferred_req_id": nilIfEmpty(pref),
			"contract_status":  nilIfEmpty(cst),
		}
	}
	policyName := ""
	flagsMap := map[string]bool{}
	if in.decision != nil {
		policyName = in.decision.Policy
		flagsMap = in.decision.Flags
	}
	tokens := SumProviderTokens(in.steps, nil)
	public := projectors.BuildPublicTrace(projectors.PublicTrace{
		TurnID:              in.turnID,
		Answer:              answer,
		Status:              string(in.status),
		Rounds:              in.rounds,
		Notes:               in.notes,
		Hops:                in.hops,
		AIProcessResult:     in.settings.AIProcessResult,
		AIProcessResultExit: in.aiExit,
		LayerA:              layerMap,
		Policy:              policyName,
		Flags:               flagsMap,
		Steps:               in.steps,
		Tokens:              tokens,
		Session:             sessionBlock,
		Stamp:               stampMap,
	})
	totalMS := int(time.Since(in.t0).Milliseconds())
	hooks := in.hooksScore
	if in.decision != nil {
		hooks = in.decision.HooksJailbreakScore
	}
	evCount := 0
	if in.journal != nil {
		evCount = len(in.journal.Events())
	}
	if in.je != nil {
		in.je(journal.EventTurnCompleted, map[string]any{
			"status":                 string(in.status),
			"rounds":                 in.rounds,
			"ai_process_result_exit": in.aiExit,
			"total_ms":               totalMS,
			"answer_preview":         clipStr(answer, 240),
			"req_ids":                reqIDs,
			"trace_id":               in.traceID,
		})
	}
	if in.err != nil && in.err.Details != nil {
		if sid != "" {
			in.err.Details["session.id"] = sid
		}
		if pref != "" {
			in.err.Details["req_id"] = pref
		}
		if len(reqIDs) > 0 {
			in.err.Details["req_ids"] = reqIDs
		}
		if in.zeusURL != "" {
			in.err.Details["zeus.url"] = in.zeusURL
		}
		if in.target.Bucket != "" {
			in.err.Details["scope"] = in.target.Bucket + "/" + in.target.Scope
		}
	} else if in.err != nil {
		in.err.Details = map[string]any{}
		if in.zeusURL != "" {
			in.err.Details["zeus.url"] = in.zeusURL
		}
	}
	finishAttrs := map[string]any{
		"session.id":  sid,
		"chat.id":     chat,
		"turn.id":     in.turnID,
		"rounds":      in.rounds,
		"duration_ms": totalMS,
		"zeus.url":    in.zeusURL,
		"scope":       "",
		"trace_id":    in.traceID,
		"mode":        in.settings.Mode,
	}
	if in.target.Bucket != "" {
		finishAttrs["scope"] = in.target.Bucket + "/" + in.target.Scope
	}
	if len(reqIDs) > 0 {
		finishAttrs["req_ids"] = reqIDs
		rid := pref
		if rid == "" {
			rid = reqIDs[len(reqIDs)-1]
		}
		finishAttrs["req_id"] = rid
	}
	hangTurnTelemetry(finishAttrs, tokens, in.hops)
	if in.emit != nil {
		if in.err == nil {
			tag := "ok"
			if in.status == TurnMaxRounds {
				tag = "forced_return"
			}
			finishAttrs["result"] = tag
			in.emit("info", "zeus_client.turn.finished", finishAttrs)
		} else {
			finishAttrs["result"] = "error"
			finishAttrs["error.type"] = string(in.err.Code)
			finishAttrs["error.message"] = in.err.Message
			in.emit("error", "zeus_client.turn.failed", finishAttrs)
		}
	}
	notes := in.notes
	if notes == nil {
		notes = []string{}
	}
	return TurnResult{
		Answer:     answer,
		Status:     in.status,
		Structured: structured,
		Session:    in.session,
		Debug: DebugBundle{
			TurnID:              in.turnID,
			ChatID:              chat,
			SessionID:           sid,
			Notes:               notes,
			Rounds:              in.rounds,
			AIProcessResult:     in.settings.AIProcessResult,
			AIProcessResultExit: in.aiExit,
			PublicTrace:         public,
			Hops:                in.hops,
			JournalEventCount:   evCount,
			HooksJailbreakScore: hooks,
			PreferredReqID:      pref,
			ReqIDs:              reqIDs,
			ZeusURL:             in.zeusURL,
			ClientVersion:       in.version,
			Target:              targetMap,
			Catalog:             catalogBlock,
			ContractStatus:      cst,
			Tokens:              tokens,
			Stamp:               stampMap,
			TraceID:             in.traceID,
		},
		Err:       in.err,
		Messages:  in.messages,
		LayerA:    in.layer,
		Policy:    in.decision,
		ToolTrail: in.toolTrail,
	}
}

type executeToolOpts struct {
	toolCalls     []map[string]any
	messages      *[]map[string]any
	zeus          ports.ZeusPort
	target        config.DataTarget
	mode          string
	middleware    *MiddlewareChain
	mwCtx         *MiddlewareContext
	roundN        int
	chatID        string
	turnID        string
	forceTrace    bool
	rewind        bool
	chatSessionID string
	briefSha12    string
	miniSha12     string
	notes         *[]string
}

func executeToolCalls(ctx context.Context, o executeToolOpts) (ToolRoundOutcome, error) {
	out := ToolRoundOutcome{}
	finalSummary := ""
	returnSeen := false
	n := len(o.toolCalls)
	if n > maxToolCallsPerRound {
		n = maxToolCallsPerRound
	}
	for i := 0; i < n; i++ {
		if err := ctxErr(ctx); err != nil {
			return out, err
		}
		tc := o.toolCalls[i]
		if tc == nil {
			continue
		}
		fn, _ := tc["function"].(map[string]any)
		name := strings.TrimSpace(asString(fn["name"]))
		tcArgs := parseToolArgs(fn["arguments"])
		tcArgs = zeushttp.VerbBodyWithoutRewind(tcArgs)
		callID := strings.TrimSpace(asString(tc["id"]))
		if callID == "" {
			callID = domain.NewZeusReqID()
		}
		if s, ok := tcArgs["summary"].(string); ok && strings.TrimSpace(s) != "" {
			out.ToolArgSummary = strings.TrimSpace(s)
		}
		if _, term := terminateNames[name]; term {
			finalSummary = asString(tcArgs["summary"])
			returnSeen = true
			out.ReturnArgs = cloneAnyMap(tcArgs)
			out.TerminateVia = "return"
			*o.messages = append(*o.messages, map[string]any{
				"role":         "tool",
				"tool_call_id": callID,
				"name":         name,
				"content":      mustJSON(tcArgs),
			})
			out.Steps = append(out.Steps, map[string]any{"round": o.roundN, "type": name, "args": tcArgs})
			continue
		}
		tcArgs = o.middleware.BeforeZeus(o.mwCtx, name, tcArgs)
		if s, ok := tcArgs["summary"].(string); ok && strings.TrimSpace(s) != "" {
			out.ToolArgSummary = strings.TrimSpace(s)
		}
		denied, _ := o.mwCtx.Data["denied_verbs"].([]string)
		if containsStr(denied, name) {
			*o.messages = append(*o.messages, map[string]any{
				"role": "tool", "tool_call_id": callID, "name": name,
				"content": mustJSON(map[string]any{"error": "verb_denied", "verb": name}),
			})
			out.Steps = append(out.Steps, map[string]any{"round": o.roundN, "type": "denied", "name": name, "args": tcArgs})
			continue
		}
		if o.mwCtx.boolData("hooks_must_refuse") {
			*o.messages = append(*o.messages, map[string]any{
				"role": "tool", "tool_call_id": callID, "name": name,
				"content": mustJSON(map[string]any{"error": "turn_refused", "verb": name}),
			})
			out.Steps = append(out.Steps, map[string]any{"round": o.roundN, "type": "refused", "name": name, "args": tcArgs})
			continue
		}
		t0 := time.Now()
		hop, err := o.zeus.CallVerb(ctx, ports.VerbRequest{
			Verb:       name,
			Body:       tcArgs,
			Target:     o.target,
			ModeHeader: orAnalytics(o.mode),
			Headers: zeushttp.CorrelationHeaders(zeushttp.Correlation{
				ChatID:        o.chatID,
				TurnID:        o.turnID,
				CallID:        callID,
				Mode:          orAnalytics(o.mode),
				ForceTrace:    o.forceTrace,
				TraceClass:    zeushttp.TraceClassAgent,
				ChatSessionID: o.chatSessionID,
				BriefSha12:    o.briefSha12,
				MiniSha12:     o.miniSha12,
			}),
			AllowPipeline: true,
			Rewind:        o.rewind,
		})
		if err != nil {
			if de := ctxErrAsDomain(err); de != nil && (de.Code == domain.CodeCancelled || de.Code == domain.CodeContextDeadline) {
				return out, err
			}
			hop = ports.VerbHopResult{OK: false, Error: err.Error(), Body: map[string]any{}}
		}
		ms := int(time.Since(t0).Milliseconds())
		body := hop.Body
		if body == nil {
			body = map[string]any{}
		}
		text := mustJSON(body)
		if len(body) == 0 {
			text = hop.Error
		}
		o.middleware.AfterZeus(o.mwCtx, name, hop.StatusCode, body)
		if ov, ok := o.mwCtx.Data["tool_body_override"].(string); ok && strings.TrimSpace(ov) != "" {
			delete(o.mwCtx.Data, "tool_body_override")
			text = ov
			var parsed any
			if json.Unmarshal([]byte(ov), &parsed) == nil {
				if m, ok := parsed.(map[string]any); ok {
					body = m
				} else {
					body = map[string]any{"error": "untrusted_tool_payload"}
				}
			} else {
				body = map[string]any{"error": "untrusted_tool_payload"}
			}
		}
		if truncated, did := truncateToolJSON(text); did {
			text = truncated
			if o.notes != nil {
				*o.notes = append(*o.notes, "tool_json_truncated")
			}
		}
		*o.messages = append(*o.messages, map[string]any{
			"role":         "tool",
			"tool_call_id": callID,
			"name":         name,
			"content":      text,
		})
		meta := projectors.ExtractPipelineMeta(body)
		hopRec := map[string]any{
			"req_id": hop.ReqID,
			"name":   name,
			"path_class": func() string {
				if name == "pipeline" {
					return "pipeline"
				}
				return name
			}(),
			"status":  hop.StatusCode,
			"ok":      hop.OK,
			"ms":      ms,
			"url":     hop.URL,
			"error":   hop.Error,
			"snippet": clipStr(text, projectors.TraceSnippetMax),
			"scope":   "",
		}
		if hop.HasBytes {
			hopRec["bytes.in"] = hop.BytesIn
			hopRec["bytes.out"] = hop.BytesOut
		}
		if o.target.Bucket != "" {
			hopRec["scope"] = o.target.Bucket + "/" + o.target.Scope
		}
		for k, v := range meta {
			hopRec[k] = v
		}
		if len(body) > 0 {
			hopRec["result_json"] = cloneAnyMap(body)
		}
		out.Hops = append(out.Hops, hopRec)
		out.Steps = append(out.Steps, map[string]any{
			"round": o.roundN, "type": "tool", "name": name, "args": tcArgs,
			"status": hop.StatusCode, "ms": ms, "req_id": hop.ReqID,
		})
		out.ToolsExecuted++
		empty := (!hop.OK) || isEmptyJSONValue(body) || strings.TrimSpace(text) == ""
		if empty {
			out.ToolsEmpty++
		} else {
			out.ToolsWithData++
			if _, orient := orientationVerbs[name]; !orient {
				out.ToolsWithPayload++
			}
		}
		if name == "pipeline" && hasTurnComplete(body) {
			returnSeen = true
			if finalSummary == "" {
				finalSummary = summaryFromTerminal(body, tcArgs)
			}
			cand := cloneAnyMap(tcArgs)
			for _, k := range []string{"summary", "query_decomposition", "decomposition", "confidence", "policy_action"} {
				if _, ok := cand[k]; !ok {
					if v, ok := body[k]; ok {
						cand[k] = v
					}
				}
			}
			if s, ok := cand["summary"].(string); ok && s != "" {
				if _, ok := cand["query_decomposition"].(map[string]any); ok {
					if _, ok := cand["decomposition"].(map[string]any); ok {
						if _, ok := cand["confidence"].(string); ok {
							out.ReturnArgs = cand
							out.TerminateVia = "pipeline"
						}
					}
				}
			}
		}
	}
	out.ReturnSeen = returnSeen
	if returnSeen {
		out.TerminalSummary = finalSummary
	}
	return out, nil
}

func insightHop(ctx context.Context, llm ports.LlmPort, messages *[]map[string]any, model, chatID string, steps *[]map[string]any, roundN int) (string, []string) {
	var notes []string
	*messages = append(*messages, map[string]any{"role": "user", "content": InsightAfterZeusInstruction})
	notes = append(notes, "force_final: ai_process_result_insight")
	temp := 0.0
	resp, err := llm.Complete(ctx, ports.LlmRequest{
		Messages:    cloneMessageSlice(*messages),
		Model:       model,
		Tools:       nil,
		Temperature: &temp,
		ConvID:      chatID,
	})
	if err != nil {
		notes = append(notes, "insight_failed: "+string(llmErrAsDomain(err).Code))
		return "", notes
	}
	content := strings.TrimSpace(resp.Content)
	if content != "" {
		*messages = append(*messages, map[string]any{"role": "assistant", "content": content})
		usage := map[string]any{}
		if resp.Usage != nil {
			usage = resp.Usage
		}
		*steps = append(*steps, map[string]any{
			"round": roundN, "type": "force_final", "cause": "ai_process_result_insight",
			"content_len": len(content), "usage": usage,
		})
	} else {
		notes = append(notes, "force_final_empty: ai_process_result_insight")
	}
	return content, notes
}

func mergeTurnSettings(def, req config.ClientSettings) config.ClientSettings {
	out := def
	out.AIProcessResult = req.AIProcessResult
	out.ForceTrace = req.ForceTrace
	if req.MaxRounds != 0 {
		out.MaxRounds = req.MaxRounds
	}
	if req.Mode != "" {
		out.Mode = req.Mode
	}
	if req.ForceReturnRoundsLeft != 0 {
		out.ForceReturnRoundsLeft = req.ForceReturnRoundsLeft
	}
	out.IgnoreUserToolPathHints = req.IgnoreUserToolPathHints || def.IgnoreUserToolPathHints
	out.ToolTrailEnabled = req.ToolTrailEnabled || def.ToolTrailEnabled
	out.ToolTrailInject = req.ToolTrailInject || def.ToolTrailInject
	if req.ToolTrailMaxEntries != 0 {
		out.ToolTrailMaxEntries = req.ToolTrailMaxEntries
	}
	if req.CompanyContext != "" {
		out.CompanyContext = req.CompanyContext
	}
	if len(req.Rules) > 0 {
		out.Rules = req.Rules
	}
	if len(req.Messages) > 0 {
		out.Messages = req.Messages
	}
	if len(req.StickyFlags) > 0 {
		out.StickyFlags = req.StickyFlags
	}
	out.SoftRequirePolicyAction = req.SoftRequirePolicyAction || def.SoftRequirePolicyAction
	if req.AppOutputOnError != "" {
		out.AppOutputOnError = req.AppOutputOnError
	}
	if req.OutputRequest != nil {
		out.OutputRequest = req.OutputRequest
	}
	if out.MaxRounds <= 0 {
		out.MaxRounds = defaultMaxRounds
	}
	if out.Mode == "" {
		out.Mode = "analytics"
	}
	if out.ForceReturnRoundsLeft == 0 {
		out.ForceReturnRoundsLeft = 1
	}
	if out.ToolTrailMaxEntries <= 0 {
		out.ToolTrailMaxEntries = 16
	}
	if out.AppOutputOnError == "" {
		out.AppOutputOnError = "strip"
	}
	return out
}

func systemFromRequest(req TurnRequest) string {
	if cr := req.ChatRequest; cr != nil {
		if msgs, ok := cr["messages"].([]any); ok {
			for _, m := range msgs {
				mm, ok := m.(map[string]any)
				if !ok {
					continue
				}
				if asString(mm["role"]) == "system" {
					if c, ok := mm["content"].(string); ok && strings.TrimSpace(c) != "" {
						return c
					}
				}
			}
		}
	}
	if strings.TrimSpace(req.SystemPrompt) != "" {
		return req.SystemPrompt
	}
	return defaultSystemPrompt
}

func toolsFromRequest(req TurnRequest) []map[string]any {
	if len(req.Tools) > 0 {
		out := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			out = append(out, cloneAnyMap(t))
		}
		return out
	}
	cr := req.ChatRequest
	if cr == nil {
		return nil
	}
	if tools, ok := cr["tools"].([]any); ok && len(tools) > 0 {
		return mapsFromAny(tools)
	}
	if verbs, ok := cr["verbs"].([]any); ok && len(verbs) > 0 {
		return mapsFromAny(verbs)
	}
	return nil
}

// hangTurnTelemetry adds REQ-9 attrs to zeus_client.turn.finished|failed.
// Omit tokens when the LLM never billed; omit bytes when no hop measured them.
// Never log tokens.input=0 to mean "not an LLM hop".
func hangTurnTelemetry(attrs map[string]any, tokens map[string]any, hops []map[string]any) {
	if attrs == nil {
		return
	}
	if ok, _ := tokens["ok"].(bool); ok {
		attrs["tokens.input"] = tokenAsInt(tokens["prompt"])
		attrs["tokens.output"] = tokenAsInt(tokens["completion"])
	}
	inB, outB, haveB := 0, 0, false
	for _, h := range hops {
		if h == nil {
			continue
		}
		if v, ok := h["bytes.in"]; ok {
			haveB = true
			inB += tokenAsInt(v)
		}
		if v, ok := h["bytes.out"]; ok {
			haveB = true
			outB += tokenAsInt(v)
		}
	}
	if haveB {
		attrs["bytes.in"] = inB
		attrs["bytes.out"] = outB
	}
}

func priorUserTexts(msgs []map[string]any) []string {
	out := make([]string, 0, len(msgs))
	for _, pm := range msgs {
		if pm == nil {
			continue
		}
		if asString(pm["role"]) != "user" {
			continue
		}
		out = append(out, asString(pm["content"]))
	}
	return out
}

func appendJailbreakHitNote(notes []string, ctx *MiddlewareContext) []string {
	if ctx == nil || ctx.Data == nil {
		return notes
	}
	hits := asStringSlice(ctx.Data["jailbreak_hits"])
	if len(hits) == 0 {
		return notes
	}
	note := "jailbreak.hits=" + strings.Join(hits, ",")
	for _, n := range notes {
		if n == note {
			return notes
		}
	}
	return append(notes, note)
}

func catalogToolNames(tools []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, t := range tools {
		fn, _ := t["function"].(map[string]any)
		if fn == nil {
			fn = t
		}
		if n := asString(fn["name"]); n != "" {
			out[n] = true
		}
	}
	return out
}

func toolCallName(tc map[string]any) any {
	fn, _ := tc["function"].(map[string]any)
	if fn == nil {
		return nil
	}
	if n := asString(fn["name"]); n != "" {
		return n
	}
	return nil
}

func parseToolArgs(raw any) map[string]any {
	switch x := raw.(type) {
	case map[string]any:
		return cloneAnyMap(x)
	case string:
		var obj any
		if json.Unmarshal([]byte(x), &obj) != nil {
			return map[string]any{}
		}
		m, ok := obj.(map[string]any)
		if !ok {
			return map[string]any{}
		}
		return m
	default:
		return map[string]any{}
	}
}

func syntheticPipelineCall(args map[string]any) map[string]any {
	return map[string]any{
		"id":   domain.NewZeusReqID(),
		"type": "function",
		"function": map[string]any{
			"name":      "pipeline",
			"arguments": mustJSON(args),
		},
	}
}

func truncateToolJSON(text string) (string, bool) {
	if len(text) <= maxToolJSONBytes {
		return text, false
	}
	cut := text[:maxToolJSONBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "\n…[truncated]", true
}

func isEmptyJSONValue(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		if len(x) == 0 {
			return true
		}
		for _, key := range []string{"rows", "items", "results", "entities"} {
			if _, ok := x[key]; ok {
				return isEmptyJSONValue(x[key])
			}
		}
		if data, ok := x["data"]; ok {
			return isEmptyJSONValue(data)
		}
		if result, ok := x["result"]; ok {
			return isEmptyJSONValue(result)
		}
		return false
	default:
		return false
	}
}

func hasTurnComplete(body map[string]any) bool {
	b, _ := body["turn_complete"].(bool)
	return b
}

func summaryFromTerminal(body, tcArgs map[string]any) string {
	for _, key := range []string{"summary", "answer", "message"} {
		if s, ok := body[key].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	if s, ok := tcArgs["summary"].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func recentAssistantHas(messages []map[string]any, content string) bool {
	start := len(messages) - 3
	if start < 0 {
		start = 0
	}
	for _, m := range messages[start:] {
		if asString(m["role"]) == "assistant" && asString(m["content"]) == content {
			return true
		}
	}
	return false
}

func systemPromptOf(messages []map[string]any, systemPrompt string, catalog map[string]any) string {
	if strings.TrimSpace(systemPrompt) != "" {
		return systemPrompt
	}
	if catalog != nil {
		if sm, ok := catalog["system_message"].(string); ok && strings.TrimSpace(sm) != "" {
			return sm
		}
	}
	for _, m := range messages {
		if asString(m["role"]) == "system" {
			if c, ok := m["content"].(string); ok && strings.TrimSpace(c) != "" {
				return c
			}
		}
	}
	return ""
}

func injectSliceSha12s(system string) (brief, mini string) {
	b := domain.SliceBlock(system, "brief")
	m := domain.SliceBlock(system, "mini")
	if b != "" {
		brief = domain.SHA12(b)
	}
	if m != "" {
		mini = domain.SHA12(m)
	}
	return brief, mini
}

func catalogFlagsOf(system string) map[string]any {
	hasScope := strings.Contains(system, "## SCOPE BRIEF")
	hasMini := strings.Contains(system, "## MINI-SCHEMA")
	var briefSha, miniSha any
	if hasScope {
		if s := domain.SliceBlock(system, "brief"); s != "" {
			briefSha = domain.SHA12(s)
		}
	}
	if hasMini {
		if s := domain.SliceBlock(system, "mini"); s != "" {
			miniSha = domain.SHA12(s)
		}
	}
	return map[string]any{
		"has_scope_brief": hasScope,
		"has_mini_schema": hasMini,
		"brief_sha12":     briefSha,
		"mini_sha12":      miniSha,
	}
}

func collectReqIDs(hops []map[string]any) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, h := range hops {
		rid := asString(h["req_id"])
		if rid == "" {
			continue
		}
		if _, ok := seen[rid]; ok {
			continue
		}
		seen[rid] = struct{}{}
		out = append(out, rid)
	}
	return out
}

func llmErrAsDomain(err error) *domain.Error {
	if err == nil {
		return nil
	}
	if de := ctxErrAsDomain(err); de != nil {
		return de
	}
	if de, ok := domain.AsError(err); ok {
		return de
	}
	return domain.NewLLM(domain.CodeAgentLLMRequestFailed, agentTurnComponent,
		domain.WithMessage(err.Error()), domain.WithCause(err))
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		if de := domain.FromContext(err); de != nil {
			return de
		}
		return err
	}
	return nil
}

func ctxErrAsDomain(err error) *domain.Error {
	if err == nil {
		return nil
	}
	if de, ok := domain.AsError(err); ok {
		return de
	}
	if de := domain.FromContext(err); de != nil {
		return de
	}
	if errors.Is(err, context.Canceled) {
		return domain.New(domain.CodeCancelled, agentTurnComponent, domain.WithCause(err))
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.New(domain.CodeContextDeadline, agentTurnComponent, domain.WithCause(err))
	}
	return nil
}

func mustJSON(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func clipStr(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func orAnalytics(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return "analytics"
	}
	return mode
}

func asBoolMap(v any) bool {
	b, _ := v.(bool)
	return b
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func cloneMessageSlice(in []map[string]any) []map[string]any {
	out := make([]map[string]any, len(in))
	for i, m := range in {
		out[i] = cloneAnyMap(m)
	}
	return out
}

func cloneToolSlice(in []map[string]any) []map[string]any {
	if in == nil {
		return nil
	}
	return cloneMessageSlice(in)
}

func mapsFromAny(in []any) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, v := range in {
		if m, ok := v.(map[string]any); ok {
			out = append(out, cloneAnyMap(m))
		}
	}
	return out
}

func mapsToAny(in []map[string]any) []any {
	out := make([]any, len(in))
	for i, m := range in {
		out[i] = m
	}
	return out
}

func anyToMaps(in []any) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, v := range in {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
