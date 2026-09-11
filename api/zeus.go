// SPDX-License-Identifier: BUSL-1.1

package api

// ZeusAPI is the Mode 2 data-plane facade (Python DataAPI).
// Verb / search land in ZCG-13. The handle holds the Client without
// importing the root package (avoids an import cycle).
type ZeusAPI struct {
	host any
}

// NewZeusAPI binds a Client (or test fake) as the facade host.
func NewZeusAPI(host any) *ZeusAPI {
	if host == nil {
		return nil
	}
	return &ZeusAPI{host: host}
}

// Host is the bound Client. Nil-safe.
func (z *ZeusAPI) Host() any {
	if z == nil {
		return nil
	}
	return z.host
}
