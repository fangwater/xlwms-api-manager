package httpapi

import (
	"math"
	"testing"
)

func TestValidateInventoryAlertThreshold(t *testing.T) {
	value := 100.0
	result, err := validateInventoryAlertThreshold(&value)
	if err != nil || result != 100 {
		t.Fatalf("default threshold must be accepted: result=%v err=%v", result, err)
	}
	for name, value := range map[string]float64{
		"negative":     -1,
		"too large":    1_000_000_001,
		"not a number": math.NaN(),
		"infinity":     math.Inf(1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := validateInventoryAlertThreshold(&value); err == nil {
				t.Fatal("expected invalid threshold to be rejected")
			}
		})
	}
	if _, err := validateInventoryAlertThreshold(nil); err == nil {
		t.Fatal("missing threshold must be rejected")
	}
}
