import { colors } from '@/theme/colors';

import { Text, type TextProps } from './text';

export type AmountDirection = 'credit' | 'debit';

export type AmountDisplayProps = {
  /** Integer minor units, matching hapto-api's money convention exactly. */
  amountMinorUnits: number;
  currency?: string;
  direction: AmountDirection;
  variant?: Extract<TextProps['variant'], 'balanceHero' | 'amountLarge'>;
};

const CURRENCY_SYMBOLS: Record<string, string> = {
  USD: '$',
};

function formatAmount(amountMinorUnits: number, currency: string) {
  const symbol = CURRENCY_SYMBOLS[currency] ?? `${currency} `;
  const major = Math.abs(amountMinorUnits) / 100;
  return `${symbol}${major.toFixed(2)}`;
}

/**
 * The one place amount sign/color logic lives. A screen never decides for
 * itself whether a number should look green or red — it passes a
 * direction and this renders it consistently.
 */
export function AmountDisplay({
  amountMinorUnits,
  currency = 'USD',
  direction,
  variant = 'amountLarge',
}: AmountDisplayProps) {
  const isCredit = direction === 'credit';
  const sign = isCredit ? '+' : '−'; // real minus sign, not a hyphen
  const color = isCredit ? colors.credit : colors.debit;

  return (
    <Text variant={variant} color={color}>
      {sign}
      {formatAmount(amountMinorUnits, currency)}
    </Text>
  );
}
