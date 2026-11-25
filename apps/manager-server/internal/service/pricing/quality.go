package pricing

import (
	"sort"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

// Quote describes API-equivalent value, never an invoice or subscription charge.
type Quote struct {
	ZeroTokenCalls int64
	Amount         float64
	Status         string
	Reasons        []string
}

// Coverage counts calls using dated rates, estimated evidence, or missing rates.
// Numeric totals exclude unpriced portions and must be displayed as partial.
type Coverage struct {
	KnownCalls     int64    `json:"known_calls"`
	EstimatedCalls int64    `json:"estimated_calls"`
	UnpricedCalls  int64    `json:"unpriced_calls"`
	Reasons        []string `json:"reasons,omitempty"`
}

func (c *Coverage) Add(q Quote, calls int64) {
	if q.Status != "unpriced" && q.ZeroTokenCalls > 0 {
		missing := min(q.ZeroTokenCalls, calls)
		c.UnpricedCalls += missing
		calls -= missing
		q.Reasons = append(q.Reasons, "missing_token_details")
	}
	switch q.Status {
	case "known":
		c.KnownCalls += calls
	case "estimated":
		c.EstimatedCalls += calls
	default:
		c.UnpricedCalls += calls
	}
	for _, reason := range q.Reasons {
		found := false
		for _, existing := range c.Reasons {
			if existing == reason {
				found = true
				break
			}
		}
		if !found {
			c.Reasons = append(c.Reasons, reason)
		}
	}
	sort.Strings(c.Reasons)
}
func (c *Coverage) Merge(other Coverage) {
	c.KnownCalls += other.KnownCalls
	c.Add(Quote{Status: "estimated", Reasons: other.Reasons}, other.EstimatedCalls)
	c.UnpricedCalls += other.UnpricedCalls
}
func quoteForPrice(name, tier string, tokens ModelTokens, catalog model.ModelPrice, timestamp int64) Quote {
	price := effectivePriceAtTime(catalog, timestamp)
	if price.Retired {
		return Quote{Status: "unpriced", Reasons: []string{"missing_rate"}}
	}
	if tokens.TotalTokens > 0 && tokens.InputTokens == 0 && tokens.OutputTokens == 0 && tokens.CachedTokens == 0 && tokens.CacheReadTokens == 0 && tokens.CacheCreationTokens == 0 {
		return Quote{Status: "unpriced", Reasons: []string{"missing_token_details"}}
	}
	q := Quote{ZeroTokenCalls: tokens.ZeroTokenCalls, Amount: costForHistoricalPrice(name, tier, tokens, price), Status: "known"}
	estimate := func(reason string) { q.Status = "estimated"; q.Reasons = append(q.Reasons, reason) }
	undated := len(catalog.History) == 0
	for i := len(catalog.History) - 1; i >= 0; i-- {
		if catalog.History[i].EffectiveFromMS <= timestamp {
			undated = catalog.History[i].EffectiveFromMS == 0
			break
		}
	}
	if undated {
		estimate("undated_rate")
	} else if price.Estimated {
		estimate("estimated_rate")
	}
	if tokens.InputTokens == 0 && tokens.OutputTokens == 0 && tokens.CachedTokens == 0 && tokens.CacheReadTokens == 0 && tokens.CacheCreationTokens == 0 {
		estimate("missing_token_details")
	}
	billingPrice := price
	bandTokens := tokens
	if tokens.ContextThresholdTokens > 0 {
		bandTokens.ContextThresholdTokens = -1
		for _, band := range price.ContextTiers {
			if band.ThresholdTokens <= tokens.ContextThresholdTokens && band.ThresholdTokens > bandTokens.ContextThresholdTokens {
				bandTokens.ContextThresholdTokens = band.ThresholdTokens
			}
		}
	}
	contextPrice, hasContext := activeContextPrice(bandTokens, price)
	if hasContext {
		billingPrice = contextPrice
	} else if tierPrice, ok := model.ModelPriceForServiceTier(price, tier); ok {
		billingPrice = tierPrice
	}
	if tokens.CachedTokens > 0 && billingPrice.Cache == 0 {
		estimate("cache_rate_fallback")
	}
	if tokens.CacheReadTokens > 0 && !configuredPriceValue(billingPrice.CacheRead, billingPrice.CacheReadConfigured) {
		estimate("cache_rate_fallback")
	}
	if tokens.CacheCreationTokens > 0 && !configuredPriceValue(billingPrice.CacheCreation, billingPrice.CacheCreationConfigured) {
		estimate("cache_rate_fallback")
	}
	normalizedTier := strings.ToLower(strings.TrimSpace(tier))
	if normalizedTier != "" && normalizedTier != "default" && normalizedTier != "standard" && normalizedTier != "auto" && !hasContext {
		if _, ok := model.ModelPriceForServiceTier(price, tier); !ok {
			estimate("service_tier_fallback")
		}
	}
	if len(price.ContextTiers) == 0 && supportsLongContextPremium(name) && tokens.LongInputTokens > 0 {
		estimate("context_rate_fallback")
	}
	if tokens.InputTokens-tokens.CachedTokens-tokens.CacheReadTokens-tokens.CacheCreationTokens > 0 && !configuredPriceValue(billingPrice.Prompt, billingPrice.PromptConfigured) || tokens.OutputTokens > 0 && !configuredPriceValue(billingPrice.Completion, billingPrice.CompletionConfigured) {
		q.Status = "unpriced"
		q.Reasons = append(q.Reasons, "missing_rate")
	}
	if tokens.TotalTokens > tokens.InputTokens+tokens.OutputTokens {
		q.Status = "unpriced"
		q.Reasons = append(q.Reasons, "missing_token_details")
	}
	return q
}
