package main

import "testing"

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
