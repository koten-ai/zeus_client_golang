// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"regexp"
	"strconv"
	"strings"
)

const (
	floorComponent = "domain.floor"
	// DefaultClientFloor is Python DEFAULT_CLIENT_FLOOR.
	DefaultClientFloor = "client-floor-5"
)

var (
	floorRankTable = map[string]int{
		"client-floor-1":   1,
		"client-floor-4":   4,
		"client-floor-5":   5,
		"client-floor-6":   6,
		"client-floor-6.1": 61,
	}
	floorParseRe = regexp.MustCompile(`^client-floor-(\d+)(?:\.(\d+))?$`)
	baseIDRe     = regexp.MustCompile(`^base-(?P<maj>\d+)(?:\.(?P<min>\d+))?$`)
)

// FloorRank is the numeric rank of a client floor id.
func FloorRank(floorID string) int {
	key := strings.TrimSpace(floorID)
	if key == "" {
		key = DefaultClientFloor
	}
	if n, ok := floorRankTable[key]; ok {
		return n
	}
	m := floorParseRe.FindStringSubmatch(key)
	if m == nil {
		return floorRankTable[DefaultClientFloor]
	}
	major, _ := strconv.Atoi(m[1])
	minor := 0
	if m[2] != "" {
		minor, _ = strconv.Atoi(m[2])
	}
	if minor != 0 {
		return major*10 + minor
	}
	return major
}

// FloorRequiredForBaseID maps pack base_id to the minimum client floor.
func FloorRequiredForBaseID(baseID string) string {
	raw := strings.TrimSpace(baseID)
	if strings.HasPrefix(raw, "cus-") {
		return DefaultClientFloor
	}
	m := baseIDRe.FindStringSubmatch(raw)
	if m == nil {
		return DefaultClientFloor
	}
	major, _ := strconv.Atoi(m[baseIDRe.SubexpIndex("maj")])
	minor := 0
	if idx := baseIDRe.SubexpIndex("min"); idx >= 0 && m[idx] != "" {
		minor, _ = strconv.Atoi(m[idx])
	}
	if major <= 1 {
		return "client-floor-1"
	}
	if major <= 4 {
		return "client-floor-4"
	}
	if major == 5 {
		return "client-floor-5"
	}
	if major == 6 && minor >= 1 {
		return "client-floor-6.1"
	}
	if major >= 6 {
		return "client-floor-6"
	}
	return DefaultClientFloor
}

// FloorViolation is the fail-closed floor check (nil when allowed).
func FloorViolation(clientFloor, baseID string) error {
	if strings.TrimSpace(baseID) == "" {
		return nil
	}
	need := FloorRequiredForBaseID(baseID)
	if FloorRank(clientFloor) >= FloorRank(need) {
		return nil
	}
	msg := "catalog base_id '" + baseID + "' requires " + need + "; client.floor is '" + clientFloor + "'"
	return NewCatalog(CodePreconditionFailed, floorComponent,
		WithMessage(msg),
		WithDetails(map[string]any{
			"base_id":        baseID,
			"required_floor": need,
			"client_floor":   clientFloor,
		}),
	)
}

// AssertFloorAllows fails closed when the pack wire requires a higher floor.
// allowDegraded skips the error (caller may log).
func AssertFloorAllows(clientFloor, baseID string, allowDegraded bool) error {
	err := FloorViolation(clientFloor, baseID)
	if err == nil || allowDegraded {
		return nil
	}
	return err
}
