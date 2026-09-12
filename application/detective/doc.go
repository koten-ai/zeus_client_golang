// SPDX-License-Identifier: BUSL-1.1

// Package detective is the Client Detective briefing projector (ZCG-27).
//
// Journal is SoT; this package is a pure local projector (Overview · Prompt ·
// Diagnosis + support pack). Package default is off (product Q&A). Hub
// profile, StampUser=admin, or DebugPolicy.DetectiveBriefing=true turns it
// on. Kill-switch: ZEUS_CLIENT_DETECTIVE=0. Soft-fail: SafeBuild never
// raises into a successful domain turn. G2 stays out of the support-pack
// user-facing slice.
package detective
