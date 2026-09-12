// SPDX-License-Identifier: BUSL-1.1

package domain

// Post-terminate Client policy table (POLICY_TABLE_SPEC).
// Model triggers are signals; Client policy is law. G2 scores stay in artifacts only.
// Jailbreak rule ids live in rules.go (JailbreakRuleIDs) — do not fork a second map.

const (
	PolicyAnswer  = "answer"
	PolicyClarify = "clarify"
	PolicyRefuse  = "refuse"
	PolicyError   = "error"
)

const (
	defaultMessageJailbreakSoft = "I can only help with questions about our product using store data. " +
		"I can't ignore those limits or invent offers that are not in our system."
	defaultMessageFailure = "Sorry — I can't complete that request. Could you rephrase what you need?"
)

// PolicySettings is the domain-local slice of product settings decide_policy reads.
// Do not import config.ClientSettings (import cycle).
type PolicySettings struct {
	Messages                map[string]string
	StickyFlags             map[string]bool
	SoftRequirePolicyAction bool
}

// DecidePolicyOpts is decide_policy kwargs.
type DecidePolicyOpts struct {
	Settings            PolicySettings
	HooksJailbreakScore float64
	HooksMustRefuse     bool
	BrandInjectPresent  bool
}

// PolicyDecision is the post-terminate table outcome (Python PolicyDecision).
type PolicyDecision struct {
	Policy                         string
	UIText                         string
	Flags                          map[string]bool
	HooksJailbreakScore            float64
	MustRefuse                     bool
	Forced                         bool
	Reason                         string
	SoftRequirePolicyActionMissing bool
}

// UI is the G1 bind — never G2.
func (d PolicyDecision) UI(layer LayerA) map[string]any {
	t := d.UIText
	return UIView(layer, &t)
}

// Artifacts is full Layer A + G2/G3 + dual jailbreak scores.
func (d PolicyDecision) Artifacts(layer LayerA) map[string]any {
	return ArtifactsView(layer, d.HooksJailbreakScore, d.Policy, d.Flags)
}

// StickyOrFlags is sticky OR flags across turns (ZC-WISH-023). False does not clear.
func StickyOrFlags(prior map[string]bool, triggers map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range prior {
		if v {
			out[k] = true
		}
	}
	for k, v := range triggers {
		if v {
			out[k] = true
		}
	}
	return out
}

// JailbreakKeysHit is true when any SDK jailbreak id is true in triggers.
func JailbreakKeysHit(triggers map[string]bool) bool {
	for k := range JailbreakRuleIDs {
		if triggers[k] {
			return true
		}
	}
	return false
}

// MapMessage maps policy → ui_text. Refuse chrome is the soft default — not G2 dumps.
func MapMessage(policy string, settings PolicySettings, layer LayerA) string {
	msgs := settings.Messages
	if msgs == nil {
		msgs = map[string]string{}
	}
	switch policy {
	case PolicyRefuse:
		soft := msgs["message_jailbreak_soft"]
		if soft == "" {
			soft = defaultMessageJailbreakSoft
		}
		if layer.Summary != "" && layer.PolicyAction == PolicyRefuse {
			return layer.Summary
		}
		return soft
	case PolicyError:
		if m := msgs["message_failure"]; m != "" {
			return m
		}
		return defaultMessageFailure
	case PolicyClarify:
		if layer.Summary != "" {
			return layer.Summary
		}
		if m := msgs["message_clarify"]; m != "" {
			return m
		}
		return layer.Summary
	default:
		return layer.Summary
	}
}

// DecidePolicy runs the normative post-terminate policy table every return.
// First match wins (POLICY_TABLE_SPEC §3).
func DecidePolicy(layer LayerA, opts DecidePolicyOpts) PolicyDecision {
	flags := StickyOrFlags(opts.Settings.StickyFlags, layer.BusinessRulesTriggers)
	score := opts.HooksJailbreakScore
	jbaF := 0.0
	if layer.JailBreakAttempt != nil {
		jbaF = *layer.JailBreakAttempt
	}

	softMissing := false
	if opts.Settings.SoftRequirePolicyAction && opts.BrandInjectPresent && layer.PolicyAction == "" {
		softMissing = true
	}

	var (
		policy string
		forced bool
		reason string
	)
	switch {
	case opts.HooksMustRefuse:
		policy, forced, reason = PolicyRefuse, true, "hooks_must_refuse"
	case !layer.OK():
		policy, forced, reason = PolicyError, true, "layer_a_validation_failed"
	case JailbreakKeysHit(layer.BusinessRulesTriggers) && (jbaF >= 0.5 || score >= 0.5):
		policy, forced, reason = PolicyRefuse, true, "jailbreak_triggers_and_score"
	case layer.PolicyAction == PolicyAnswer || layer.PolicyAction == PolicyClarify ||
		layer.PolicyAction == PolicyRefuse || layer.PolicyAction == PolicyError:
		policy, forced, reason = layer.PolicyAction, false, "model_policy_action"
	case incompleteObject(layer.QueryDecomposition) || incompleteObject(layer.Decomposition):
		policy, forced, reason = PolicyClarify, false, "incomplete_layer_a"
	default:
		policy, forced, reason = PolicyAnswer, false, "default_answer"
	}

	return PolicyDecision{
		Policy:                         policy,
		UIText:                         MapMessage(policy, opts.Settings, layer),
		Flags:                          flags,
		HooksJailbreakScore:            score,
		MustRefuse:                     policy == PolicyRefuse && forced,
		Forced:                         forced,
		Reason:                         reason,
		SoftRequirePolicyActionMissing: softMissing,
	}
}
