import { describe, expect, it } from 'vitest';
import type { TFunction } from 'i18next';
import { formatPricingValue, pricingQualityHint } from './pricingQuality';

const t = ((key: string, options?: { estimated?: number; unpriced?: number }) =>
  options ? `${options.estimated} estimated; ${options.unpriced} unpriced` : key) as TFunction;

describe('cost evidence labels', () => {
  it('distinguishes known free costs, estimates, and incomplete subtotals', () => {
    expect(formatPricingValue('$0.00', { known_calls: 1, estimated_calls: 0, unpriced_calls: 0 }, t)).toBe('$0.00');
    expect(formatPricingValue('$1.00', { known_calls: 0, estimated_calls: 1, unpriced_calls: 0 }, t)).toBe('$1.00 · pricing_quality.estimated');
    const mixed = { known_calls: 1, estimated_calls: 2, unpriced_calls: 3 };
    expect(formatPricingValue('$1.00', mixed, t)).toBe('$1.00 · pricing_quality.partial');
    expect(pricingQualityHint(mixed, t)).toBe('2 estimated; 3 unpriced');
  });
});
