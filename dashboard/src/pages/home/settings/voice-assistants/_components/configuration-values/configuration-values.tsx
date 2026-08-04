/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { ExternalLink, SlidersHorizontal } from "lucide-react";
import {
  Alert,
  Button,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import { useOAuthClient } from "@/api/oauth-clients";
import {
  getAlexaSkillArns,
  getAuthorizeUrl,
  getGvaFulfillmentUrl,
  getTokenUrl,
  getVaClientId,
} from "@/lib/config";
import { ConfigurationValuesList } from "./_components/configuration-values-list";
import type { ConfigurationValuesProps } from "./configuration-values.props";
import { usePreviewSignIn } from "./use-preview-sign-in";

/**
 * Every value an admin copies from our platform into the Alexa / Google developer console.
 * They are shown to be copied, not followed: the authorization endpoint answers a signed
 * request carrying a client and redirect URI, so opening it on its own is refused — which is
 * what the preview action exists for, since it assembles that request. The endpoints, client
 * id and secret hold whether or not either assistant is configured yet; only the fulfillment
 * URL is specific to one assistant.
 */
export default function ConfigurationValues({
  title,
  activeTab,
}: ConfigurationValuesProps) {
  const { t } = useTranslation("voice-assistants");
  const preview = usePreviewSignIn();

  const authorizeUrl = getAuthorizeUrl();
  const clientId = getVaClientId();
  const { client } = useOAuthClient(clientId);

  return (
    <SectionCard
      primaryText={title}
      size="default"
      variant="outline"
      color="mist"
      allowCollapse={true}
      defaultOpen={preview.hasFailed}
      icon={<SlidersHorizontal className="h-4 w-4" aria-hidden />}
      actions={
        /* In the header, so it stays reachable while the card is collapsed. */
        <Button
          type="button"
          variant="link"
          color="primary"
          size="sm"
          loading={preview.isPreparing}
          disabled={preview.isPreparing || !authorizeUrl || !clientId}
          endIcon={<ExternalLink className="h-4 w-4" aria-hidden />}
          tooltip={t(
            "previewSignInTooltip",
            "Opens the sign-in page your users see, in a new tab",
          )}
          onClick={() => void preview.start()}
        >
          {t("previewSignIn", "Preview sign-in page")}
        </Button>
      }
    >
      <div className="flex flex-col gap-3">
        {preview.error && (
          <Alert
            type="error"
            variant="soft"
            title={t(
              "previewSignInFailed",
              "Could not open the sign-in preview",
            )}
            description={preview.error}
          />
        )}
        <ConfigurationValuesList
          authorizeUrl={authorizeUrl}
          tokenUrl={getTokenUrl()}
          clientId={clientId}
          secret={client?.client_secret}
          // Space-separated, which is how both consoles expect a scope list.
          scopes={client?.scopes?.join(" ")}
          fulfillmentUrl={activeTab === "gva" ? getGvaFulfillmentUrl() : ""}
          skillArns={activeTab === "alexa" ? getAlexaSkillArns() : {}}
        />
      </div>
    </SectionCard>
  );
}
