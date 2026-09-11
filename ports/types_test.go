// SPDX-License-Identifier: BUSL-1.1

package ports

import "testing"

func TestVerbRequestAllowPipelineDefaultFalse(t *testing.T) {
	var req VerbRequest
	if req.AllowPipeline {
		t.Fatal("AllowPipeline must default false")
	}
	if req.Rewind {
		t.Fatal("Rewind must default false")
	}
	if req.PreMintReqID {
		t.Fatal("PreMintReqID must default false")
	}
	if req.PasswordEnv != "" || req.TokenEnv != "" {
		t.Fatal("secrets must not live on VerbRequest")
	}
}

func TestAuthContextZeroMode(t *testing.T) {
	var a AuthContext
	if a.Mode != "" {
		t.Fatalf("zero Mode %q (empty means none)", a.Mode)
	}
}
