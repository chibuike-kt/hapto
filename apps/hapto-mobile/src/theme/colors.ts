/**
 * hapto's color tokens. Dark is the only mode for now — no light theme, no
 * toggle. Every screen should reference these tokens, never a raw hex.
 */
export const colors = {
  background: '#0A0A0B',
  surface: '#151517',

  /**
   * Primary action / brand accent. Picked at the minty end of the
   * "electric green" range (vs. a pure #00FF00) so it stays vivid without
   * vibrating against the near-black background.
   */
  accent: '#39FF88',

  /**
   * "Money coming in." Deliberately distinct from accent — a desaturated,
   * calmer green so a credit never visually competes with a primary
   * action button.
   */
  success: '#4CAF7D',
  credit: '#4CAF7D',

  /** "Money going out" / destructive states. Desaturated red. */
  danger: '#CC5257',
  debit: '#CC5257',

  textPrimary: '#F5F5F7',
  textSecondary: '#8A8A8E',

  /**
   * Near-invisible. Used sparingly — prefer elevation/shadow (see
   * Card) over borders for separating surfaces from the background.
   */
  border: '#232326',
} as const;

export type ColorToken = keyof typeof colors;
