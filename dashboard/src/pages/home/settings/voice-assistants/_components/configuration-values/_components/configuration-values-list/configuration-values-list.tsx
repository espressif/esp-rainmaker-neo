/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  Alert,
  MonospaceContent,
} from "@espressif/dashboard-ui-components/components";
import type { ConfigurationValuesListProps } from "./configuration-values-list.props";

/** The Alexa region each endpoint serves, named as the Alexa console names it. */
const ALEXA_REGION_LABELS: Record<string, string> = {
  "us-east-1": "North America",
  "eu-west-1": "Europe & India",
  "us-west-2": "Far East",
};

/**
 * Renders whichever configuration values the platform has published, or an
 * empty-state notice when none of them are available yet.
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

  const hasValues = Boolean(
    authorizeUrl ||
      tokenUrl ||
      clientId ||
      secret ||
      scopes ||
      fulfillmentUrl ||
      Object.keys(skillArns).length > 0,
  );

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

  return (
    <>
      {authorizeUrl && (
        <MonospaceContent
          title={t("authorizationUri", "Authorization URI")}
          value={authorizeUrl}
          color="mist"
        />
      )}
      {tokenUrl && (
        <MonospaceContent
          title={t("accessTokenUri", "Access Token URI")}
          value={tokenUrl}
          color="mist"
        />
      )}
      {clientId && (
        <MonospaceContent
          title={t("clientId", "Client ID")}
          value={clientId}
          color="mist"
        />
      )}
      {secret && (
        <MonospaceContent
          title={t("clientSecret", "Client Secret")}
          value={secret}
          color="mist"
          mask
        />
      )}
      {scopes && (
        <MonospaceContent
          title={t("scopes", "Scopes")}
          value={scopes}
          color="mist"
        />
      )}
      {fulfillmentUrl && (
        <MonospaceContent
          title={t("fulfillmentUrl", "Fulfillment URL")}
          value={fulfillmentUrl}
          color="mist"
        />
      )}
      {Object.entries(skillArns).map(([region, arn]) => (
        <MonospaceContent
          key={region}
          title={
            t("skillEndpoint", "Skill endpoint") +
            ` — ${ALEXA_REGION_LABELS[region] ?? region}`
          }
          value={arn}
          color="mist"
        />
      ))}
    </>
  );
}
