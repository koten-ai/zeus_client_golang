// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"regexp"
	"testing"
)

var sixDigits = regexp.MustCompile(`^[0-9]{6}$`)

func TestOKResultEnvelope(t *testing.T) {
	rid := "a1000000-0000-4000-8000-000000000001"
	r := OKResult(map[string]any{"hits": []any{}}, rid)
	m := r.Map()
	if m["result"] != true {
		t.Fatalf("%v", m["result"])
	}
	if m["req_id"] != rid {
		t.Fatalf("%v", m["req_id"])
	}
	if _, ok := m["error"]; ok {
		t.Fatal("error on success")
	}
	val, ok := m["value"].(map[string]any)
	if !ok {
		t.Fatalf("value %T", m["value"])
	}
	if _, ok := val["hits"]; !ok {
		t.Fatal("hits")
	}
	got, err := r.Unwrap()
	if err != nil {
		t.Fatal(err)
	}
	if got["hits"] == nil {
		t.Fatal("unwrap")
	}
}

func TestFailResultEnvelope(t *testing.T) {
	rid := "40900000-0000-4000-8000-000000000409"
	err := NewZeusTransport(CodeZeusContractRequired, "internal.httpx")
	r := FailResult[map[string]any](err, rid)
	m := r.Map()
	if m["result"] != false {
		t.Fatal("result")
	}
	if m["req_id"] != rid {
		t.Fatalf("%v", m["req_id"])
	}
	if _, ok := m["value"]; ok {
		t.Fatal("value on failure")
	}
	obj, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("error %T", m["error"])
	}
	code, _ := obj["code"].(string)
	if !sixDigits.MatchString(code) {
		t.Fatalf("code %q", code)
	}
	if code != string(CodeZeusContractRequired) {
		t.Fatalf("code %q", code)
	}
	if obj["type"] != "zeus_http" {
		t.Fatalf("type %v", obj["type"])
	}
	if obj["retryable"] != false {
		t.Fatal("retryable")
	}
	if obj["message"] == "" {
		t.Fatal("message")
	}
	_, uerr := r.Unwrap()
	if uerr == nil {
		t.Fatal("unwrap err")
	}
	de, ok := AsError(uerr)
	if !ok || de.Code != CodeZeusContractRequired {
		t.Fatalf("%v", uerr)
	}
}

func TestReqIDFromHeaders(t *testing.T) {
	if ReqIDFromHeaders(nil) != "" {
		t.Fatal("nil")
	}
	got := ReqIDFromHeaders(map[string]string{"x-zeus-req-id": "  abc  "})
	if got != "abc" {
		t.Fatalf("%q", got)
	}
	if !HasReqIDHeader(map[string]string{"X-Zeus-Req-Id": "x"}) {
		t.Fatal("has")
	}
	if HasReqIDHeader(map[string]string{"X-Other": "1"}) {
		t.Fatal("other")
	}
}
