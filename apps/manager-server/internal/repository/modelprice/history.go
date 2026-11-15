package modelprice

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

// spliceHistory records only billing changes; metadata refreshes don't create versions.
func spliceHistory(ctx context.Context, tx *sql.Tx, before, after map[string]model.ModelPrice, now int64) error {
	for modelID, existing := range before {
		snapshot := existing
		snapshot.History = nil
		snapshot.EffectiveFromMS = 0
		snapshot.UpdatedAtMS = 0
		snapshot.SyncedAtMS = nil
		if err := insertHistory(ctx, tx, modelID, 0, snapshot); err != nil {
			return err
		}
	}
	for modelID := range before {
		if _, ok := after[modelID]; !ok {
			// An empty snapshot marks this model unpriced from this time. loadHistory
			// still returns older versions so historical requests remain priced.
			if err := insertHistory(ctx, tx, modelID, now, model.ModelPrice{}); err != nil {
				return err
			}
		}
	}

	for modelID, price := range after {
		effectiveFrom := now
		existing, ok := before[modelID]
		if price.EffectiveFromMS > 0 {
			effectiveFrom = price.EffectiveFromMS
		} else if !ok {
			// The first entry preserves old estimates; subsequent changes start now.
			effectiveFrom = 0
		}
		snapshot := price
		snapshot.History = nil
		snapshot.EffectiveFromMS = 0
		snapshot.UpdatedAtMS = now
		if ok && sameBillingRules(existing, snapshot) {
			continue
		}
		if err := insertHistory(ctx, tx, modelID, effectiveFrom, snapshot); err != nil {
			return err
		}
	}
	return nil
}

func sameBillingRules(left, right model.ModelPrice) bool {
	left.History, right.History = nil, nil
	left.Source, right.Source = "", ""
	left.SourceModelID, right.SourceModelID = "", ""
	left.RawJSON, right.RawJSON = "", ""
	left.UpdatedAtMS, right.UpdatedAtMS = 0, 0
	left.SyncedAtMS, right.SyncedAtMS = nil, nil
	left.EffectiveFromMS, right.EffectiveFromMS = 0, 0
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func insertHistory(ctx context.Context, tx *sql.Tx, modelID string, effectiveFrom int64, price model.ModelPrice) error {
	if effectiveFrom < 0 {
		return fmt.Errorf("model price effective date cannot be negative")
	}
	payload, err := json.Marshal(price)
	if err != nil {
		return fmt.Errorf("encode model price history %s: %w", modelID, err)
	}
	_, err = tx.ExecContext(ctx, `insert into model_price_history (model, effective_from_ms, price_json)
		values (?, ?, ?) on conflict(model, effective_from_ms) do nothing`, modelID, effectiveFrom, string(payload))
	return err
}

func loadHistory(ctx context.Context, tx *sql.Tx, prices map[string]model.ModelPrice) error {
	rows, err := tx.QueryContext(ctx, `select model, effective_from_ms, price_json from model_price_history order by model, effective_from_ms`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var modelID string
		var effectiveFrom int64
		var payload string
		if err := rows.Scan(&modelID, &effectiveFrom, &payload); err != nil {
			return err
		}
		price := prices[modelID]
		// A deleted rule is still present here only as dated history. Its
		// most recent empty snapshot stops its use for new usage.

		var version model.ModelPriceVersion
		if err := json.Unmarshal([]byte(payload), &version.Price); err != nil {
			return fmt.Errorf("decode model price history %s at %d: %w", modelID, effectiveFrom, err)
		}
		version.EffectiveFromMS = effectiveFrom
		price.History = append(price.History, version)
		prices[modelID] = price
	}
	return rows.Err()
}
