import * as SplashScreen from 'expo-splash-screen';
import { useState } from 'react';
import { StyleSheet, View } from 'react-native';
import Animated, { Easing, Keyframe } from 'react-native-reanimated';
import { scheduleOnRN } from 'react-native-worklets';

import { Text } from '@/components/ui/text';
import { colors } from '@/theme/colors';

const DURATION = 600;

export function AnimatedSplashOverlay() {
  const [animate, setAnimate] = useState(false);
  const [visible, setVisible] = useState(true);

  if (!visible) return null;

  // Fast, understated, no bounce — a fade only, matching hapto's motion
  // rules for transitions generally.
  const splashKeyframe = new Keyframe({
    0: {
      opacity: 1,
    },
    20: {
      opacity: 1,
    },
    100: {
      opacity: 0,
      easing: Easing.out(Easing.cubic),
    },
  });

  // UnifrakturMaguntia is display-only: the app name/logo and the splash
  // screen, nowhere else. This is that splash use.
  const wordmark = (
    <Text variant="display" color={colors.accent}>
      hapto
    </Text>
  );

  return animate ? (
    <Animated.View
      entering={splashKeyframe.duration(DURATION).withCallback((finished) => {
        'worklet';
        if (finished) {
          scheduleOnRN(setVisible, false);
        }
      })}
      style={styles.splashOverlay}>
      {wordmark}
    </Animated.View>
  ) : (
    <View
      onLayout={() => {
        SplashScreen.hideAsync().finally(() => {
          setAnimate(true);
        });
      }}
      style={styles.splashOverlay}>
      {wordmark}
    </View>
  );
}

const styles = StyleSheet.create({
  splashOverlay: {
    ...StyleSheet.absoluteFill,
    backgroundColor: colors.background,
    alignItems: 'center',
    justifyContent: 'center',
    zIndex: 1000,
  },
});
