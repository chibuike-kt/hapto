/**
 * One stroke width, applied everywhere lucide-react-native is used, so
 * icons never look mismatched next to each other. Don't pass a different
 * strokeWidth at a call site — change it here if it ever needs to change.
 */
export const ICON_STROKE_WIDTH = 1.75;

export const iconSize = {
  sm: 16,
  md: 20,
  lg: 24,
  xl: 32,
} as const;

export type IconSizeToken = keyof typeof iconSize;
