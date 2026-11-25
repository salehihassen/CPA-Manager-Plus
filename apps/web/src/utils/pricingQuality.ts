import type { TFunction } from 'i18next';

export interface PricingCoverage {
  known_calls: number;
  estimated_calls: number;
  unpriced_calls: number;
  reasons?: string[];
}

export function pricingQualityLabel(coverage: PricingCoverage | undefined, t: TFunction): string {
  if (!coverage) return '';
  if (coverage.unpriced_calls > 0) return t('pricing_quality.partial');
  if (coverage.estimated_calls > 0) return t('pricing_quality.estimated');
  return '';
}

export function formatPricingValue(value: string, coverage: PricingCoverage | undefined, t: TFunction): string {
  const label = pricingQualityLabel(coverage, t);
  return label ? `${value} · ${label}` : value;
}

export function pricingQualityHint(coverage: PricingCoverage | undefined, t: TFunction): string {
  return coverage && (coverage.estimated_calls > 0 || coverage.unpriced_calls > 0)
    ? t('pricing_quality.coverage', { estimated: coverage.estimated_calls, unpriced: coverage.unpriced_calls })
    : '';
}
