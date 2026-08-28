import { Check } from 'lucide-react-native';
import { useEffect } from 'react';
import { StyleSheet, View } from 'react-native';
import Animated, { Easing, useAnimatedStyle, useSharedValue, withDelay, withTiming } from 'react-native-reanimated';

import { colors } from '@/theme/colors';
import { ICON_STROKE_WIDTH } from '@/theme/icons';

const CIRCLE_DURATION = 420;
const CHECK_DELAY = 220;
const CHECK_DURATION = 320;
const TOTAL_DURATION = CHECK_DELAY + CHECK_DURATION;

export type SuccessAnimationProps = {
  size?: number;
  /** Fires once the full sequence has finished. */
  onComplete?: () => void;
};

/**
 * hapto's one shared "this payment completed" moment. A deliberate staged
 * reveal (circle settles in, then the checkmark) — not instant, and not a
 * bouncy spring. Every screen showing a completed payment should reuse
 * this rather than inventing its own confirmation animation.
 */
export function SuccessAnimation({ size = 96, onComplete }: SuccessAnimationProps) {
  const circleOpacity = useSharedValue(0);
  const circleScale = useSharedValue(0.8);
  const checkOpacity = useSharedValue(0);
  const checkScale = useSharedValue(0.6);

  useEffect(() => {
    circleOpacity.value = withTiming(1, { duration: 200, easing: Easing.out(Easing.quad) });
    circleScale.value = withTiming(1, { duration: CIRCLE_DURATION, easing: Easing.out(Easing.cubic) });
    checkOpacity.value = withDelay(CHECK_DELAY, withTiming(1, { duration: CHECK_DURATION, easing: Easing.out(Easing.quad) }));
    checkScale.value = withDelay(CHECK_DELAY, withTiming(1, { duration: CHECK_DURATION, easing: Easing.out(Easing.cubic) }));

    if (!onComplete) return;
    const timer = setTimeout(onComplete, TOTAL_DURATION);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const circleStyle = useAnimatedStyle(() => ({
    opacity: circleOpacity.value,
    transform: [{ scale: circleScale.value }],
  }));

  const checkStyle = useAnimatedStyle(() => ({
    opacity: checkOpacity.value,
    transform: [{ scale: checkScale.value }],
  }));

  return (
    <View style={[styles.container, { width: size, height: size }]}>
      <Animated.View
        style={[
          styles.circle,
          { width: size, height: size, borderRadius: size / 2, backgroundColor: colors.success },
          circleStyle,
        ]}
      />
      <Animated.View style={checkStyle}>
        <Check color={colors.background} size={size * 0.5} strokeWidth={ICON_STROKE_WIDTH} />
      </Animated.View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    alignItems: 'center',
    justifyContent: 'center',
  },
  circle: {
    position: 'absolute',
  },
});
