# Effective-dated API-equivalent accounting

The user-facing contract and correction workflow are in [Model Prices](../../apps/docs/en/manual/model-prices.md#historical-accounting-contract).

## Invariants

1. Forward price changes preserve costs before the UTC-hour boundary.
2. Delayed events select the rate at event time, even when imported after multiple changes.
3. Explicit historical corrections affect only `[effectiveFromMs, next version)`; the current price map continues to show the latest version.
4. Account lifetime totals, quota windows, hourly analytics, dashboard, credentials and API-key summaries price hourly/billing/context/service segments before grouping.
5. Numeric amounts are derived, not immutable request charge snapshots. Retain usage dimensions and price history. Never present an unpriced subtotal as complete.
6. Undated baselines are estimated. Explicit dated rules may also be estimated. Free configured rates differ from missing rates. Coverage reports evidence quality, not invoice accuracy.

## Storage and migration

`model_price_history` stores ordered rules. Input `history` is not trusted for mutations. `historicalCorrection` is a write acknowledgement and is not persisted in snapshots. Deletion appends a forward retirement marker; older rates remain readable.

The account pricing key includes `bucket_ms`. Existing tables without that key are parked for bounded derived cleanup. The pricing structure revision includes `hourly-accounts-1`, forcing both pricing aggregates to rebuild with their shared watermark. No raw events or price history are rewritten. Rebuild recovery uses raw events plus a verified projection for genuinely archive-deleted events, with original event IDs and no duplicates. Missing retained events abort migration before replacing the account pricing table. Read recovery checks permanent core coverage before returning monetary totals. Archive deletion requires compatible pricing state at the target watermark.

Cost-only historical corrections need no token rebuild. New context thresholds extend the retained band partition and require a verified rebuild source; older thresholds remain so every historical rate can select the correct band. Hourly storage is proportional to active account/model/billing/service/context/hour combinations. Profile with a representative synthetic workload before large deployments.

Back up database and archive files before upgrading. Restore the matching backup for a binary rollback; older binaries may reject or attempt a different migration of the hourly key. Do not rely on a downgrade as rollback.

## Verification

Run `npm run type-check`, `npm run lint`, `npm test`, `npm run build`, and `cd apps/manager-server && go test -race ./...` as appropriate. Backend regressions cover raw/stored lifetime totals with a late tail; backdated insertion with later-rate preservation; price evidence/free/missing-token distinctions; and archive-deleted upgrade, bounded rebuild reads, restart, and refusal when projection evidence is missing.

The mock-only black-box harness in `tests/integration/verify_historical_pricing.py` requires Docker and a cached `python:3.13-alpine` image. Build a static Manager Server binary and run:

```sh
python3 tests/integration/verify_historical_pricing.py /absolute/path/to/cpa-manager-plus --report-dir /tmp/cpamp-pricing-report
```

It uses a fresh database, synthetic credentials and token usage, no published ports, and a container with networking disabled. It removes its container in `finally`; reports and synthetic SQLite data remain in the selected directory. It never calls an inference provider. Pass `--upgrade-from /absolute/path/to/previous-binary` to populate the database with an older binary before restarting with the new build. This verifies a real schema upgrade rather than just a clean installation.
