// SPDX-License-Identifier: BUSL-1.1

package security

import (
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestRedactHeadersAuthorizationAndAPIKey(t *testing.T) {
	r := New()
	out := r.Headers(map[string]string{
		"Authorization": "Bearer sk-secret-token",
		"X-Api-Key":     "abc123",
		"Content-Type":  "application/json",
		"Cookie":        "session=xyz",
		"X-Zeus-Mode":   "analytics",
	})
	if out["Authorization"] != Redacted {
		t.Fatalf("Authorization: %q", out["Authorization"])
	}
	if out["X-Api-Key"] != Redacted {
		t.Fatalf("X-Api-Key: %q", out["X-Api-Key"])
	}
	if out["Cookie"] != Redacted {
		t.Fatalf("Cookie: %q", out["Cookie"])
	}
	if out["Content-Type"] != "application/json" {
		t.Fatalf("Content-Type: %q", out["Content-Type"])
	}
	if out["X-Zeus-Mode"] != "analytics" {
		t.Fatalf("X-Zeus-Mode: %q", out["X-Zeus-Mode"])
	}
}

func TestRedactHeadersCaseInsensitive(t *testing.T) {
	r := New()
	out := r.Headers(map[string]string{
		"authorization": "Basic dXNlcjpwYXNz",
		"X-API-KEY":     "k",
	})
	if out["authorization"] != Redacted {
		t.Fatalf("authorization: %q", out["authorization"])
	}
	if out["X-API-KEY"] != Redacted {
		t.Fatalf("X-API-KEY: %q", out["X-API-KEY"])
	}
}

func TestRedactHeadersFullDenySet(t *testing.T) {
	r := New()
	in := map[string]string{
		"Proxy-Authorization": "Basic abc",
		"api-key":             "k",
		"Set-Cookie":          "sid=1",
		"X-Auth-Token":        "t",
		"X-Access-Token":      "a",
		"Accept":              "application/json",
	}
	out := r.Headers(in)
	for _, k := range []string{"Proxy-Authorization", "api-key", "Set-Cookie", "X-Auth-Token", "X-Access-Token"} {
		if out[k] != Redacted {
			t.Errorf("%s: %q", k, out[k])
		}
	}
	if out["Accept"] != "application/json" {
		t.Fatalf("Accept: %q", out["Accept"])
	}
}

func TestRedactHeadersDoesNotMutateInput(t *testing.T) {
	r := New()
	in := map[string]string{"Authorization": "Bearer secret"}
	_ = r.Headers(in)
	if in["Authorization"] != "Bearer secret" {
		t.Fatalf("mutated: %q", in["Authorization"])
	}
}

func TestRedactHeadersNil(t *testing.T) {
	out := New().Headers(nil)
	if out == nil || len(out) != 0 {
		t.Fatalf("%v", out)
	}
}

func TestRedactJSONNestedKeys(t *testing.T) {
	r := New()
	payload := map[string]any{
		"user":     "alice",
		"password": "s3cret",
		"nested": map[string]any{
			"api_key": "key-1",
			"token":   "tok",
			"ok":      true,
			"deeper": map[string]any{
				"authorization": "Bearer x",
				"count":         1,
			},
		},
		"items": []any{
			map[string]any{"secret": "nope", "id": 7},
		},
	}
	out, ok := r.JSONValue(payload).(map[string]any)
	if !ok {
		t.Fatalf("%T", r.JSONValue(payload))
	}
	if out["user"] != "alice" {
		t.Fatalf("user: %v", out["user"])
	}
	if out["password"] != Redacted {
		t.Fatalf("password: %v", out["password"])
	}
	nested := out["nested"].(map[string]any)
	if nested["api_key"] != Redacted {
		t.Fatalf("api_key: %v", nested["api_key"])
	}
	if nested["token"] != Redacted {
		t.Fatalf("token: %v", nested["token"])
	}
	if nested["ok"] != true {
		t.Fatalf("ok: %v", nested["ok"])
	}
	deeper := nested["deeper"].(map[string]any)
	if deeper["authorization"] != Redacted {
		t.Fatalf("authorization: %v", deeper["authorization"])
	}
	if deeper["count"] != 1 {
		t.Fatalf("count: %v", deeper["count"])
	}
	items := out["items"].([]any)
	item0 := items[0].(map[string]any)
	if item0["secret"] != Redacted {
		t.Fatalf("secret: %v", item0["secret"])
	}
	if item0["id"] != 7 {
		t.Fatalf("id: %v", item0["id"])
	}
}

func TestRedactJSONCreditCardAndSSNKeys(t *testing.T) {
	r := New()
	out := r.JSONValue(map[string]any{
		"credit_card": "4111",
		"ssn":         "123-45-6789",
		"name":        "bob",
	}).(map[string]any)
	if out["credit_card"] != Redacted {
		t.Fatalf("credit_card: %v", out["credit_card"])
	}
	if out["ssn"] != Redacted {
		t.Fatalf("ssn: %v", out["ssn"])
	}
	if out["name"] != "bob" {
		t.Fatalf("name: %v", out["name"])
	}
}

func TestRedactJSONListOfScalarsPassthrough(t *testing.T) {
	r := New()
	got := r.JSONValue([]any{1, "a", nil})
	want := []any{1, "a", nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%#v", got)
	}
}

func TestRedactJSONStringLeafBearer(t *testing.T) {
	r := New()
	got := r.JSONValue(map[string]any{"note": "Authorization: Bearer sk-live-abc123"})
	out := got.(map[string]any)
	if out["note"] != Redacted {
		t.Fatalf("note: %v", out["note"])
	}
}

func TestRedactJSONDoesNotMutateInput(t *testing.T) {
	in := map[string]any{"password": "s3cret", "user": "alice"}
	_ = New().JSONValue(in)
	if in["password"] != "s3cret" {
		t.Fatalf("mutated: %v", in["password"])
	}
}

func TestRedactTextMasksBearerAndTruncates(t *testing.T) {
	r := New()
	text := strings.Repeat("call with Authorization: Bearer sk-live-abc and more padding ", 5)
	out := r.Text(text, 80)
	if strings.Contains(out, "sk-live-abc") {
		t.Fatalf("key leaked: %q", out)
	}
	if strings.Contains(out, "Bearer") && !strings.Contains(out, Redacted) {
		t.Fatalf("Bearer leaked: %q", out)
	}
	if len([]rune(out)) > 80+len([]rune("…")) {
		t.Fatalf("too long: %d %q", len([]rune(out)), out)
	}
}

func TestRedactTextShortUnchangedIfClean(t *testing.T) {
	if got := New().Text("hello world", 100); got != "hello world" {
		t.Fatalf("%q", got)
	}
}

func TestRedactTextMaxCharsZero(t *testing.T) {
	if got := New().Text("hello", 0); got != "…" {
		t.Fatalf("%q", got)
	}
	if got := New().Text("", 0); got != "" {
		t.Fatalf("empty: %q", got)
	}
}

func TestRedactTextXAIKey(t *testing.T) {
	out := New().Text("use xai-abcdefgh token", 100)
	if strings.Contains(out, "xai-abcdefgh") {
		t.Fatalf("xai key leaked: %q", out)
	}
	if out != "use "+Redacted+" token" {
		t.Fatalf("%q", out)
	}
}

func TestNewReturnsDefaultRedactor(t *testing.T) {
	r := New()
	if r == nil {
		t.Fatal("nil")
	}
	var _ Redactor = r
}

func TestRedactAttrsHardDenyWhenRedactFalse(t *testing.T) {
	attrs := RedactAttrs(map[string]any{
		"authorization": "Bearer sk-secret",
		"req_id":        "abc",
		"password":      "s3cret",
		"api_key":       "k",
	}, false)
	if attrs["authorization"] != Redacted {
		t.Fatalf("authorization: %v", attrs["authorization"])
	}
	if attrs["password"] != Redacted {
		t.Fatalf("password: %v", attrs["password"])
	}
	if attrs["api_key"] != Redacted {
		t.Fatalf("api_key: %v", attrs["api_key"])
	}
	if attrs["req_id"] != "abc" {
		t.Fatalf("req_id: %v", attrs["req_id"])
	}
}

func TestRedactAttrsNestedJSONWhenRedactFalse(t *testing.T) {
	attrs := RedactAttrs(map[string]any{
		"body": map[string]any{"password": "s3cret", "user": "alice"},
	}, false)
	body, ok := attrs["body"].(map[string]any)
	if !ok {
		t.Fatalf("%T", attrs["body"])
	}
	if body["password"] != Redacted {
		t.Fatalf("nested password: %v", body["password"])
	}
	if body["user"] != "alice" {
		t.Fatalf("user: %v", body["user"])
	}
}

func TestRedactAttrsSkipsNil(t *testing.T) {
	attrs := RedactAttrs(map[string]any{"req_id": "abc", "missing": nil}, true)
	if _, ok := attrs["missing"]; ok {
		t.Fatal("nil value should be omitted")
	}
	if attrs["req_id"] != "abc" {
		t.Fatalf("%v", attrs["req_id"])
	}
}

func TestRedactAttrsDoesNotMutateInput(t *testing.T) {
	in := map[string]any{"authorization": "Bearer x", "n": 1}
	_ = RedactAttrs(in, false)
	if in["authorization"] != "Bearer x" {
		t.Fatalf("mutated: %v", in["authorization"])
	}
}

func TestRedactorConcurrent(t *testing.T) {
	r := New()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.Headers(map[string]string{"Authorization": "Bearer x"})
			_ = r.JSONValue(map[string]any{"password": "p", "n": 1})
			_ = r.Text("Bearer tokentokentoken", 10)
			_ = RedactAttrs(map[string]any{"api_key": "k", "req_id": "r"}, false)
		}()
	}
	wg.Wait()
}
