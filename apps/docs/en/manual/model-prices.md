---
title: Model Prices And Cost Estimation
description: Configure model prices, service tiers, long-context multipliers, and cache read/write/creation billing for local CPAMP cost estimates.
---

# Model Prices And Cost Estimation

Model Prices maintains CPAMP's local cost-estimation rules. It affects Dashboard, Monitoring, and Usage Analytics; it does not change provider billing or CPA routing.

Open the [Model Prices Demo](https://seakee.github.io/CPA-Manager-Plus/#/demo/model-prices) to inspect fictional prices and model usage.

## Price Sources

- Public metadata synchronized from models.dev first, with LiteLLM and OpenRouter used as fallbacks when the preferred source is unavailable or lacks a model.
- Local prices added or overridden by the user.
- Entries for aliases, internal names, or provider-specific variants.

Synchronization only occurs when the user triggers it and may use the current Manager Server proxy configuration.

Automatic matching runs strictly in models.dev, LiteLLM, then OpenRouter order. CPAMP uses the canonical model metadata in the models.dev catalog to prefer the first-party official entry. A source is saved automatically only when it has one clear, strong identity match; fuzzy similarities are never auto-confirmed. An ambiguous source falls through to the next source. If none of the three sources yields a unique match, the confirmation list keeps candidates from each source separately, even when they share the same original model ID.

The current sync maps models.dev `cost.input`, `cost.output`, `cost.cache_read`, and `cost.cache_write`, converts valid `cost.tiers` context tiers into CPAMP billing rules, and maps `experimental.modes.fast.cost` to short-context Fast/Priority prices. The complete model object remains available in raw metadata; reasoning prices, unknown experimental modes, unknown tier types, and rules that cannot be validated safely do not activate automatic billing.

### Sync failures and last-known-good prices

- When models.dev is temporarily unavailable, CPAMP continues with LiteLLM and OpenRouter.
- A transient models.dev failure cannot automatically replace a stored models.dev price with a lower-priority source; fallback sources may still fill models that have no local price.
- When models.dev responds successfully but has no official entry or remains ambiguous, fallback sources are tried in order; only a unique strong identity match may replace the model.
- If every source fails, synchronization stops before any database write and existing prices remain unchanged.
- A manually saved price remains authoritative during synchronization, even when a source returns an exact match for the same model. Delete the manual entry and sync again to use synchronized pricing.
- A synchronized price remains the last-known-good value until a later successful sync or a manual edit; `syncedAtMs` indicates its freshness.

## Supported Billing Semantics

A price rule may include:

- Input and output tokens.
- Reasoning tokens.
- Cache read, cache write, and cache creation.
- Fixed per-request cost.
- `service_tier` differences.
- models.dev context-price tiers.
- Long-context thresholds and multipliers.
- Model alias and billing-model mapping.

Models such as GPT-5.6 may vary by context length, service tier, and cache type. CPAMP can only apply a rule when both the request event and price entry contain the required fields.

### Context-tier semantics

- A tier matches only when normalized input tokens are **strictly greater than** `tier.size`; an exact-threshold request stays in the lower band.
- When multiple tiers match, CPAMP selects the highest matching threshold.
- The selected tier's rates apply to the entire request, not only to tokens above the threshold.
- Input, output, or cache rates omitted by a tier inherit the base price; an explicit zero from models.dev remains zero.
- CPAMP currently activates only safely validated tiers with `tier.type = context` and a positive threshold. Other rules remain in raw metadata for inspection.

### Fast/Priority semantics

- `experimental.modes.fast.cost` matches both `fast` usage telemetry and API `priority` telemetry.
- Short-context requests prefer explicit Fast/Priority prices. Missing fields inherit base rates, while explicit zeros remain zero.
- A matched context tier or the legacy GPT long-context rule uses its standard context price without stacking Fast/Priority pricing.
- Non-models.dev entries, older data, and models without an explicit mode price retain the existing multiplier as a compatibility fallback.

The manual editor supports base prices, context tiers, and service-tier prices. Editing a rate preserves its advanced rules unless you explicitly change them. Expand price history to select an earlier version for correction.

## Matching Model Names

The client model, CPA alias, provider model, and price-table name may differ. When cost is missing:

1. Inspect the event model and billing model in [Monitoring](./monitoring.md).
2. Search for matching entries in Model Prices.
3. Add a local alias or override when required.
4. Refresh [Usage Analytics](./usage-analytics.md).

## Usage Summary

The page uses a compact model-usage summary to show which prices are active. It does not download full request history just to count model calls.

## Accuracy Boundaries

- Provider billing remains authoritative.
- Missing token, service tier, long-context, or cache fields reduce estimate accuracy.
- Subscriptions, grants, non-context tiers, unsupported dynamic-mode prices, and multiple currencies may not fit a single price entry.
- Historical amounts are derived from retained usage and effective-dated rates. Deliberate historical corrections can change those amounts; they are not immutable invoice snapshots.

## Historical Accounting Contract

Historical pricing and coverage metadata require Manager Server; browser-only local price storage does not provide this ledger.

CPAMP reports **API-equivalent value**: what the retained usage would cost under the selected API rates. It does not allocate subscriptions, credits, or actual provider invoices.

- Forward price changes start at the next whole UTC hour by default. Usage before that boundary retains its prior rate; delayed imports use the usage timestamp, not import time.
- Rates take effect at a whole UTC hour, inclusively, and end at the next version, exclusively. A midnight or timezone display change does not change the UTC boundary. Arbitrary minute boundaries and scheduling beyond the next UTC hour are unsupported.
- Lifetime account history and quota windows retain hourly pricing segments, including context/cache/service-tier dimensions. Segments are priced first, then summed. Restarting, rebuilding, or archiving raw events must preserve those segments and totals.
- **Correct historical pricing** explicitly acknowledges a change to past costs. An earlier insertion or replacement affects only its interval; later rates remain intact. The API requires `historicalCorrection: true` to insert before the latest rate or overwrite an existing past version. A normal replacement request cannot silently do this.
- Synchronization uses current catalogs. It does not reconstruct a provider's historical tariffs. Collect the published historical rate and effective date, select the model and UTC boundary, enable historical correction, and save. Repeat for each historical interval. Use **This rate is an estimate** when the evidence is incomplete.
- An undated legacy baseline is always an estimate, including after an upgrade. It preserves the previous numerical estimate without pretending to know the historical tariff. A model first saved with an explicit date has no rate before that date.
- **Estimate** means at least some calls use an undated/estimated rate, compatibility multipliers/cache rates, or missing token evidence. **Partial cost** means calls lack a matching rate or necessary token details; the displayed subtotal excludes unpriced portions. Explicitly configured free rates remain valid zero costs.
- Summary, dashboard, lifetime-account and quota-window API responses expose `pricing_coverage` with `known_calls`, `estimated_calls`, `unpriced_calls`, and reason codes. Counts are conservative per retained pricing segment: if its token totals do not reconcile, all calls in that segment are flagged. "Known" means covered by the saved dated rule and available telemetry, not independently audited provider billing. Monitoring, Dashboard and lifetime account totals display uncertainty labels.

Example: save $1/M input from January 1 and $2/M from February 1. A January request imported in March uses $1/M. Inserting $0.80/M from January 15 with historical correction changes January 15–31 only; February stays $2/M.

### Historical correction API

`PUT /v0/management/model-prices` replaces the active model map, so preserve unrelated entries from `GET` and change only the target. Do not submit response `history` as an import mechanism; the server owns history. For the target rate, send the complete intended rule (including context/service tiers), `effectiveFromMs` (UTC milliseconds), and `historicalCorrection: true`. `estimated: true` marks a supplied rate as uncertain. Unknown historical rates should remain unpriced instead of being filled silently with today's rate. An explicitly corrected rate before a retired model’s final version does not reactivate it; deletion is a forward retirement, not a history purge.

### Upgrade, archive and rollback

The upgrade changes the lifetime pricing aggregate key to include the UTC hour and invalidates the previous pricing revision. Rebuilds run in bounded background batches; reads recover from retained events while the rebuild catches up. Storage grows with active account/model/hour combinations rather than just account/model combinations.

Archived raw events can rebuild from the verified retained monitoring projection. If that projection is incomplete, the migration refuses to replace the old lifetime pricing table. Restore the missing history from a backup before retrying; aggregate totals alone cannot reconstruct lost time/token dimensions. Raw archive deletion is gated on the new pricing revision and completed coverage.

Back up the SQLite database and archive directory before upgrading. A binary downgrade is not a schema rollback; restore the matching database backup as well. Historical corrections also require a backup or an explicit compensating correction to undo.
