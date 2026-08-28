import { Text as RNText, type TextProps as RNTextProps } from 'react-native';

import { colors } from '@/theme/colors';
import { typography, type TypographyVariantName } from '@/theme/typography';

export type TextProps = RNTextProps & {
  variant?: TypographyVariantName;
  color?: string;
};

/**
 * The only Text component screens should use. It's wired to the
 * typography scale so no screen ever hardcodes a fontSize or fontWeight by
 * hand — pick a variant, or extend the scale in src/theme/typography.ts if
 * a new one is genuinely needed.
 */
export function Text({ variant = 'body', color, style, ...rest }: TextProps) {
  const v = typography[variant];

  return (
    <RNText
      style={[
        {
          fontFamily: v.fontFamily,
          fontSize: v.fontSize,
          lineHeight: v.lineHeight,
          letterSpacing: v.letterSpacing,
          fontVariant: v.fontVariant,
          color: color ?? colors.textPrimary,
        },
        style,
      ]}
      {...rest}
    />
  );
}
