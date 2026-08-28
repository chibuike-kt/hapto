import type { TextStyle } from 'react-native';

/**
 * Font family names as registered with useFonts (see
 * src/hooks/use-app-fonts.ts). These string keys are what React Native's
 * fontFamily style resolves against — they must match exactly.
 */
export const fontFamily = {
  regular: 'SpaceGrotesk_400Regular',
  medium: 'SpaceGrotesk_500Medium',
  bold: 'SpaceGrotesk_700Bold',
  /** Display-only. Never use for body/UI text. */
  display: 'UnifrakturMaguntia_400Regular',
} as const;

type TypographyVariant = {
  fontFamily: string;
  fontSize: number;
  lineHeight: number;
  letterSpacing?: number;
  fontVariant?: TextStyle['fontVariant'];
};

type TypographyVariantName = 'display' | 'balanceHero' | 'amountLarge' | 'heading' | 'body' | 'caption';

/**
 * hapto's type scale. Every screen should reach for one of these variants
 * via <Text variant="..."> rather than hardcoding a fontSize/fontWeight —
 * that's the whole point of the scale existing.
 *
 * Typed as Record<Name, TypographyVariant> rather than left to `satisfies`
 * inference: with `satisfies`, a variant that omits an optional field
 * (letterSpacing, fontVariant) loses that key entirely instead of having
 * it as `undefined`, so indexing by a variable name fails to type-check.
 */
export const typography: Record<TypographyVariantName, TypographyVariant> = {
  /** Logo / splash only. Never body or UI text. */
  display: {
    fontFamily: fontFamily.display,
    fontSize: 48,
    lineHeight: 58,
  },
  /** The primary balance number. Tabular figures so digits don't jitter. */
  balanceHero: {
    fontFamily: fontFamily.bold,
    fontSize: 52,
    lineHeight: 60,
    letterSpacing: -0.5,
    fontVariant: ['tabular-nums'],
  },
  /** Transaction amounts in detail views. Also tabular. */
  amountLarge: {
    fontFamily: fontFamily.bold,
    fontSize: 28,
    lineHeight: 34,
    fontVariant: ['tabular-nums'],
  },
  heading: {
    fontFamily: fontFamily.medium,
    fontSize: 18,
    lineHeight: 24,
  },
  body: {
    fontFamily: fontFamily.regular,
    fontSize: 16,
    lineHeight: 22,
  },
  caption: {
    fontFamily: fontFamily.regular,
    fontSize: 12,
    lineHeight: 16,
  },
};

export type { TypographyVariantName };
