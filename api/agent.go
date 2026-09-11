// SPDX-License-Identifier: BUSL-1.1

package api

// AgentAPI is the Mode 1 agent-plane facade (Python AgentAPI).
// run_turn lands in ZCG-22. The handle holds the Client without importing
// the root package (avoids an import cycle).
type AgentAPI struct {
	host any
}

// NewAgentAPI binds a Client (or test fake) as the facade host.
func NewAgentAPI(host any) *AgentAPI {
	if host == nil {
		return nil
	}
	return &AgentAPI{host: host}
}

// Host is the bound Client. Nil-safe.
func (a *AgentAPI) Host() any {
	if a == nil {
		return nil
	}
	return a.host
}
