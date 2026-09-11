// SPDX-License-Identifier: BUSL-1.1

package zeusclient

import "sync"

// runtime is the hexagonal wiring bundle (Python ZeusRuntime.Services).
// Ports and adapters are injected later (ZCG-11). No process-global HTTP.
type runtime struct {
	mu     sync.Mutex
	closed bool
}

func newRuntime() *runtime {
	return &runtime{}
}

func (r *runtime) close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}
