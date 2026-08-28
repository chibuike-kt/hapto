import {
  SpaceGrotesk_400Regular,
  SpaceGrotesk_500Medium,
  SpaceGrotesk_700Bold,
  useFonts,
} from '@expo-google-fonts/space-grotesk';
import { UnifrakturMaguntia_400Regular } from '@expo-google-fonts/unifrakturmaguntia';

/**
 * Loads every font hapto's typography scale depends on. Callers must not
 * render real UI until this resolves — see src/app/_layout.tsx, which
 * keeps the splash screen up until [loaded] is true. Rendering before then
 * would flash the system font for a frame.
 */
export function useAppFonts() {
  return useFonts({
    SpaceGrotesk_400Regular,
    SpaceGrotesk_500Medium,
    SpaceGrotesk_700Bold,
    UnifrakturMaguntia_400Regular,
  });
}
