// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"net"
	"os"
	"strings"
	"time"
)

const (
	// ProductUser is the product SDK stamp (Python PRODUCT_USER).
	ProductUser = "zeus_client"
	// HubUser is the Hub Debug Chat stamp (UNIFICATION U5). Set via Options, not a second loop.
	HubUser        = "admin"
	ipEnvName      = "ZEUS_CLIENT_IP"
	stampComponent = "domain.stamps"
)

// ProductUserEnum is the closed set of known stamp users (Python PRODUCT_USER_ENUM).
var ProductUserEnum = map[string]struct{}{
	"zeus_client": {},
	"zeus":        {},
	"helios":      {},
	"admin":       {},
}

// StampOptions builds a root stamp (Python product_stamp kwargs).
// User empty → ProductUser. Hub Options.StampUser=admin uses the same builder.
type StampOptions struct {
	User      string
	IPAddress string
	Version   string
	TS        string
	Scope     string
	SessionID string
	Now       func() time.Time
}

// ResolveStampUser is the closed-enum stamp user. Empty or unknown → product
// zeus_client (never invent). Hub admin is Options.StampUser, same builder.
func ResolveStampUser(user string) string {
	u := strings.TrimSpace(user)
	if u == "" {
		return ProductUser
	}
	if _, ok := ProductUserEnum[u]; ok {
		return u
	}
	return ProductUser
}

// IsIPText reports whether value is a textual IPv4 or IPv6 address.
func IsIPText(value string) bool {
	text := strings.TrimSpace(value)
	if text == "" {
		return false
	}
	return net.ParseIP(text) != nil
}

// IsLoopbackIP reports whether value is a loopback address.
func IsLoopbackIP(value string) bool {
	text := strings.TrimSpace(value)
	if text == "" {
		return false
	}
	ip := net.ParseIP(text)
	return ip != nil && ip.IsLoopback()
}

// BestEffortHostIP is a non-loopback host IP when the OS can resolve one.
func BestEffortHostIP() string {
	for _, probe := range []struct {
		network string
		addr    string
	}{
		{"udp4", "8.8.8.8:80"},
		{"udp6", "[2001:4860:4860::8888]:80"},
	} {
		c, err := net.DialTimeout(probe.network, probe.addr, 500*time.Millisecond)
		if err != nil {
			continue
		}
		addr := c.LocalAddr()
		_ = c.Close()
		host, _, err := net.SplitHostPort(addr.String())
		if err != nil {
			host = addr.String()
		}
		if IsIPText(host) && !IsLoopbackIP(host) {
			return host
		}
	}
	return ""
}

// ResolveClientIP prefers config, then ZEUS_CLIENT_IP, then optional host probe.
// Configured or env values may be loopback (operator-explicit). Probe never
// returns loopback. env nil uses process env; empty map isolates tests.
func ResolveClientIP(configIP string, env map[string]string, probeHost bool) string {
	var envVal string
	if env == nil {
		envVal = os.Getenv(ipEnvName)
	} else {
		envVal = env[ipEnvName]
	}
	for _, raw := range []string{configIP, envVal} {
		text := strings.TrimSpace(raw)
		if IsIPText(text) {
			return text
		}
	}
	if probeHost {
		return BestEffortHostIP()
	}
	return ""
}

// ProductStamp is root identity for session/report sinks.
// Default user is zeus_client. Hub Options.StampUser=admin shares this builder.
// Version empty → caller should pass zeusclient.Version; tests may set it.
func ProductStamp(opts StampOptions) map[string]any {
	ver := strings.TrimSpace(opts.Version)
	out := map[string]any{
		"user":    ResolveStampUser(opts.User),
		"version": ver,
	}
	ip := strings.TrimSpace(opts.IPAddress)
	if IsIPText(ip) {
		out["ip_address"] = ip
	}
	if opts.TS != "" {
		out["ts"] = opts.TS
	} else {
		now := time.Now
		if opts.Now != nil {
			now = opts.Now
		}
		out["ts"] = now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	if opts.Scope != "" {
		out["scope"] = opts.Scope
	}
	if opts.SessionID != "" {
		out["session_id"] = opts.SessionID
	}
	return out
}

// AssertProductStamp is the Helios-style product-purity filter — product
// sinks must never claim Hub admin traffic. Invalid IP is refused.
// Returns nil when the stamp is ok. Hub admin stamps fail this check on purpose.
func AssertProductStamp(stamp map[string]any) error {
	user := asString(stamp["user"])
	if user != ProductUser {
		return New(CodeInvalidArgument, stampComponent,
			WithMessage("product stamp user must be "+ProductUser+", got "+user))
	}
	if user == "admin" {
		return New(CodeInvalidArgument, stampComponent,
			WithMessage("product SDK must not stamp user=admin"))
	}
	if ip, ok := stamp["ip_address"]; ok && ip != nil {
		if !IsIPText(asString(ip)) {
			return New(CodeInvalidArgument, stampComponent,
				WithMessage("ip_address must be omitted or a real IP"))
		}
	}
	return nil
}
