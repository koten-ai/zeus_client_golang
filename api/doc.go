// SPDX-License-Identifier: BUSL-1.1

// Package api is the typed facade: Agent, Zeus (data verbs), Catalog, Debug, Session.
//
// ZCG-11 lands stub ZeusAPI / AgentAPI handles that hold the Client. ZCG-15
// lands CatalogAPI (mock load, extract stamp, public mini_schema). ZCG-13
// fills ZeusAPI Call / Search / Find / Get / Project + SearchSuggest. ZCG-19
// fills SessionAPI Create / Continue / Rehydrate. ZCG-16 fills SessionAPI.Trace.
// ZCG-24 fills AgentAPI.RunTurn; ZCG-25 defaults the agent chain to SecurityHooks.
package api
