/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react";
import { Trans, useTranslation } from "react-i18next";
import { Check } from "lucide-react";
import { Link, SectionCard } from "@espressif/dashboard-ui-components/components";
import { PUSH_INTEGRATION_HELP } from "@/config/push-integration.config";
import type { IntegrationHelpCardProps } from "./integration-help-card.props";

/** Flattens `<Trans>` children (string | array of nodes) into plain text. */
function flattenText(node: ReactNode): string {
  if (typeof node === "string") {
    return node;
  }
  if (Array.isArray(node)) {
    return node.map(flattenText).join("");
  }
  return "";
}

/**
 * Renders a `<url>` tag from a translated step. The link text is the URL itself,
 * so it doubles as the destination. `Link` detects the absolute URL as external
 * and opens it in a new tab with safe `rel` attributes.
 */
function StepLink({ children }: { children?: ReactNode }) {
  const href = flattenText(children);
  return (
    <Link to={href} color="primary">
      {children}
    </Link>
  );
}

/** Emphasises a key term (e.g. "bundle ID") from a translated step. */
function StepTerm({ children }: { children?: ReactNode }) {
  return <span className="font-medium italic text-foreground">{children}</span>;
}

const STEP_COMPONENTS = { url: <StepLink />, term: <StepTerm /> };

/**
 * Contextual "how to get credentials" card shown above the platform fields.
 * Title, icon and steps come from `PUSH_INTEGRATION_HELP`, so both platforms
 * (and any future one) share this single, config-driven component.
 */
export default function IntegrationHelpCard({ type }: IntegrationHelpCardProps) {
  const { t } = useTranslation("push-notifications");
  const { Icon, titleKey, stepKeys } = PUSH_INTEGRATION_HELP[type];

  return (
    <SectionCard
      size="sm"
      variant="soft"
      color="info"
      allowCollapse={true}
      icon={<Icon className="h-5 w-5" aria-hidden />}
      primaryText={t(titleKey)}
    >
      <ul className="flex flex-col gap-2">
        {stepKeys.map((key) => (
          <li key={key} className="flex items-center gap-2">
            <Check className="h-4 w-4 shrink-0 text-info" aria-hidden />
            <span className="text-sm text-muted-foreground">
              <Trans t={t} i18nKey={key} components={STEP_COMPONENTS} />
            </span>
          </li>
        ))}
      </ul>
    </SectionCard>
  );
}
