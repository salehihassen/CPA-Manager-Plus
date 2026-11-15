package modelprice_test

import (
	"context"
	"math"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/pricing"
)

func TestPriceHistoryPreservesHistoricalBucketRates(t *testing.T) {
	ctx := context.Background()
	_, repo := openTestDB(t)
	const modelID = "test-model"
	base := map[string]model.ModelPrice{modelID: {
		Prompt: 1, Completion: 2, Cache: .1, CacheRead: .1, CacheCreation: 1.25,
		PromptConfigured: true, CompletionConfigured: true, CacheReadConfigured: true, CacheCreationConfigured: true,
	}}
	if err := repo.ReplaceAll(ctx, base); err != nil {
		t.Fatalf("save base: %v", err)
	}
	const effective = int64(2_000_000)
	newPrice := base[modelID]
	newPrice.Prompt, newPrice.Completion, newPrice.EffectiveFromMS = 2, 4, effective
	if err := repo.ReplaceAll(ctx, map[string]model.ModelPrice{modelID: newPrice}); err != nil {
		t.Fatalf("save next rate: %v", err)
	}
	if err := repo.ReplaceAll(ctx, map[string]model.ModelPrice{modelID: newPrice}); err != nil {
		t.Fatalf("repeat identical save: %v", err)
	}
	book, err := repo.LoadAll(ctx)
	if err != nil {
		t.Fatalf("load prices: %v", err)
	}
	price := book[modelID]
	if len(price.History) != 2 {
		t.Fatalf("history count = %d, want base and new effective rate: %#v", len(price.History), price.History)
	}
	if price.History[0].EffectiveFromMS != 0 || price.History[0].Price.Prompt != 1 {
		t.Errorf("base history lost = %#v", price.History[0])
	}
	if price.History[1].EffectiveFromMS != effective || price.History[1].Price.Prompt != 2 {
		t.Errorf("new history not saved = %#v", price.History[1])
	}
	tokens := pricing.ModelTokens{InputTokens: 1_000_000, OutputTokens: 0}
	oldCost := pricing.CostForModelCandidatesWithServiceTierAtTime([]string{modelID}, "", tokens, book, effective-1)
	newCost := pricing.CostForModelCandidatesWithServiceTierAtTime([]string{modelID}, "", tokens, book, effective+1)
	if math.Abs(oldCost-1) > 1e-9 || math.Abs(newCost-2) > 1e-9 {
		t.Fatalf("historical/new cost = %v/%v, want 1/2", oldCost, newCost)
	}
}

func TestFirstRateDoesNotPriceEarlierUnknownUsage(t *testing.T) {
	ctx := context.Background()
	_, repo := openTestDB(t)
	if err := repo.ReplaceAll(ctx, map[string]model.ModelPrice{"new-model": {
		Prompt: 10, Completion: 10, EffectiveFromMS: 5_000,
	}}); err != nil {
		t.Fatal(err)
	}
	book, err := repo.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tokens := pricing.ModelTokens{InputTokens: 1_000_000}
	if got := pricing.CostForModelCandidatesWithServiceTierAtTime([]string{"new-model"}, "", tokens, book, 4_000); got != 0 {
		t.Fatalf("earlier unpriced usage = %v, want zero", got)
	}
	if got := pricing.CostForModelCandidatesWithServiceTierAtTime([]string{"new-model"}, "", tokens, book, 5_000); got != 10 {
		t.Fatalf("effective usage = %v, want 10", got)
	}
}

// this helper assertion is folded into TestPriceHistoryPreservesHistoricalBucketRates.
