/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  Alert,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import { cn } from "@/utils/utils";
import type { OnboardingCardProps } from "./onboarding-card.props";

const CARD_CLASS =
  "w-full max-w-xl bg-transparent border-none";

/**
 * Shared card chrome for the unauthenticated onboarding pages (sign in, reset
 * password, set password). Keeps the three pages visually identical so they
 * cannot drift apart as the flow grows.
 */
export default function OnboardingCard({
  icon,
  title,
  description,
  children,
  actions,
  className,
}: OnboardingCardProps) {
  return (
    <SectionCard
      allowCollapse={false}
      icon={icon}
      variant="soft"
      color="mist"
      className={cn(CARD_CLASS, className)}
      primaryText={title}
      actions={actions}
    >
      {description ? (
        <Alert hideIcon color="gray" variant="soft" className="mb-6">
          {description}
        </Alert>
      ) : null}

      {children}
    </SectionCard>
  );
}
