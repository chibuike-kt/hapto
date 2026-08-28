import {
  ArrowDownLeft,
  ArrowUpRight,
  Bluetooth,
  QrCode,
  Send,
  Settings,
  ShieldCheck,
  Wallet,
} from 'lucide-react-native';
import { useState } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { AmountDisplay } from '@/components/ui/amount-display';
import { AnimatedNumber } from '@/components/ui/animated-number';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { SuccessAnimation } from '@/components/ui/success-animation';
import { Text } from '@/components/ui/text';
import { colors, type ColorToken } from '@/theme/colors';
import { ICON_STROKE_WIDTH, iconSize } from '@/theme/icons';
import { spacing } from '@/theme/spacing';
import { typography, type TypographyVariantName } from '@/theme/typography';

/**
 * hapto's style guide: every color token, type variant, icon, and starter
 * component in one place, so the whole visual system can be reviewed once
 * here rather than judged screen by screen later. No business logic — this
 * screen is the design system's deliverable, not a real app screen.
 */
export default function StyleGuideScreen() {
  return (
    <SafeAreaView style={styles.safeArea} edges={['top', 'bottom']}>
      <ScrollView contentContainerStyle={styles.content} showsVerticalScrollIndicator={false}>
        <View style={styles.header}>
          <Text variant="display" color={colors.accent}>
            hapto
          </Text>
          <Text variant="caption" color={colors.textSecondary}>
            design system — v1
          </Text>
        </View>

        <Section title="Colors">
          <View style={styles.swatchGrid}>
            {(Object.keys(colors) as ColorToken[]).map((token) => (
              <ColorSwatch key={token} token={token} />
            ))}
          </View>
        </Section>

        <Section title="Typography">
          <Card style={styles.stack}>
            {(Object.keys(typography) as TypographyVariantName[])
              .filter((name) => name !== 'display')
              .map((name) => (
                <TypeSample key={name} name={name} />
              ))}
          </Card>
        </Section>

        <Section title="Icons">
          <Card style={styles.iconRow}>
            <IconSample icon={Wallet} label="Wallet" />
            <IconSample icon={Send} label="Send" />
            <IconSample icon={ArrowDownLeft} label="Credit" />
            <IconSample icon={ArrowUpRight} label="Debit" />
            <IconSample icon={Bluetooth} label="BLE" />
            <IconSample icon={QrCode} label="Scan" />
            <IconSample icon={ShieldCheck} label="Trust" />
            <IconSample icon={Settings} label="Settings" />
          </Card>
          <Text variant="caption" color={colors.textSecondary} style={styles.hint}>
            lucide-react-native, outline style, {ICON_STROKE_WIDTH}px stroke everywhere.
          </Text>
        </Section>

        <Section title="Buttons">
          <View style={styles.stack}>
            <Button label="Primary action" variant="primary" onPress={() => {}} />
            <Button label="Secondary action" variant="secondary" onPress={() => {}} />
            <Button label="Ghost action" variant="ghost" onPress={() => {}} />
            <Button label="Disabled" variant="primary" disabled onPress={() => {}} />
          </View>
        </Section>

        <Section title="Amounts">
          <Card style={styles.stack}>
            <Text variant="caption" color={colors.textSecondary}>
              Balance
            </Text>
            <AmountDisplay amountMinorUnits={128430} direction="credit" variant="balanceHero" />
            <View style={styles.divider} />
            <AmountRow label="Coffee with a friend" amountMinorUnits={850} direction="debit" />
            <AmountRow label="Payment received" amountMinorUnits={4200} direction="credit" />
            <AmountRow label="Split dinner" amountMinorUnits={2375} direction="debit" />
          </Card>
        </Section>

        <Section title="Motion">
          <MotionDemo />
        </Section>
      </ScrollView>
    </SafeAreaView>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <View style={styles.section}>
      <Text variant="heading" style={styles.sectionTitle}>
        {title}
      </Text>
      {children}
    </View>
  );
}

function ColorSwatch({ token }: { token: ColorToken }) {
  return (
    <View style={styles.swatchItem}>
      <View style={[styles.swatch, { backgroundColor: colors[token] }]} />
      <Text variant="caption">{token}</Text>
      <Text variant="caption" color={colors.textSecondary}>
        {colors[token]}
      </Text>
    </View>
  );
}

function TypeSample({ name }: { name: TypographyVariantName }) {
  return (
    <View>
      <Text variant={name}>
        {name === 'balanceHero' || name === 'amountLarge' ? '$1,234.56' : 'The quick brown fox'}
      </Text>
      <Text variant="caption" color={colors.textSecondary}>
        {name}
      </Text>
    </View>
  );
}

function IconSample({ icon: Icon, label }: { icon: typeof Wallet; label: string }) {
  return (
    <View style={styles.iconItem}>
      <Icon color={colors.textPrimary} size={iconSize.lg} strokeWidth={ICON_STROKE_WIDTH} />
      <Text variant="caption" color={colors.textSecondary}>
        {label}
      </Text>
    </View>
  );
}

function AmountRow({
  label,
  amountMinorUnits,
  direction,
}: {
  label: string;
  amountMinorUnits: number;
  direction: 'credit' | 'debit';
}) {
  return (
    <View style={styles.amountRow}>
      <Text variant="body" color={colors.textSecondary}>
        {label}
      </Text>
      <AmountDisplay amountMinorUnits={amountMinorUnits} direction={direction} variant="amountLarge" />
    </View>
  );
}

function MotionDemo() {
  const [amount, setAmount] = useState(1000);
  const [successKey, setSuccessKey] = useState(0);
  const [showSuccess, setShowSuccess] = useState(false);

  return (
    <View style={styles.stack}>
      <Card style={styles.stack}>
        <Text variant="caption" color={colors.textSecondary}>
          AnimatedNumber — settles, never bounces
        </Text>
        <AnimatedNumber
          value={amount}
          formatValue={(v) => `$${v.toFixed(2)}`}
          variant="amountLarge"
          color={colors.accent}
        />
        <Button
          label="Randomize"
          variant="secondary"
          onPress={() => setAmount(Math.round(Math.random() * 200000) / 100)}
        />
      </Card>

      <Card style={[styles.stack, styles.successCard]}>
        <Text variant="caption" color={colors.textSecondary}>
          SuccessAnimation — a deliberate confirmation moment
        </Text>
        {showSuccess ? (
          <SuccessAnimation key={successKey} onComplete={() => {}} />
        ) : (
          <View style={styles.successPlaceholder} />
        )}
        <Button
          label="Trigger confirmation"
          variant="primary"
          onPress={() => {
            setShowSuccess(false);
            setSuccessKey((k) => k + 1);
            requestAnimationFrame(() => setShowSuccess(true));
          }}
        />
      </Card>
    </View>
  );
}

const styles = StyleSheet.create({
  safeArea: {
    flex: 1,
    backgroundColor: colors.background,
  },
  content: {
    padding: spacing.lg,
    gap: spacing.xl,
    paddingBottom: spacing.xxxl,
  },
  header: {
    alignItems: 'center',
    gap: spacing.xs,
    paddingVertical: spacing.lg,
  },
  section: {
    gap: spacing.md,
  },
  sectionTitle: {
    textTransform: 'uppercase',
    letterSpacing: 1,
    color: colors.textSecondary,
  },
  stack: {
    gap: spacing.md,
  },
  divider: {
    height: 1,
    backgroundColor: colors.border,
  },
  swatchGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.md,
  },
  swatchItem: {
    width: 96,
    gap: spacing.xs,
  },
  swatch: {
    width: 96,
    height: 64,
    borderRadius: 12,
  },
  iconRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.lg,
  },
  iconItem: {
    alignItems: 'center',
    gap: spacing.xs,
    width: 64,
  },
  hint: {
    marginTop: -spacing.xs,
  },
  amountRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  successCard: {
    alignItems: 'center',
  },
  successPlaceholder: {
    height: 96,
  },
});
