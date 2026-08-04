package store

import (
	"slices"
	"testing"

	"xlwms-api-manager/internal/model"
)

func TestOneMVariantsMatchesMissingTrailingM(t *testing.T) {
	variants := oneMVariants("NSH+H-50Pcs-Orange-42c")
	if !slices.Contains(variants, "NSH+H-50Pcs-Orange-42cm") {
		t.Fatalf("expected trailing-m candidate, got %#v", variants)
	}
}

func TestCompatibleOneMSpecRequiresUniqueCompleteCandidate(t *testing.T) {
	canonical := model.WarehouseSKUSpec{WarehouseSKU: "SKU-cm", Complete: true, Enabled: true}
	got, candidates, ok := compatibleOneMSpec("SKU-c", map[string]model.WarehouseSKUSpec{"SKU-cm": canonical})
	if !ok || got.WarehouseSKU != canonical.WarehouseSKU || len(candidates) != 1 {
		t.Fatalf("unexpected compatibility result: got=%#v candidates=%#v ok=%v", got, candidates, ok)
	}
	_, candidates, ok = compatibleOneMSpec("SKU-c", map[string]model.WarehouseSKUSpec{
		"SKU-cm": canonical,
		"SKU-mc": {WarehouseSKU: "SKU-mc", Complete: true, Enabled: true},
	})
	if ok || len(candidates) != 2 {
		t.Fatalf("ambiguous candidates must not match: candidates=%#v ok=%v", candidates, ok)
	}
}
