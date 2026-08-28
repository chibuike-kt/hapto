import { useCallback, useEffect, useState } from 'react';
import { Easing, runOnJS, useAnimatedReaction, useSharedValue, withTiming } from 'react-native-reanimated';

import { colors } from '@/theme/colors';
import { typography, type TypographyVariantName } from '@/theme/typography';

import { Text } from './text';

export type AnimatedNumberProps = {
  /** The numeric value to settle to. */
  value: number;
  /** Formats the interpolated value each frame, e.g. as currency. */
  formatValue?: (value: number) => string;
  variant?: Extract<TypographyVariantName, 'balanceHero' | 'amountLarge'>;
  color?: string;
  /** Timing duration in ms. This is a timing animation, not a spring — no overshoot, it settles. */
  duration?: number;
};

const defaultFormat = (value: number) => value.toFixed(2);

/**
 * hapto's shared number-transition primitive: when `value` changes, the
 * displayed number eases to the new value over `duration` rather than
 * jumping instantly. Deliberately timing-based (Easing.out), never a
 * spring — numbers settle into place, they don't bounce past their target
 * and back.
 */
export function AnimatedNumber({
  value,
  formatValue = defaultFormat,
  variant = 'amountLarge',
  color,
  duration = 400,
}: AnimatedNumberProps) {
  const animatedValue = useSharedValue(value);
  const [displayText, setDisplayText] = useState(() => formatValue(value));

  useEffect(() => {
    animatedValue.value = withTiming(value, {
      duration,
      easing: Easing.out(Easing.cubic),
    });
  }, [value, duration, animatedValue]);

  // formatValue is an arbitrary, caller-supplied JS-thread function — it
  // must never be invoked from worklet code directly (that's what threw
  // "Tried to synchronously call a Remote Function" here before). This
  // runs on the JS thread only, called via runOnJS below.
  const commitDisplayText = useCallback(
    (current: number) => {
      setDisplayText(formatValue(current));
    },
    [formatValue],
  );

  useAnimatedReaction(
    () => animatedValue.value,
    (current) => {
      // current is a plain number crossing the UI-thread/JS-thread
      // boundary as data; commitDisplayText (and the formatValue call
      // inside it) runs on the JS thread via runOnJS, per Reanimated's
      // documented pattern for syncing a shared value to React state.
      runOnJS(commitDisplayText)(current);
    },
    [commitDisplayText],
  );

  const v = typography[variant];

  return (
    <Text
      variant={variant}
      color={color ?? colors.textPrimary}
      style={{
        fontVariant: v.fontVariant,
      }}>
      {displayText}
    </Text>
  );
}
