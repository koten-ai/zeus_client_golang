// SPDX-License-Identifier: BUSL-1.1

// Package ports defines Zeus, LLM, catalog, secrets, clock, ids, HTTP, and jobs
// interfaces. Every method takes context.Context first (ZCG-11).
//
// Adapters are injected on Client construction. This package stays free of
// net/http and live clients — HttpPort uses []byte and maps. SecretStore
// landed in ZCG-12.
package ports
