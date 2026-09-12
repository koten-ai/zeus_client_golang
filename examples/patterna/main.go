// SPDX-License-Identifier: BUSL-1.1

//go:build patterna

// Pattern A demo (ZCG-33): one job, ≥2 isolated units, Zeus Direct + AgentTurn.
// Product path is adapters/jobsma (koten_multi_agent_golang Engine.RunJob).
// FakeJobRuntime is not this demo. Everyday Q&A stays Mode 1 — this binary
// never auto-promotes chat into jobs.
//
// Default -mode=recorded is a real Client.Jobs().Run against in-process
// scripted Zeus/LLM (checked-in transcript). -mode=live hits lab Zeus + LLM.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	zeusclient "github.com/koten-ai/zeus_client_golang"
	"github.com/koten-ai/zeus_client_golang/adapters/jobsma"
	"github.com/koten-ai/zeus_client_golang/adapters/llmopenai"
	"github.com/koten-ai/zeus_client_golang/adapters/zeushttp"
	"github.com/koten-ai/zeus_client_golang/api"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const (
	demoGoal = "Fruit beer sales shortlist + inventory risk on those styles"
	demoPack = "desk"
)

func main() {
	mode := flag.String("mode", "recorded", "recorded (default, no live Zeus/LLM) or live")
	partial := flag.Bool("partial", false, "one unit error (public pipeline) + one ok → status=partial")
	configPath := flag.String("config", "", "optional runtime JSON for -mode=live (env names, no secrets)")
	profile := flag.String("profile", "development", "config profile for -mode=live")
	flag.Parse()

	ctx := context.Background()
	if err := run(ctx, *mode, *partial, *configPath, *profile); err != nil {
		fmt.Fprintf(os.Stderr, "patterna demo: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, mode string, partial bool, configPath, profile string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "recorded", "live":
	default:
		return fmt.Errorf("unknown -mode %q (recorded|live)", mode)
	}

	fmt.Printf("zeus_client_golang %s  claim=candidate  multi_agent=demo (not supported)\n", zeusclient.Version)
	fmt.Printf("mode=%s partial=%v\n", mode, partial)
	fmt.Printf("goal=%s\n", demoGoal)
	fmt.Printf("pack=%s\n", demoPack)
	fmt.Println("everyday Q&A stays Mode 1; this demo only calls Jobs().Run")

	c, err := newClient(mode, configPath, profile)
	if err != nil {
		return err
	}
	defer c.Close()

	if err := c.BindJobs(jobsma.New(api.UnitHost{Units: c.Units()})); err != nil {
		return err
	}

	bag := domain.DefaultJobBudgets()
	bag.MaxWorkers = 4
	bag.WallMS = 60_000
	bag.MaxWaves = 2
	fmt.Printf("budgets max_workers=%d wall_ms=%d max_waves=%d\n", bag.MaxWorkers, bag.WallMS, bag.MaxWaves)
	fmt.Println("llm.roles worker=fast-worker (AgentTurn); orch/advisor configured, PlannerStatic+AdvisorOff")
	fmt.Println("isolation: distinct scope triples; no share_session_id; bags not shared")

	units := demoUnits(c.Config().Zeus.URL, partial)
	for _, u := range units {
		fmt.Printf("unit %s kind=%s zeus_url=%s %s/%s/%s\n",
			u.UnitID, u.Kind, u.ZeusURL, u.Bucket, u.Scope, u.Collection)
	}

	handle, err := c.Jobs().Run(ctx, demoGoal, api.JobsRunParams{
		Pack:    demoPack,
		Budgets: &bag,
		Units:   units,
	})
	if err != nil {
		return err
	}
	fmt.Printf("job_id=%s handle_status=%s\n", handle.JobID, handle.Status)

	snap, err := waitJob(ctx, c, handle.JobID)
	if err != nil {
		return err
	}
	printSnapshot(snap)

	ch, err := c.Jobs().Watch(ctx, handle.JobID, 0)
	if err != nil {
		return err
	}
	var types []string
	for ev := range ch {
		types = append(types, fmt.Sprintf("%d:%s", ev.Seq, ev.Type))
	}
	fmt.Printf("events %s\n", strings.Join(types, " "))
	return nil
}

func newClient(mode, configPath, profile string) (*zeusclient.Client, error) {
	if mode == "live" {
		return newLiveClient(configPath, profile)
	}
	return newRecordedClient()
}

func newRecordedClient() (*zeusclient.Client, error) {
	cfg := config.Default()
	cfg.Settings.DurableSessions = false
	cfg.Settings.Mode = "analytics"
	cfg.Settings.AIProcessResult = false
	cfg.Settings.MaxRounds = 4
	cfg.LLM.Model = "fast-worker"
	cfg.LLM.APIKeyEnv = "LLM_WORKER_KEY"
	cfg.LLM.Roles = map[string]config.LlmRoleConfig{
		"orchestrator": {Model: "strong-planner", APIKeyEnv: "LLM_ORCH_KEY"},
		"advisor":      {Model: "strong-planner", APIKeyEnv: "LLM_ADV_KEY"},
		"worker":       {Model: "fast-worker", APIKeyEnv: "LLM_WORKER_KEY"},
	}
	return zeusclient.New(zeusclient.Options{
		Config: &cfg,
		Zeus:   &recordedZeus{},
		LLM:    &recordedLLM{},
		Env:    map[string]string{},
	})
}

func newLiveClient(configPath, profile string) (*zeusclient.Client, error) {
	cfg, err := config.Load(configPath, profile, nil)
	if err != nil {
		return nil, err
	}
	cfg.Settings.DurableSessions = false
	if len(cfg.LLM.Roles) == 0 {
		cfg.LLM.Roles = map[string]config.LlmRoleConfig{
			"orchestrator": {Model: cfg.LLM.Model, APIKeyEnv: cfg.LLM.APIKeyEnv},
			"advisor":      {Model: cfg.LLM.Model, APIKeyEnv: cfg.LLM.APIKeyEnv},
			"worker":       {Model: cfg.LLM.Model, APIKeyEnv: cfg.LLM.APIKeyEnv},
		}
	}
	zeus := zeushttp.NewPort(zeushttp.PortOptions{
		Endpoint: cfg.Zeus,
		Version:  zeusclient.Version,
	})
	llm := llmopenai.New(llmopenai.Options{Config: cfg.LLM})
	return zeusclient.New(zeusclient.Options{
		Config: &cfg,
		Zeus:   zeus,
		LLM:    llm,
	})
}

func demoUnits(zeusURL string, partial bool) []domain.UnitConfig {
	if zeusURL == "" {
		zeusURL = "http://127.0.0.1:8080"
	}
	directVerb := "find"
	if partial {
		directVerb = "pipeline"
	}
	sales := domain.UnitConfig{
		UnitID:      "unit_sales",
		Kind:        domain.UnitKindAgentTurn,
		Goal:        "Shortlist fruit beers for sales",
		ZeusURL:     zeusURL,
		Bucket:      "beer-sample",
		Scope:       "sales",
		Collection:  "_default",
		AuthMode:    string(config.AuthNone),
		CatalogMode: "analytics",
		BaseID:      "base-5-mock",
		ChatRequest: map[string]any{
			"_kit":    "MOCK_NOT_FOR_PRODUCTION",
			"base_id": "base-5-mock",
			"mode":    "analytics",
			"metadata": map[string]any{
				"note": "Demo inject only. Not a Hub stamp. Do not invent contract_hash.",
			},
			"instructions": map[string]any{
				"system_prompt": "You are a sales worker. Use find. Terminate with return.",
			},
			"verbs": []any{
				map[string]any{"name": "find", "description": "Find entities"},
				map[string]any{"name": "return", "description": "Terminate with Layer A"},
			},
			"messages": []any{map[string]any{
				"role": "system",
				"content": "## SCOPE BRIEF\n" +
					"bucket: beer-sample\nscope: sales\ncollection: _default\n" +
					"\n## MINI-SCHEMA\nentity_type: Beer\nfields: name, style\n",
			}},
		},
	}
	inventory := domain.UnitConfig{
		UnitID:     "unit_inventory",
		Kind:       domain.UnitKindZeusDirect,
		Goal:       "Inventory risk on fruit-beer styles in the east warehouse",
		ZeusURL:    zeusURL,
		Bucket:     "inventory",
		Scope:      "east",
		Collection: "_default",
		AuthMode:   string(config.AuthNone),
		Call: map[string]any{
			"verb": directVerb,
			"body": map[string]any{"entity_type": "Beer"},
		},
	}
	return []domain.UnitConfig{sales, inventory}
}

func waitJob(ctx context.Context, c *zeusclient.Client, jobID string) (domain.JobSnapshot, error) {
	deadline := time.Now().Add(30 * time.Second)
	var last domain.JobSnapshot
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return last, err
		}
		snap, err := c.Jobs().Get(ctx, jobID)
		if err != nil {
			return last, err
		}
		last = snap
		if snap.Status != "" && snap.Status != "running" {
			return snap, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return last, fmt.Errorf("timeout waiting for job %s (last status=%s)", jobID, last.Status)
}

func printSnapshot(snap domain.JobSnapshot) {
	fmt.Printf("status=%s partial=%v seq=%d\n", snap.Status, snap.Partial, snap.Seq)
	if snap.Answer != "" {
		fmt.Printf("answer=%s\n", snap.Answer)
	}
	for _, row := range snap.UnitSummaries {
		id, _ := row["unit_id"].(string)
		st, _ := row["status"].(string)
		code := row["error_code"]
		fmt.Printf("unit_summary unit_id=%s status=%s error_code=%v\n", id, st, code)
	}
}

var (
	_ ports.ZeusPort = (*recordedZeus)(nil)
	_ ports.LlmPort  = (*recordedLLM)(nil)
)
