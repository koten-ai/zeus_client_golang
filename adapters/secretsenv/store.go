// SPDX-License-Identifier: BUSL-1.1

package secretsenv

import (
	"context"
	"fmt"
	"os"

	"github.com/koten-ai/zeus_client_golang/ports"
)

// Store reads secrets from a map, or os.Getenv when Environ is nil.
type Store struct {
	Environ map[string]string
}

var _ ports.SecretStore = (*Store)(nil)

// New returns a store that reads process env at Get time.
func New() *Store {
	return &Store{}
}

// NewWithEnviron returns a store over a fixed mapping (tests).
func NewWithEnviron(env map[string]string) *Store {
	return &Store{Environ: env}
}

// Get returns the named secret. Empty name, missing, or empty value → ok=false.
func (s *Store) Get(_ context.Context, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	var val string
	var ok bool
	if s == nil || s.Environ == nil {
		val, ok = os.LookupEnv(name)
	} else {
		val, ok = s.Environ[name]
	}
	if !ok || val == "" {
		return "", false
	}
	return val, true
}

// String never dumps secret values (Python EnvSecretStore.__repr__).
func (s *Store) String() string {
	n := 0
	if s != nil && s.Environ != nil {
		n = len(s.Environ)
	}
	return fmt.Sprintf("EnvSecretStore(keys=%d)", n)
}
