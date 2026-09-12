// SPDX-License-Identifier: BUSL-1.1

package config

// Snapshot is a value copy of RuntimeConfig for Client.Config().
// Maps and slices are cloned so callers cannot alias the runtime.
type Snapshot = RuntimeConfig

// Snapshot returns a copy whose maps/slices do not alias c.
func (c RuntimeConfig) Snapshot() Snapshot {
	return c.clone()
}

func (c RuntimeConfig) clone() RuntimeConfig {
	out := c
	out.Zeus.ScopeCredentials = cloneNestedStringMap(c.Zeus.ScopeCredentials)
	out.LLM.Roles = cloneRoles(c.LLM.Roles)
	out.Settings.StickyFlags = copyBoolMap(c.Settings.StickyFlags)
	out.Settings.Messages = copyStringMap(c.Settings.Messages)
	out.Settings.OutputRequest = copyAnyMap(c.Settings.OutputRequest)
	out.Settings.Rules = copyStringMap(c.Settings.Rules)
	out.Settings.TenantRules = copyStringMap(c.Settings.TenantRules)
	if len(c.Jobs.Models) == 0 {
		out.Jobs.Models = nil
	} else {
		out.Jobs.Models = copyAnyMap(c.Jobs.Models)
	}
	out.ScopeContracts = copyAnyMap(c.ScopeContracts)
	if !c.SemanticCache.Enabled {
		out.SemanticCache.ApplyToModes = nil
		out.SemanticCache.Recall.Types = nil
		out.SemanticCache.Inject.IncludeFields = nil
		out.SemanticCache.Write.TypesAllowed = nil
		out.SemanticCache.Privacy.DenyRegex = nil
	} else {
		out.SemanticCache.ApplyToModes = copyStrings(c.SemanticCache.ApplyToModes)
		out.SemanticCache.Recall.Types = copyStrings(c.SemanticCache.Recall.Types)
		out.SemanticCache.Inject.IncludeFields = copyStrings(c.SemanticCache.Inject.IncludeFields)
		out.SemanticCache.Write.TypesAllowed = copyStrings(c.SemanticCache.Write.TypesAllowed)
		out.SemanticCache.Privacy.DenyRegex = copyStrings(c.SemanticCache.Privacy.DenyRegex)
	}
	return out
}

func cloneRoles(m map[string]LlmRoleConfig) map[string]LlmRoleConfig {
	if m == nil {
		return nil
	}
	out := make(map[string]LlmRoleConfig, len(m))
	for k, v := range m {
		if v.Temperature != nil {
			t := *v.Temperature
			v.Temperature = &t
		}
		out[k] = v
	}
	return out
}

func cloneNestedStringMap(m map[string]map[string]string) map[string]map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]map[string]string, len(m))
	for k, v := range m {
		out[k] = copyStringMap(v)
	}
	return out
}
