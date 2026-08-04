/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { LucideIcon } from "lucide-react";
import { FileJson, KeyRound } from "lucide-react";
import type { IntegrationDetail } from "@/api/integrations";
import type { CustomIconId } from "@/components/custom-icon";

/**
 * Presentation config for push notification integrations.
 *
 * Single source of truth mapping an `integration_type` to its brand icon and
 * mapping an integration to its human-readable descriptor (bundle vs project
 * id). Keeping this here means adding a new push provider is a one-line change
 * that every UI surface (card, future detail view) picks up automatically.
 */

/** APNS (all variants) render with the Apple icon; GCM/FCM with Google. */
const APNS_ICON: CustomIconId = "apple";
const GCM_ICON: CustomIconId = "android";

export interface PushIntegrationPresentation {
  iconType: CustomIconId;
}

/**
 * Resolve the brand icon for an integration type. Anything starting with
 * `apns` is Apple; everything else (gcm/fcm) is Google. Never throws — an
 * unknown type falls back to the Google icon so the card still renders.
 */
export function getPushIntegrationPresentation(
  integrationType: string,
): PushIntegrationPresentation {
  if (integrationType.startsWith("apns")) {
    return { iconType: APNS_ICON };
  }
  return { iconType: GCM_ICON };
}

/** True for APNS sandbox integrations (development builds). */
export function isSandboxIntegration(integrationType: string): boolean {
  return integrationType.includes("sandbox");
}

/**
 * The descriptor shown under an integration's title: APNS integrations are
 * identified by their bundle id, GCM/FCM by their Firebase project id.
 * `i18nLabelKey` points at the `push` namespace label for that field.
 */
export interface PushIntegrationDescriptor {
  /** Existing `push` namespace label key (reused, not duplicated). */
  i18nLabelKey: "bundleId" | "projectId";
  value: string;
}

export function getIntegrationDescriptor(
  integration: IntegrationDetail,
): PushIntegrationDescriptor | null {
  if (integration.integration_type.startsWith("apns")) {
    if (!integration.bundle_id) {
      return null;
    }
    return { i18nLabelKey: "bundleId", value: integration.bundle_id };
  }

  if (!integration.project_id) {
    return null;
  }
  return { i18nLabelKey: "projectId", value: integration.project_id };
}

/**
 * Platform choice presented in the "Add integration" form. This is the UI-level
 * selection ("ios"/"android"); it maps to a concrete `integration_type`
 * (apns / apns_sandbox / gcm) only when the register request is built.
 */
export type PushIntegrationFormType = "ios" | "android";

export interface PushIntegrationTypeOption {
  id: PushIntegrationFormType;
  iconType: CustomIconId;
  /** `push` namespace key for the card's primary label (e.g. "iOS"). */
  primaryKey: string;
  /** `push` namespace key for the platform badge (e.g. "APNS"). */
  badgeKey: string;
}

/** Feeds the integration-type `SelectableCardList`. iOS is listed first (default). */
export const PUSH_INTEGRATION_TYPE_OPTIONS: readonly PushIntegrationTypeOption[] =
  [
    {
      id: "ios",
      iconType: APNS_ICON,
      primaryKey: "push-notifications:form.typeIosPrimary",
      badgeKey: "push-notifications:form.typeIosBadge",
    },
    {
      id: "android",
      iconType: GCM_ICON,
      primaryKey: "push-notifications:form.typeAndroidPrimary",
      badgeKey: "push-notifications:form.typeAndroidBadge",
    },
  ];

export interface PushIntegrationHelpContent {
  Icon: LucideIcon;
  /** `push` namespace key for the help card title. */
  titleKey: string;
  /**
   * `push` namespace keys for the ordered steps. These resolve to marked-up
   * strings (`<url>` / `<term>` tags) rendered via `<Trans>`, so they are
   * separate from the plain legacy `*HowToStep*` keys.
   */
  stepKeys: readonly string[];
}

/**
 * Contextual "how to get credentials" help shown per platform. Steps live under
 * `form.steps.*` with inline markup so URLs become links and key terms are
 * emphasised; the title reuses the existing plain `*HowToTitle` key.
 */
export const PUSH_INTEGRATION_HELP: Record<
  PushIntegrationFormType,
  PushIntegrationHelpContent
> = {
  ios: {
    Icon: KeyRound,
    titleKey: "push-notifications:apnsHowToTitle",
    stepKeys: ["form.steps.ios.1", "form.steps.ios.2", "form.steps.ios.3"],
  },
  android: {
    Icon: FileJson,
    titleKey: "push-notifications:gcmHowToTitle",
    stepKeys: [
      "form.steps.android.1",
      "form.steps.android.2",
      "form.steps.android.3",
      "form.steps.android.4",
    ],
  },
};
