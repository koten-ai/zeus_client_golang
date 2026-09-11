// SPDX-License-Identifier: BUSL-1.1

// Package projectors maps journal / hop records onto sink shapes.
//
// ZCG-16 lands the session-trace projector (POST /v2/session/trace per hop,
// identical aggregate body, primary last, dispatch req_id join) and the
// public_trace widget projection (G2 never in answer). Projectors soft-fail:
// they never raise out of a successful domain turn.
package projectors
