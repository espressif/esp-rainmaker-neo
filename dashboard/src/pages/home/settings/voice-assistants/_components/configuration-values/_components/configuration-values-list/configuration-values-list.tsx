/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  Alert,
  MonospaceContent,
  SimpleList,
  type SimpleListItem,
} from "@espressif/dashboard-ui-components/components";
import {
  KeyRound,
  Link as LinkIcon,
  Lock,
  Server,
  ShieldCheck,
} from "lucide-react";
import type { ConfigurationValuesListProps } from "./configuration-values-list.props";

/** The Alexa region each endpoint serves, named as the Alexa console names it. */
const ALEXA_REGION_LABELS: Record<string, string> = {
  "us-east-1": "North America",
  "eu-west-1": "Europe & India",
  "us-west-2": "Far East",
};

/**
 * Renders whichever configuration values the platform has published, or an
 * empty-state notice when none of them are available yet. Uses `SimpleList` so
 * each value keeps its own titled row while the shared list owns spacing,
 * separators and icon sizing.
 */
export default function ConfigurationValuesList({
  authorizeUrl,
  tokenUrl,
  clientId,
  secret,
  scopes,
  fulfillmentUrl,
  skillArns,
}: ConfigurationValuesListProps) {
  const { t } = useTranslation("voice-assistants");

  // `SimpleList` skips items whose `content` is null/undefined, so unset values
  // fall out of the list automatically instead of us branching per-row.
  const items: SimpleListItem[] = [
    {
      key: "authorizeUrl",
      label: t("authorizationUri", "Authorization URI"),
      icon: LinkIcon,
      content: authorizeUrl ? (
        <MonospaceContent value={authorizeUrl} color="silver" />
      ) : undefined,
    },
    {
      key: "tokenUrl",
      label: t("accessTokenUri", "Access Token URI"),
      icon: LinkIcon,
      content: tokenUrl ? (
        <MonospaceContent value={tokenUrl} color="silver" />
      ) : undefined,
    },
    {
      key: "clientId",
      label: t("clientId", "Client ID"),
      icon: KeyRound,
      content: clientId ? (
        <MonospaceContent value={clientId} color="silver" />
      ) : undefined,
    },
    {
      key: "secret",
      label: t("clientSecret", "Client Secret"),
      icon: Lock,
      content: secret ? (
        <MonospaceContent value={secret} color="silver" mask />
      ) : undefined,
    },
    {
      key: "scopes",
      label: t("scopes", "Scopes"),
      icon: ShieldCheck,
      content: scopes ? (
        <MonospaceContent value={scopes} color="silver" />
      ) : undefined,
    },
    {
      key: "fulfillmentUrl",
      label: t("fulfillmentUrl", "Fulfillment URL"),
      icon: Server,
      content: fulfillmentUrl ? (
        <MonospaceContent value={fulfillmentUrl} color="silver" />
      ) : undefined,
    },
    ...Object.entries(skillArns).map<SimpleListItem>(([region, arn]) => ({
      key: `skill-${region}`,
      label:
        t("skillEndpoint", "Skill endpoint") +
        ` — ${ALEXA_REGION_LABELS[region] ?? region}`,
      icon: Server,
      content: <MonospaceContent value={arn} color="silver" />,
    })),
  ];

  const hasValues = items.some((item) => item.content !== undefined);

  if (!hasValues) {
    return (
      <Alert
        type="info"
        variant="soft"
        hideIcon
        description={t(
          "noConfigurationValues",
          "No configuration values published",
        )}
      />
    );
  }

  return <SimpleList items={items} iconStyle="none" />;
}
