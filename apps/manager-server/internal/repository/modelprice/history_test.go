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
	const effective = int64(7_200_000)
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
		Prompt: 10, Completion: 10, EffectiveFromMS: 7_200_000,
	}}); err != nil {
		t.Fatal(err)
	}
	book, err := repo.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tokens := pricing.ModelTokens{InputTokens: 1_000_000}
	if got := pricing.CostForModelCandidatesWithServiceTierAtTime([]string{"new-model"}, "", tokens, book, 3_600_000); got != 0 {
		t.Fatalf("earlier unpriced usage = %v, want zero", got)
	}
	if got := pricing.CostForModelCandidatesWithServiceTierAtTime([]string{"new-model"}, "", tokens, book, 7_200_000); got != 10 {
		t.Fatalf("effective usage = %v, want 10", got)
	}
}

func TestExplicitFirstDateSurvivesUnrelatedSave(t *testing.T) {
	ctx := context.Background()
	_, repo := openTestDB(t)
	book := map[string]model.ModelPrice{"m": {Prompt: 10, EffectiveFromMS: 7200000}}
	if err := repo.ReplaceAll(ctx, book); err != nil {
		t.Fatal(err)
	}
	book["other"] = model.ModelPrice{Prompt: 1}
	if err := repo.ReplaceAll(ctx, book); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := pricing.CostForModelCandidatesWithServiceTierAtTime([]string{"m"}, "", pricing.ModelTokens{InputTokens: 1000000}, loaded, 3600000)
	if got != 0 {
		t.Fatalf("unrelated save rewrote earlier cost: %v", got)
	}
}
func TestSameEffectiveDateCorrectionReplacesRate(t *testing.T) {
	ctx := context.Background()
	_, repo := openTestDB(t)
	for _, rate := range []float64{100, 2} {
		if err := repo.ReplaceAll(ctx, map[string]model.ModelPrice{"m": {Prompt: rate, EffectiveFromMS: 7200000}}); err != nil {
			t.Fatal(err)
		}
	}
	book, err := repo.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := pricing.CostForModelCandidatesWithServiceTierAtTime([]string{"m"}, "", pricing.ModelTokens{InputTokens: 1000000}, book, 7200000)
	if got != 2 {
		t.Fatalf("corrected cost: %v, want 2", got)
	}
}

func TestDeletedPricesStayOutOfPublicBookAndDoNotGrowHistory(t *testing.T) {
	ctx := context.Background()
	db, repo := openTestDB(t)
	if err := repo.ReplaceAll(ctx, map[string]model.ModelPrice{"m": {Prompt: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceAll(ctx, map[string]model.ModelPrice{}); err != nil {
		t.Fatal(err)
	}
	book, err := repo.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(book) != 0 {
		t.Fatalf("deleted model reappeared: %#v", book)
	}
	var before, after int
	if err := db.QueryRowContext(ctx, "select count(*) from model_price_history where model = 'm'").Scan(&before); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := repo.ReplaceAll(ctx, map[string]model.ModelPrice{"other": {Prompt: 1}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRowContext(ctx, "select count(*) from model_price_history where model = 'm'").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("deleted history grew: %d -> %d", before, after)
	}
	pricingBook, err := repo.LoadPricingBook(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := pricing.CostForModelCandidatesWithServiceTierAtTime([]string{"m"}, "", pricing.ModelTokens{InputTokens: 1000000}, pricingBook, 1); got != 2 {
		t.Fatalf("lost deleted model history: %v", got)
	}
}
func TestRetiredResolvedPriceFallsBackToAlias(t *testing.T) {
	ctx := context.Background()
	_, repo := openTestDB(t)
	if err := repo.ReplaceAll(ctx, map[string]model.ModelPrice{"alias": {Prompt: 1}, "resolved": {Prompt: 5, EffectiveFromMS: 7200000}}); err != nil {
		t.Fatal(err)
	}
	book, err := repo.LoadPricingBook(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tokens := pricing.ModelTokens{InputTokens: 1000000, PricingModel: "resolved"}
	if got := pricing.CostForModelCandidatesWithServiceTierAtTime([]string{"resolved", "alias"}, "", tokens, book, 3600000); got != 1 {
		t.Fatalf("pre-first-rate alias fallback: %v", got)
	}
	if got := pricing.CostForModelCandidatesWithServiceTierAtTime([]string{"resolved", "alias"}, "", tokens, book, 7200000); got != 5 {
		t.Fatalf("resolved rate: %v", got)
	}
}
func TestReaddingModelDoesNotOverwriteOriginalPrice(t *testing.T) {
	ctx := context.Background()
	_, repo := openTestDB(t)
	for _, book := range []map[string]model.ModelPrice{{"m": {Prompt: 1}}, {}, {"m": {Prompt: 8}}} {
		if err := repo.ReplaceAll(ctx, book); err != nil {
			t.Fatal(err)
		}
	}
	book, err := repo.LoadPricingBook(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := pricing.CostForModelCandidatesWithServiceTierAtTime([]string{"m"}, "", pricing.ModelTokens{InputTokens: 1000000}, book, 1); got != 1 {
		t.Fatalf("re-add changed original cost: %v", got)
	}
}
func TestEarlierVersionCannotSilentlyReplaceCurrentPrice(t *testing.T) {
	ctx := context.Background()
	_, repo := openTestDB(t)
	if err := repo.ReplaceAll(ctx, map[string]model.ModelPrice{"m": {Prompt: 2, EffectiveFromMS: 7200000}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceAll(ctx, map[string]model.ModelPrice{"m": {Prompt: 3, EffectiveFromMS: 3600000}}); err == nil {
		t.Fatal("accepted insertion before latest version")
	}
	book, err := repo.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if book["m"].Prompt != 2 {
		t.Fatal("failed save changed current rate")
	}
}

func TestSyncAppendsInsteadOfRewritingHistory(t *testing.T) {
	ctx := context.Background()
	_, repo := openTestDB(t)
	for _, p := range []model.ModelPrice{{Prompt: 1, Source: "sync"}, {Prompt: 2, Source: "sync", EffectiveFromMS: 7200000}} {
		if err := repo.ReplaceAll(ctx, map[string]model.ModelPrice{"m": p}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.UpsertSynced(ctx, map[string]model.ModelPrice{"m": {Prompt: 3, Source: "sync"}}); err != nil {
		t.Fatal(err)
	}
	book, err := repo.LoadPricingBook(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := pricing.CostForModelCandidatesWithServiceTierAtTime([]string{"m"}, "", pricing.ModelTokens{InputTokens: 1000000}, book, 7200000); got != 2 {
		t.Fatalf("sync changed earlier cost: %v", got)
	}
}
func TestClientCannotRetireActivePrice(t *testing.T) {
	ctx := context.Background()
	_, repo := openTestDB(t)
	if err := repo.ReplaceAll(ctx, map[string]model.ModelPrice{"m": {Prompt: 1, Retired: true}}); err != nil {
		t.Fatal(err)
	}
	book, err := repo.LoadPricingBook(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := pricing.CostForModelCandidatesWithServiceTierAtTime([]string{"m"}, "", pricing.ModelTokens{InputTokens: 1000000}, book, 1); got != 1 {
		t.Fatalf("client retired active price: %v", got)
	}
}
