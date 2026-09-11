// SPDX-License-Identifier: BUSL-1.1

package httpx

import "testing"

func TestShouldRetryPolicy(t *testing.T) {
	cases := []struct {
		method string
		status int
		want   bool
	}{
		{"POST", 409, false},
		{"POST", 401, false},
		{"POST", 400, false},
		{"GET", 404, false},
		{"POST", 500, false},
		{"GET", 500, true},
		{"HEAD", 503, true},
		{"GET", 200, false},
		{"POST", 200, false},
	}
	for _, tc := range cases {
		got := ShouldRetry(tc.method, tc.status)
		if got != tc.want {
			t.Errorf("%s %d: got %v want %v", tc.method, tc.status, got, tc.want)
		}
	}
}
