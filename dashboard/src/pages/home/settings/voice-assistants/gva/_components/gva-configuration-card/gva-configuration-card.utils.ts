/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import type {
  DynamicListEntry,
  DynamicListMetaEntry,
} from "@espressif/dashboard-ui-components/components";
import type { GvaConfigGetResponse } from "@/api/integrations";

/**
 * Service-account fields rendered in the DynamicList, in display order.
 * `redirect_uris` is intentionally excluded — it renders separately in
 * {@link RedirectUrisCard} as a list of links.
 */
const FIELD_ORDER: (keyof GvaConfigGetResponse)[] = [
  "type",
  "project_id",
  "private_key_id",
  "client_email",
  "client_id",
  "auth_uri",
  "token_uri",
  "auth_provider_x509_cert_url",
  "client_x509_cert_url",
  "universe_domain",
];

/**
 * Builds the DynamicList rows from the saved config, skipping empty fields so
 * the card never shows blank rows for optional service-account keys.
 */
export function buildGvaConfigItems(
  config: GvaConfigGetResponse,
): DynamicListEntry[] {
  return FIELD_ORDER.reduce<DynamicListEntry[]>((entries, key) => {
    const value = config[key];
    if (value != null && value !== "") {
      entries.push({ key, value });
    }
    return entries;
  }, []);
}

/**
 * Per-key rendering config for the DynamicList. `label` reuses the existing
 * gva.json field keys so labels stay translated and correctly cased
 * ("Auth URI", not the default startCase "Auth Uri"). `universe_domain` has no
 * `type`, so it renders as plain text.
 */
export function buildGvaConfigMeta(
  t: TFunction,
): Record<string, DynamicListMetaEntry> {
  return {
    type: { type: "badge", label: t("gva.type", "Type") },
    private_key_id: {
      type: "mono",
      label: t("gva.privateKeyId", "Private Key ID"),
    },
    auth_uri: { type: "url", label: t("gva.authUri", "Auth URI") },
    token_uri: { type: "url", label: t("gva.tokenUri", "Token URI") },
    auth_provider_x509_cert_url: {
      type: "url",
      label: t("gva.authProviderCertUrl", "Auth Provider x509 Cert URL"),
    },
    client_x509_cert_url: {
      type: "url",
      label: t("gva.clientCertUrl", "Client x509 Cert URL"),
    },
    universe_domain: { label: t("gva.universeDomain", "Universe Domain") },
  };
}
