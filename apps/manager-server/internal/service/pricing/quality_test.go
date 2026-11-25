package pricing

import (
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"testing"
)

func TestPricingQualityDistinguishesHistoricalEvidence(t *testing.T) {
	rate := model.ModelPrice{Prompt: 2, PromptConfigured: true, CompletionConfigured: true}
	dated := rate
	dated.History = []model.ModelPriceVersion{{EffectiveFromMS: 3600000, Price: rate}}
	baseline := rate
	baseline.History = []model.ModelPriceVersion{{Price: rate}}
	estimated := rate
	estimated.Estimated = true
	estimated.History = []model.ModelPriceVersion{{EffectiveFromMS: 3600000, Price: estimated}}
	free := rate
	free.Prompt = 0
	free.History = []model.ModelPriceVersion{{EffectiveFromMS: 3600000, Price: free}}
	for _, tc := range []struct {
		name   string
		price  model.ModelPrice
		tokens ModelTokens
		time   int64
		status string
		amount float64
	}{
		{"dated", dated, ModelTokens{InputTokens: 1000000}, 3600000, "known", 2},
		{"before_first", dated, ModelTokens{InputTokens: 1000000}, 1, "unpriced", 0},
		{"legacy_baseline", baseline, ModelTokens{InputTokens: 1000000}, 1, "estimated", 2},
		{"undated", rate, ModelTokens{InputTokens: 1000000}, 3600000, "estimated", 2},
		{"explicit_estimate", estimated, ModelTokens{InputTokens: 1000000}, 3600000, "estimated", 2},
		{"missing_details", dated, ModelTokens{TotalTokens: 1000000}, 3600000, "unpriced", 0},
		{"mixed_missing_details", dated, ModelTokens{InputTokens: 1000000, TotalTokens: 2000000}, 3600000, "unpriced", 2},
		{"explicit_free", free, ModelTokens{InputTokens: 1000000}, 3600000, "known", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := QuoteForModelCandidatesAtTime([]string{"m"}, "", tc.tokens, map[string]model.ModelPrice{"m": tc.price}, tc.time)
			if q.Status != tc.status || q.Amount != tc.amount {
				t.Fatalf("quote=%#v want %s/%v", q, tc.status, tc.amount)
			}
		})
	}
	q := QuoteForModelCandidatesAtTime([]string{"missing"}, "", ModelTokens{InputTokens: 1000000}, nil, 3600000)
	if q.Status != "unpriced" {
		t.Fatalf("missing model %#v", q)
	}
	var c Coverage
	c.Add(Quote{Status: "known"}, 1)
	c.Add(Quote{Status: "estimated", Reasons: []string{"undated_rate"}}, 2)
	c.Merge(Coverage{UnpricedCalls: 3, Reasons: []string{"missing_rate"}})
	if c.KnownCalls != 1 || c.EstimatedCalls != 2 || c.UnpricedCalls != 3 || len(c.Reasons) != 2 {
		t.Fatalf("coverage=%#v", c)
	}
}
