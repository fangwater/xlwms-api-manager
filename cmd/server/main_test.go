package main

import (
	"testing"
	"time"
)

func TestRequireLoopbackAddress(t *testing.T) {
	for _, address := range []string{"127.0.0.1:18083", "[::1]:18083"} {
		if err := requireLoopbackAddress(address); err != nil {
			t.Fatalf("%s: %v", address, err)
		}
	}
	if err := requireLoopbackAddress("0.0.0.0:18083"); err == nil {
		t.Fatal("public listen address must be rejected")
	}
}

func TestNextFulfillmentAuditRunUsesWholeHour(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 8, 5, 14, 56, 23, 0, location)
	want := time.Date(2026, 8, 5, 15, 0, 0, 0, location)
	if got := nextFulfillmentAuditRun(now, time.Hour); !got.Equal(want) {
		t.Fatalf("next run = %s, want %s", got, want)
	}
}
