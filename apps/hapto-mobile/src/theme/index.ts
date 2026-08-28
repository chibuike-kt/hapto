import { colors } from './colors';
import { iconSize, ICON_STROKE_WIDTH } from './icons';
import { radius, spacing } from './spacing';
import { fontFamily, typography } from './typography';

export * from './colors';
export * from './typography';
export * from './spacing';
export * from './icons';

export const theme = {
  colors,
  typography,
  fontFamily,
  spacing,
  radius,
  iconSize,
  iconStrokeWidth: ICON_STROKE_WIDTH,
} as const;
