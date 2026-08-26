package migrations

import (
	"strings"
	"testing"
)

func TestInitSQLStoresManualFulfillmentAuditResolutions(t *testing.T) {
	for _, fragment := range []string{
		"terminal_status text NOT NULL DEFAULT ''",
		"terminal_note text NOT NULL DEFAULT ''",
		"manual_resolved_at timestamptz",
		"xlwms_fulfillment_audits_terminal_status_check",
		"manually_fulfilled', 'cancelled', 'not_required', 'other'",
	} {
		if !strings.Contains(InitSQL, fragment) {
			t.Fatalf("InitSQL missing %q", fragment)
		}
	}
}
