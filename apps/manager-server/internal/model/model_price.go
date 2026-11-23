package model

type ModelPrice struct {
	Retired                 bool                    `json:"retired,omitempty"`
	Prompt                  float64                 `json:"prompt"`
	Completion              float64                 `json:"completion"`
	Cache                   float64                 `json:"cache"`
	CacheRead               float64                 `json:"cacheRead,omitempty"`
	CacheCreation           float64                 `json:"cacheCreation,omitempty"`
	PromptConfigured        bool                    `json:"promptConfigured,omitempty"`
	CompletionConfigured    bool                    `json:"completionConfigured,omitempty"`
	CacheReadConfigured     bool                    `json:"cacheReadConfigured,omitempty"`
	CacheCreationConfigured bool                    `json:"cacheCreationConfigured,omitempty"`
	Source                  string                  `json:"source,omitempty"`
	SourceModelID           string                  `json:"sourceModelId,omitempty"`
	RawJSON                 string                  `json:"rawJson,omitempty"`
	ContextTiers            []ModelPriceContextTier `json:"contextTiers,omitempty"`
	ServiceTiers            []ModelPriceServiceTier `json:"serviceTiers,omitempty"`
	UpdatedAtMS             int64                   `json:"updatedAtMs,omitempty"`
	SyncedAtMS              *int64                  `json:"syncedAtMs,omitempty"`
	EffectiveFromMS         int64                   `json:"effectiveFromMs,omitempty"`
	History                 []ModelPriceVersion     `json:"history,omitempty"`
}

type ModelPriceSyncResult struct {
	Imported  int      `json:"imported"`
	Skipped   int      `json:"skipped"`
	Preserved []string `json:"preserved,omitempty"`
}

type ModelUsageStat struct {
	Model          string `json:"model"`
	Calls          int64  `json:"calls"`
	RequestedCalls int64  `json:"requested_calls"`
	ResolvedCalls  int64  `json:"resolved_calls"`
}

type ModelUsageSummary struct {
	SampledEvents int64            `json:"sampled_events"`
	TotalEvents   int64            `json:"total_events"`
	Truncated     bool             `json:"truncated"`
	Models        []ModelUsageStat `json:"models"`
}

// ModelPriceVersion is an immutable price rule effective from a UTC timestamp.
type ModelPriceVersion struct {
	Retired         bool       `json:"retired,omitempty"`
	EffectiveFromMS int64      `json:"effectiveFromMs"`
	Price           ModelPrice `json:"price"`
}
