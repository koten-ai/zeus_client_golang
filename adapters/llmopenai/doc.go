// SPDX-License-Identifier: BUSL-1.1

// Package llmopenai is the OpenAI-compatible LLM adapter (Python
// adapters.llm_openai_compatible). xAI is the default pin; provider=custom
// requires operator base_url + model (api_style default openai_compatible).
//
// ZCG-14: POST /chat/completions, classify 050010–050018, RetryBudget only
// for retryable classes. Quota 429 never retries. No Agent loop.
package llmopenai
