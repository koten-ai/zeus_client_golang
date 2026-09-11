// SPDX-License-Identifier: BUSL-1.1

package secretsenv

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestEnvSecretStore(t *testing.T) {
	ctx := context.Background()
	store := NewWithEnviron(map[string]string{"XAI_API_KEY": "secret-value", "EMPTY": ""})
	got, ok := store.Get(ctx, "XAI_API_KEY")
	if !ok || got != "secret-value" {
		t.Fatalf("get %q ok=%v", got, ok)
	}
	if _, ok := store.Get(ctx, "EMPTY"); ok {
		t.Fatal("empty")
	}
	if _, ok := store.Get(ctx, "MISSING"); ok {
		t.Fatal("missing")
	}
	if _, ok := store.Get(ctx, ""); ok {
		t.Fatal("blank name")
	}
	if strings.Contains(store.String(), "secret-value") {
		t.Fatalf("repr leaked: %s", store)
	}
	if strings.Contains(fmt.Sprintf("%v", store), "secret-value") {
		t.Fatal("fmt leaked")
	}
}

func TestEnvSecretStoreConcurrent(t *testing.T) {
	ctx := context.Background()
	store := NewWithEnviron(map[string]string{"XAI_API_KEY": "secret-value"})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = store.Get(ctx, "XAI_API_KEY")
			_, _ = store.Get(ctx, "MISSING")
			_ = store.String()
		}()
	}
	wg.Wait()
}
