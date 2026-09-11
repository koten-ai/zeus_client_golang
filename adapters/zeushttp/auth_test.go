// SPDX-License-Identifier: BUSL-1.1

package zeushttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/koten-ai/zeus_client_golang/adapters/secretsenv"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
	"github.com/koten-ai/zeus_client_golang/security"
)

const (
	secretPassword = "s3cret-password"
	secretToken    = "tok-super-secret"
	fullSID        = "sid-abc123456789"
	mintRID        = "a1000000-0000-4000-8000-000000000001"
)

func beerTarget() config.DataTarget {
	return config.DataTarget{Bucket: "beer-sample", Scope: "_default"}
}

func ctx() context.Context { return context.Background() }

func basicEndpoint(url string) config.ZeusEndpointConfig {
	return config.ZeusEndpointConfig{
		URL:         url,
		AuthMode:    config.AuthBasic,
		Username:    "admin",
		PasswordEnv: "ZEUS_PASSWORD",
		TimeoutS:    5,
		TLSVerify:   true,
	}
}

func basicSecrets() *secretsenv.Store {
	return secretsenv.NewWithEnviron(map[string]string{"ZEUS_PASSWORD": secretPassword})
}

func mustCode(t *testing.T, err error, code domain.Code) *domain.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s", code)
	}
	de, ok := domain.AsError(err)
	if !ok {
		t.Fatalf("got %T %v", err, err)
	}
	if de.Code != code {
		t.Fatalf("code %s want %s (%v)", de.Code, code, de)
	}
	return de
}

func assertNoSecrets(t *testing.T, blob string) {
	t.Helper()
	for _, s := range []string{secretPassword, secretToken, fullSID} {
		if s != "" && strings.Contains(blob, s) {
			t.Fatalf("secret %q leaked in logs:\n%s", s, blob)
		}
	}
}

type mintOpts struct {
	sid    string
	status int
	body   []byte
	posts  *atomic.Int32
	check  func(*http.Request)
	reqID  string
}

func mintServer(t *testing.T, opts mintOpts) *httptest.Server {
	t.Helper()
	if opts.sid == "" {
		opts.sid = fullSID
	}
	if opts.status == 0 {
		opts.status = 200
	}
	if opts.reqID == "" {
		opts.reqID = mintRID
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if opts.posts != nil {
			opts.posts.Add(1)
		}
		if r.URL.Path != "/v1/beer-sample/_default/auth/session" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		if opts.check != nil {
			opts.check(r)
		}
		w.Header().Set(domain.ReqIDHeader, opts.reqID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(opts.status)
		if opts.body != nil {
			_, _ = w.Write(opts.body)
			return
		}
		if opts.status == 200 {
			_, _ = w.Write([]byte(`{"session_id":"` + opts.sid + `","expires_in":1800,"hard_ttl_s":43200}`))
		}
	}))
}

func TestNoneEmptyHeaders(t *testing.T) {
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: config.ZeusEndpointConfig{AuthMode: config.AuthNone},
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
	})
	defer r.Close(ctx())
	auth, err := r.ResolveAuth(ctx(), beerTarget(), false)
	if err != nil {
		t.Fatal(err)
	}
	if auth.Mode != "none" {
		t.Fatalf("mode %q", auth.Mode)
	}
	if auth.Headers == nil || len(auth.Headers) != 0 {
		t.Fatalf("headers %v", auth.Headers)
	}
}

func TestEmptyAuthModeIsNone(t *testing.T) {
	r := NewAuthResolver(AuthResolverOptions{
		Secrets: secretsenv.NewWithEnviron(map[string]string{}),
	})
	defer r.Close(ctx())
	auth, err := r.ResolveAuth(ctx(), config.DataTarget{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if auth.Mode != "none" || len(auth.Headers) != 0 {
		t.Fatalf("%+v", auth)
	}
}

func TestCertificateNotImplemented(t *testing.T) {
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: config.ZeusEndpointConfig{
			AuthMode:   config.AuthCertificate,
			CertFile:   "/tmp/c.pem",
			KeyFileEnv: "ZEUS_TLS_KEY",
		},
		Secrets: secretsenv.NewWithEnviron(map[string]string{}),
	})
	defer r.Close(ctx())
	_, err := r.ResolveAuth(ctx(), config.DataTarget{}, false)
	de := mustCode(t, err, domain.CodeNotImplemented)
	if de.Details["cert_file"] != "/tmp/c.pem" {
		t.Fatalf("details %v", de.Details)
	}
	if de.Details["key_file_env"] != "ZEUS_TLS_KEY" {
		t.Fatalf("details %v", de.Details)
	}
}

func TestInvalidAuthMode(t *testing.T) {
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: config.ZeusEndpointConfig{AuthMode: "digest"},
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
	})
	defer r.Close(ctx())
	_, err := r.ResolveAuth(ctx(), beerTarget(), false)
	mustCode(t, err, domain.CodeZeusAuthModeInvalid)
}

func TestBearerDoesNotLogToken(t *testing.T) {
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: config.ZeusEndpointConfig{AuthMode: config.AuthBearer, TokenEnv: "ZEUS_BEARER_TOKEN"},
		Secrets:  secretsenv.NewWithEnviron(map[string]string{"ZEUS_BEARER_TOKEN": secretToken}),
	})
	defer r.Close(ctx())
	auth, err := r.ResolveAuth(ctx(), config.DataTarget{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if auth.Mode != "bearer" {
		t.Fatalf("mode %q", auth.Mode)
	}
	if auth.Headers[headerAuthorization] != "Bearer "+secretToken {
		t.Fatalf("headers %v", auth.Headers)
	}
	blob := r.logBlob()
	assertNoSecrets(t, blob)
	if !strings.Contains(blob, "token_env=ZEUS_BEARER_TOKEN") {
		t.Fatalf("expected env name in logs:\n%s", blob)
	}
}

func TestBearerMissingToken(t *testing.T) {
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: config.ZeusEndpointConfig{AuthMode: config.AuthBearer},
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
	})
	defer r.Close(ctx())
	_, err := r.ResolveAuth(ctx(), config.DataTarget{}, false)
	de := mustCode(t, err, domain.CodeAuthFailed)
	if de.Details["token_env"] != defaultBearerEnv {
		t.Fatalf("details %v", de.Details)
	}
}

func TestSessionReadsEnvNoPOST(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		t.Errorf("session mode must not POST %s", r.URL.Path)
	}))
	defer srv.Close()
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: config.ZeusEndpointConfig{
			URL:      srv.URL,
			AuthMode: config.AuthSession,
			TokenEnv: "ZEUS_SESSION_ID",
		},
		Secrets: secretsenv.NewWithEnviron(map[string]string{"ZEUS_SESSION_ID": fullSID}),
	})
	defer r.Close(ctx())
	auth, err := r.ResolveAuth(ctx(), beerTarget(), false)
	if err != nil {
		t.Fatal(err)
	}
	if auth.Mode != "session" {
		t.Fatalf("mode %q", auth.Mode)
	}
	if auth.Headers[headerZeusSession] != fullSID {
		t.Fatalf("headers %v", auth.Headers)
	}
	if posts.Load() != 0 {
		t.Fatalf("posts %d", posts.Load())
	}
	assertNoSecrets(t, r.logBlob())
}

func TestSessionMissingID(t *testing.T) {
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: config.ZeusEndpointConfig{AuthMode: config.AuthSession},
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
	})
	defer r.Close(ctx())
	_, err := r.ResolveAuth(ctx(), beerTarget(), false)
	de := mustCode(t, err, domain.CodeAuthFailed)
	if de.Details["token_env"] != defaultSessionEnv {
		t.Fatalf("details %v", de.Details)
	}
}

func TestBasicMintsPerScopeSession(t *testing.T) {
	var posts atomic.Int32
	srv := mintServer(t, mintOpts{
		posts: &posts,
		check: func(r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok || user != "admin" || pass != secretPassword {
				t.Errorf("basic auth user=%q pass=%q ok=%v", user, pass, ok)
			}
			if r.Header.Get(domain.ReqIDHeader) != "" {
				t.Errorf("mint must omit %s by default", domain.ReqIDHeader)
			}
		},
	})
	defer srv.Close()
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: basicEndpoint(srv.URL),
		Secrets:  basicSecrets(),
	})
	defer r.Close(ctx())

	auth, err := r.ResolveAuth(ctx(), beerTarget(), false)
	if err != nil {
		t.Fatal(err)
	}
	if auth.Mode != "basic" {
		t.Fatalf("mode %q", auth.Mode)
	}
	if auth.Headers[headerZeusSession] != fullSID {
		t.Fatalf("headers %v", auth.Headers)
	}
	if posts.Load() != 1 {
		t.Fatalf("posts %d", posts.Load())
	}

	auth2, err := r.ResolveAuth(ctx(), beerTarget(), false)
	if err != nil {
		t.Fatal(err)
	}
	if auth2.Headers[headerZeusSession] != fullSID {
		t.Fatalf("cache %v", auth2.Headers)
	}
	if posts.Load() != 1 {
		t.Fatalf("cache hit posted again: %d", posts.Load())
	}
	blob := r.logBlob()
	assertNoSecrets(t, blob)
	if !strings.Contains(blob, "sid_prefix="+sidPrefix(fullSID)) {
		t.Fatalf("expected sid prefix in logs:\n%s", blob)
	}
}

func TestBasicForceRemints(t *testing.T) {
	var posts atomic.Int32
	srv := mintServer(t, mintOpts{posts: &posts})
	defer srv.Close()
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: basicEndpoint(srv.URL),
		Secrets:  basicSecrets(),
	})
	defer r.Close(ctx())
	if _, err := r.ResolveAuth(ctx(), beerTarget(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ResolveAuth(ctx(), beerTarget(), true); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 2 {
		t.Fatalf("posts %d want 2", posts.Load())
	}
}

func TestBasicIncompleteNoBucketScope(t *testing.T) {
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: basicEndpoint("http://zeus.example"),
		Secrets:  basicSecrets(),
	})
	defer r.Close(ctx())
	_, err := r.ResolveAuth(ctx(), config.DataTarget{Bucket: "beer-sample"}, false)
	mustCode(t, err, domain.CodeZeusAuthIncomplete)
	_, err = r.ResolveAuth(ctx(), config.DataTarget{Scope: "_default"}, false)
	mustCode(t, err, domain.CodeZeusAuthIncomplete)
}

func TestBasicIncompleteMissingPassword(t *testing.T) {
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: basicEndpoint("http://zeus.example"),
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
	})
	defer r.Close(ctx())
	_, err := r.ResolveAuth(ctx(), beerTarget(), false)
	de := mustCode(t, err, domain.CodeZeusAuthIncomplete)
	if de.Details["username_set"] != true {
		t.Fatalf("details %v", de.Details)
	}
	if de.Details["password_env"] != "ZEUS_PASSWORD" {
		t.Fatalf("details %v", de.Details)
	}
}

func TestBasicIncompleteMissingUsername(t *testing.T) {
	ep := basicEndpoint("http://zeus.example")
	ep.Username = ""
	r := NewAuthResolver(AuthResolverOptions{Endpoint: ep, Secrets: basicSecrets()})
	defer r.Close(ctx())
	_, err := r.ResolveAuth(ctx(), beerTarget(), false)
	de := mustCode(t, err, domain.CodeZeusAuthIncomplete)
	if de.Details["username_set"] != false {
		t.Fatalf("details %v", de.Details)
	}
}

func TestBasicScopeCredentialsOverlay(t *testing.T) {
	var posts atomic.Int32
	srv := mintServer(t, mintOpts{
		posts: &posts,
		check: func(r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok || user != "scoped" || pass != "scoped-pw" {
				t.Errorf("overlay user=%q pass=%q ok=%v", user, pass, ok)
			}
		},
	})
	defer srv.Close()
	ep := basicEndpoint(srv.URL)
	ep.ScopeCredentials = map[string]map[string]string{
		"beer-sample/_default": {
			"username":     "scoped",
			"password_env": "SCOPED_PASSWORD",
		},
	}
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: ep,
		Secrets: secretsenv.NewWithEnviron(map[string]string{
			"ZEUS_PASSWORD":   "global-pw",
			"SCOPED_PASSWORD": "scoped-pw",
		}),
	})
	defer r.Close(ctx())
	auth, err := r.ResolveAuth(ctx(), beerTarget(), false)
	if err != nil {
		t.Fatal(err)
	}
	if auth.Headers[headerZeusSession] != fullSID {
		t.Fatalf("%v", auth.Headers)
	}
	if posts.Load() != 1 {
		t.Fatalf("posts %d", posts.Load())
	}
	if strings.Contains(r.logBlob(), "scoped-pw") {
		t.Fatal("overlay password logged")
	}
}

func TestBasicHTTPNot200CapturesReqID(t *testing.T) {
	srv := mintServer(t, mintOpts{status: 401, body: []byte(`unauthorized`)})
	defer srv.Close()
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: basicEndpoint(srv.URL),
		Secrets:  basicSecrets(),
	})
	defer r.Close(ctx())
	_, err := r.ResolveAuth(ctx(), beerTarget(), false)
	de := mustCode(t, err, domain.CodeAuthFailed)
	if de.Details["status_code"] != 401 {
		t.Fatalf("details %v", de.Details)
	}
	if de.Details["req_id"] != mintRID {
		t.Fatalf("req_id %v", de.Details["req_id"])
	}
	assertNoSecrets(t, r.logBlob())
}

func TestBasicInvalidJSON(t *testing.T) {
	srv := mintServer(t, mintOpts{body: []byte(`not-json`)})
	defer srv.Close()
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: basicEndpoint(srv.URL),
		Secrets:  basicSecrets(),
	})
	defer r.Close(ctx())
	_, err := r.ResolveAuth(ctx(), beerTarget(), false)
	mustCode(t, err, domain.CodeAuthFailed)
}

func TestBasicMissingSessionID(t *testing.T) {
	srv := mintServer(t, mintOpts{body: []byte(`{"expires_in":1800}`)})
	defer srv.Close()
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: basicEndpoint(srv.URL),
		Secrets:  basicSecrets(),
	})
	defer r.Close(ctx())
	_, err := r.ResolveAuth(ctx(), beerTarget(), false)
	de := mustCode(t, err, domain.CodeAuthFailed)
	if !strings.Contains(de.Message, "session_id") {
		t.Fatalf("message %q", de.Message)
	}
}

func TestBasicUnreachable(t *testing.T) {
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: config.ZeusEndpointConfig{
			URL:         "http://127.0.0.1:1",
			AuthMode:    config.AuthBasic,
			Username:    "admin",
			PasswordEnv: "ZEUS_PASSWORD",
			TimeoutS:    0.25,
			TLSVerify:   true,
		},
		Secrets: basicSecrets(),
	})
	defer r.Close(ctx())
	_, err := r.ResolveAuth(ctx(), beerTarget(), false)
	de := mustCode(t, err, domain.CodeAuthSessionUnavailable)
	if de.Details["bucket"] != "beer-sample" || de.Details["scope"] != "_default" {
		t.Fatalf("details %v", de.Details)
	}
}

func TestBasicCacheIsInstanceScoped(t *testing.T) {
	var posts atomic.Int32
	srv := mintServer(t, mintOpts{posts: &posts})
	defer srv.Close()
	a := NewAuthResolver(AuthResolverOptions{Endpoint: basicEndpoint(srv.URL), Secrets: basicSecrets()})
	b := NewAuthResolver(AuthResolverOptions{Endpoint: basicEndpoint(srv.URL), Secrets: basicSecrets()})
	defer a.Close(ctx())
	defer b.Close(ctx())
	if _, err := a.ResolveAuth(ctx(), beerTarget(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ResolveAuth(ctx(), beerTarget(), false); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 2 {
		t.Fatalf("instance cache leaked: posts=%d", posts.Load())
	}
}

func TestBasicPasswordChangeBustsCache(t *testing.T) {
	var posts atomic.Int32
	srv := mintServer(t, mintOpts{posts: &posts})
	defer srv.Close()
	env := map[string]string{"ZEUS_PASSWORD": secretPassword}
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: basicEndpoint(srv.URL),
		Secrets:  secretsenv.NewWithEnviron(env),
	})
	defer r.Close(ctx())
	if _, err := r.ResolveAuth(ctx(), beerTarget(), false); err != nil {
		t.Fatal(err)
	}
	env["ZEUS_PASSWORD"] = "rotated-password"
	if _, err := r.ResolveAuth(ctx(), beerTarget(), false); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 2 {
		t.Fatalf("posts %d want 2 after password rotation", posts.Load())
	}
}

func TestBasicIdleCacheExpiry(t *testing.T) {
	var posts atomic.Int32
	srv := mintServer(t, mintOpts{posts: &posts})
	defer srv.Close()
	now := time.Now()
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: basicEndpoint(srv.URL),
		Secrets:  basicSecrets(),
		Now:      func() time.Time { return now },
	})
	defer r.Close(ctx())
	if _, err := r.ResolveAuth(ctx(), beerTarget(), false); err != nil {
		t.Fatal(err)
	}
	now = now.Add(1771 * time.Second) // idle_ttl 1800 − 30s margin
	if _, err := r.ResolveAuth(ctx(), beerTarget(), false); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 2 {
		t.Fatalf("posts %d want 2 after idle expiry", posts.Load())
	}
}

func TestBasic401RemintOnceThenFail(t *testing.T) {
	var posts atomic.Int32
	srv := mintServer(t, mintOpts{posts: &posts})
	defer srv.Close()
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: basicEndpoint(srv.URL),
		Secrets:  basicSecrets(),
	})
	defer r.Close(ctx())

	var hops atomic.Int32
	auth, status, err := r.DoWithBasic401Remint(ctx(), beerTarget(), func(_ context.Context, a ports.AuthContext) (int, error) {
		n := hops.Add(1)
		if n > 2 {
			t.Errorf("hop looped (%d)", n)
		}
		if a.Headers[headerZeusSession] != fullSID {
			t.Errorf("session %v", a.Headers)
		}
		return 401, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != 401 {
		t.Fatalf("status %d", status)
	}
	if auth.Mode != "basic" {
		t.Fatalf("mode %q", auth.Mode)
	}
	if hops.Load() != 2 {
		t.Fatalf("hops %d want 2", hops.Load())
	}
	if posts.Load() != 2 {
		t.Fatalf("mints %d want 2 (initial + force remint)", posts.Load())
	}
}

func TestBasic401RemintNotOnSuccess(t *testing.T) {
	var posts atomic.Int32
	srv := mintServer(t, mintOpts{posts: &posts})
	defer srv.Close()
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: basicEndpoint(srv.URL),
		Secrets:  basicSecrets(),
	})
	defer r.Close(ctx())
	var hops atomic.Int32
	_, status, err := r.DoWithBasic401Remint(ctx(), beerTarget(), func(_ context.Context, _ ports.AuthContext) (int, error) {
		hops.Add(1)
		return 200, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != 200 || hops.Load() != 1 || posts.Load() != 1 {
		t.Fatalf("status=%d hops=%d posts=%d", status, hops.Load(), posts.Load())
	}
}

func TestBearer401DoesNotRemint(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		t.Errorf("bearer must not POST %s", r.URL.Path)
	}))
	defer srv.Close()
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: config.ZeusEndpointConfig{
			URL:      srv.URL,
			AuthMode: config.AuthBearer,
			TokenEnv: "ZEUS_BEARER_TOKEN",
		},
		Secrets: secretsenv.NewWithEnviron(map[string]string{"ZEUS_BEARER_TOKEN": secretToken}),
	})
	defer r.Close(ctx())
	var hops atomic.Int32
	_, status, err := r.DoWithBasic401Remint(ctx(), beerTarget(), func(_ context.Context, a ports.AuthContext) (int, error) {
		hops.Add(1)
		if a.Mode != "bearer" {
			t.Errorf("mode %q", a.Mode)
		}
		return 401, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != 401 || hops.Load() != 1 || posts.Load() != 0 {
		t.Fatalf("status=%d hops=%d posts=%d", status, hops.Load(), posts.Load())
	}
}

func TestCallVerbNotImplemented(t *testing.T) {
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: config.ZeusEndpointConfig{AuthMode: config.AuthNone},
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
	})
	defer r.Close(ctx())
	_, err := r.CallVerb(ctx(), ports.VerbRequest{Verb: "search"})
	mustCode(t, err, domain.CodeNotImplemented)
}

func TestCloseIdempotentDoesNotCloseInjectedHTTP(t *testing.T) {
	var closes atomic.Int32
	h := &fakeHTTP{onClose: func() { closes.Add(1) }}
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: config.ZeusEndpointConfig{AuthMode: config.AuthNone},
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
		HTTP:     h,
	})
	if err := r.Close(ctx()); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(ctx()); err != nil {
		t.Fatal(err)
	}
	if closes.Load() != 0 {
		t.Fatalf("injected HTTP closed %d times", closes.Load())
	}
	_, err := r.ResolveAuth(ctx(), beerTarget(), false)
	mustCode(t, err, domain.CodeAuthFailed)
}

func TestCloseOwnedHTTPAfterMint(t *testing.T) {
	srv := mintServer(t, mintOpts{})
	defer srv.Close()
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: basicEndpoint(srv.URL),
		Secrets:  basicSecrets(),
	})
	if _, err := r.ResolveAuth(ctx(), beerTarget(), false); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(ctx()); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(ctx()); err != nil {
		t.Fatal(err)
	}
	var n *AuthResolver
	if err := n.Close(ctx()); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentResolveAuthCache(t *testing.T) {
	var posts atomic.Int32
	srv := mintServer(t, mintOpts{posts: &posts})
	defer srv.Close()
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: basicEndpoint(srv.URL),
		Secrets:  basicSecrets(),
	})
	defer r.Close(ctx())
	if _, err := r.ResolveAuth(ctx(), beerTarget(), false); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			auth, err := r.ResolveAuth(ctx(), beerTarget(), false)
			if err != nil {
				t.Errorf("%v", err)
				return
			}
			if auth.Headers[headerZeusSession] != fullSID {
				t.Errorf("%v", auth.Headers)
			}
		}()
	}
	wg.Wait()
	if posts.Load() != 1 {
		t.Fatalf("cache not shared under concurrency: posts=%d", posts.Load())
	}
}

func TestConcurrentFirstMintNoRace(t *testing.T) {
	var posts atomic.Int32
	srv := mintServer(t, mintOpts{posts: &posts})
	defer srv.Close()
	r := NewAuthResolver(AuthResolverOptions{
		Endpoint: basicEndpoint(srv.URL),
		Secrets:  basicSecrets(),
	})
	defer r.Close(ctx())
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			auth, err := r.ResolveAuth(ctx(), beerTarget(), false)
			if err != nil {
				t.Errorf("%v", err)
				return
			}
			if auth.Mode != "basic" || auth.Headers[headerZeusSession] != fullSID {
				t.Errorf("%+v", auth)
			}
		}()
	}
	wg.Wait()
	if posts.Load() < 1 {
		t.Fatal("no mint")
	}
}

func TestRedactorStillScrubsAuthSecrets(t *testing.T) {
	red := security.New()
	h := red.Headers(map[string]string{
		"Authorization":  "Bearer " + secretToken,
		"X-Zeus-Session": fullSID,
		"X-Zeus-Mode":    "analytics",
	})
	if h["Authorization"] != security.Redacted {
		t.Fatalf("Authorization %q", h["Authorization"])
	}
	if h["X-Zeus-Mode"] != "analytics" {
		t.Fatalf("mode %q", h["X-Zeus-Mode"])
	}
	js := red.JSONValue(map[string]any{
		"password":     secretPassword,
		"token":        secretToken,
		"bearer_token": secretToken,
		"ok":           true,
	}).(map[string]any)
	if js["password"] != security.Redacted || js["token"] != security.Redacted || js["bearer_token"] != security.Redacted {
		t.Fatalf("json %v", js)
	}
	if js["ok"] != true {
		t.Fatal("ok")
	}
	text := red.Text("Authorization: Bearer "+secretToken, -1)
	if strings.Contains(text, secretToken) {
		t.Fatalf("text %q", text)
	}
}

func TestProductionLoadRejectsAuthModeNone(t *testing.T) {
	_, err := config.FromMapping(map[string]any{
		"zeus": map[string]any{"url": "https://zeus.example", "auth_mode": "none"},
	}, "production")
	de := mustCode(t, err, domain.CodeConfigInvalid)
	if !strings.Contains(de.Message, "auth_mode=none") {
		t.Fatalf("message %q", de.Message)
	}
}

func TestMintJSONRoundTrip(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"session_id": fullSID, "expires_in": 1800})
	body, err := parseObjectJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if jsonString(body["session_id"]) != fullSID {
		t.Fatalf("%v", body)
	}
	if jsonNumber(body["expires_in"], 1) != 1800 {
		t.Fatalf("%v", body["expires_in"])
	}
	if jsonNumber(nil, defaultIdleTTL) != defaultIdleTTL {
		t.Fatal("default")
	}
}

type fakeHTTP struct {
	onClose func()
}

func (f *fakeHTTP) Request(_ context.Context, _ ports.HTTPRequest) (ports.HTTPResponse, error) {
	return ports.HTTPResponse{Status: 200, Headers: map[string]string{}}, nil
}

func (f *fakeHTTP) Close(_ context.Context) error {
	if f.onClose != nil {
		f.onClose()
	}
	return nil
}

var _ ports.HttpPort = (*fakeHTTP)(nil)
