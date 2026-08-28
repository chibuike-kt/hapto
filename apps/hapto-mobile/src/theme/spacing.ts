/** hapto's spacing scale. Reach for these instead of raw numbers. */
export const spacing = {
  xs: 4,
  sm: 8,
  md: 16,
  lg: 24,
  xl: 32,
  xxl: 48,
  xxxl: 64,
} as const;

export type SpacingToken = keyof typeof spacing;

/** Shared corner radii, kept alongside spacing since they're used together. */
export const radius = {
  sm: 8,
  md: 12,
  lg: 16,
  pill: 999,
} as const;
