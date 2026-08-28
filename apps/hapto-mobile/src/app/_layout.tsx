import { DarkTheme, ThemeProvider } from 'expo-router';
import * as SplashScreen from 'expo-splash-screen';

import { AnimatedSplashOverlay } from '@/components/animated-icon';
import AppTabs from '@/components/app-tabs';
import { useAppFonts } from '@/hooks/use-app-fonts';

SplashScreen.preventAutoHideAsync();

export default function TabLayout() {
  const [fontsLoaded, fontError] = useAppFonts();

  // Block render behind the native splash screen until fonts are ready
  // (or have definitively failed) — never let the system font flash
  // before Space Grotesk / UnifrakturMaguntia load.
  if (!fontsLoaded && !fontError) {
    return null;
  }

  return (
    // hapto is dark-only for now — no light mode, no toggle.
    <ThemeProvider value={DarkTheme}>
      <AnimatedSplashOverlay />
      <AppTabs />
    </ThemeProvider>
  );
}
