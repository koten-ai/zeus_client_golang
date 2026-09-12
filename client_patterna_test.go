// SPDX-License-Identifier: BUSL-1.1

//go:build patterna

package zeusclient

import (
	"context"
	"testing"
	"time"

	"github.com/koten-ai/zeus_client_golang/adapters/jobsma"
	"github.com/koten-ai/zeus_client_golang/api"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

func TestClientJobsPatternA(t *testing.T) {
	zeus := &recordingJobsZeus{}
	cfg := config.RuntimeConfig{
		Target:   config.DataTarget{Bucket: "west", Scope: "s", Collection: "c"},
		Zeus:     config.ZeusEndpointConfig{URL: "http://127.0.0.1:8080"},
		Settings: config.ClientSettings{Mode: "analytics"},
	}
	j := journal.NewInMemoryJournal(nil)
	units := api.NewUnitsAPIWith("host", api.UnitsOptions{
		Zeus:    zeus,
		Journal: j,
		Config:  cfg,
		Version: Version,
		Clock:   ports.SystemClock{},
	})
	rt := jobsma.New(api.UnitHost{Units: units})
	c, err := New(Options{Config: &cfg, Zeus: zeus, Journal: j, Jobs: rt, Env: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	handle, err := c.Jobs().Run(context.Background(), "two scopes", api.JobsRunParams{
		Units: []domain.UnitConfig{
			clientDirectUnit("u1", "east", "find"),
			clientDirectUnit("u2", "north", "find"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	var snap domain.JobSnapshot
	for time.Now().Before(deadline) {
		snap, err = c.Jobs().Get(context.Background(), handle.JobID)
		if err != nil {
			t.Fatal(err)
		}
		if snap.Status != "" && snap.Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snap.Status != "ok" || snap.Partial {
		t.Fatalf("snap %+v", snap)
	}
	zeus.mu.Lock()
	defer zeus.mu.Unlock()
	got := map[string]bool{}
	for _, call := range zeus.calls {
		got[call.Target.Bucket] = true
	}
	if !got["east"] || !got["north"] {
		t.Fatalf("hops %+v", zeus.calls)
	}
}
