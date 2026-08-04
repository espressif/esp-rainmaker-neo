/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  Alert,
  Link,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import type { RedirectUrisCardProps } from "./redirect-uris-card.props";
import { ArrowRightLeft } from "lucide-react";

/**
 * Collapsible soft card that lists account-linking redirect URIs as external
 * links. Shared across the voice-assistant tabs (Alexa, GVA) since every
 * provider returns the same `redirect_uris` shape.
 */
export default function RedirectUrisCard({
  title,
  uris,
}: RedirectUrisCardProps) {
  const { t } = useTranslation("voice-assistants");

  return (
    <SectionCard
      primaryText={title}
      size="default"
      variant="soft"
      color="silver"
      allowCollapse={true}
      icon={<ArrowRightLeft className="h-4 w-4" aria-hidden />}
    >
      {uris.length === 0 ? (
        <Alert
          type="info"
          variant="soft"
          title={t("noRedirectUris", "No redirect URIs configured")}
        />
      ) : (
        <ul className="flex flex-col gap-2">
          {uris.map((uri) => (
            <li key={uri} className="min-w-0">
              <Link to={uri} color="primary" className="break-all">
                {uri}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </SectionCard>
  );
}
