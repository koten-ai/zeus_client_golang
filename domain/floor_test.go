// SPDX-License-Identifier: BUSL-1.1

package domain

import "testing"

func TestFloorRequiredMapping(t *testing.T) {
	if FloorRequiredForBaseID("base-1") != "client-floor-1" {
		t.Fatal("base-1")
	}
	if FloorRequiredForBaseID("base-5.3") != "client-floor-5" {
		t.Fatal("base-5.3")
	}
	if FloorRequiredForBaseID("base-6.1") != "client-floor-6.1" {
		t.Fatal("base-6.1")
	}
	if FloorRequiredForBaseID("cus-3") != "client-floor-5" {
		t.Fatal("cus-3")
	}
}

func TestFloorFailClosed(t *testing.T) {
	err := AssertFloorAllows("client-floor-5", "base-6.1", false)
	requireCode(t, err, CodePreconditionFailed)
}

func TestFloorDegradedLogsNotRaise(t *testing.T) {
	if err := AssertFloorAllows("client-floor-5", "base-6.1", true); err != nil {
		t.Fatal(err)
	}
}

func TestFloor5AllowsBase53(t *testing.T) {
	if err := AssertFloorAllows("client-floor-5", "base-5.3", false); err != nil {
		t.Fatal(err)
	}
}
