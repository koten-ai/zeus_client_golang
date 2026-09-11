// SPDX-License-Identifier: BUSL-1.1

// Package config is RuntimeConfig, named profiles, and loaders (Python
// zeus_client.config).
//
// Load order: defaults → pins / JSON file → env. Pins and config hold
// api_key_env / password_env names, never secret values. Production
// rejects zeus.auth_mode=none and TLS verify off.
//
// Secret values are resolved later via ports.SecretStore (adapters/secretsenv).
package config
