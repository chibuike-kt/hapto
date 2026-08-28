import { View, StyleSheet, type ViewProps } from 'react-native';

import { colors } from '@/theme/colors';
import { radius, spacing } from '@/theme/spacing';

export type CardProps = ViewProps;

/**
 * Separates content from the background via elevation/shadow, not a
 * border — per hapto's visual rules, borders are near-invisible and used
 * sparingly, cards get their shape from shadow instead.
 */
export function Card({ style, ...rest }: CardProps) {
  return <View style={[styles.card, style]} {...rest} />;
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: colors.surface,
    borderRadius: radius.lg,
    padding: spacing.md,
    shadowColor: '#000000',
    shadowOffset: { width: 0, height: 4 },
    shadowOpacity: 0.3,
    shadowRadius: 12,
    elevation: 4,
  },
});
