// SPDX-License-Identifier: BUSL-1.1

// Package security is redaction, input validation, and jailbreak scoring.
//
// ZCG-5 lands DefaultRedactor (headers, nested JSON, text) and RedactAttrs.
// Secrets have no off-switch (LOGGING.md §5.3). Jailbreak scoring is later.
package security
