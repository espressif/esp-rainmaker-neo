/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Copy, Pencil } from "lucide-react";
import {
  Button,
  ButtonGroup,
  MonospaceContent,
  SectionCard,
  toast,
} from "@espressif/dashboard-ui-components/components";
import { CustomIcon } from "@/components/custom-icon";
import { RedirectUrisCard } from "../../../_components/redirect-uris-card";
import type { AlexaConfigurationCardProps } from "./alexa-configuration-card.props";

export default function AlexaConfigurationCard({
  config,
  onEdit,
}: AlexaConfigurationCardProps) {
  const { t } = useTranslation(["voice-assistants", "common"]);

  const handleCopyJson = async () => {
    try {
      await navigator.clipboard.writeText(JSON.stringify(config, null, 2));
      toast.success(
        t("alexa.copyJsonSuccess", "Configuration JSON copied to clipboard"),
      );
    } catch {
      toast.error(t("alexa.copyJsonError", "Failed to copy configuration JSON"));
    }
  };

  return (
    <SectionCard
      icon={<CustomIcon type="amazon-alexa" size={24} aria-hidden />}
      primaryText={t("alexa.heading", "Alexa Configuration")}
      secondaryText={t("alexa.secondaryText", "Amazon Alexa skill credentials and account-linking redirect URIs")}
      allowCollapse={false}
      color="mist"
      variant="outline"
      size="lg"
      actions={
        <ButtonGroup>
          <Button
            type="button"
            variant="outline"
            color="gray"
            size="sm"
            fullWidth={false}
            onClick={() => void handleCopyJson()}
            startIcon={<Copy className="h-4 w-4" aria-hidden />}
          >
            {t("alexa.copyJson", "Copy JSON")}
          </Button>
          <Button
            type="button"
            variant="outline"
            color="gray"
            size="sm"
            fullWidth={false}
            onClick={onEdit}
            startIcon={<Pencil className="h-4 w-4" aria-hidden />}
          >
            {t("common:edit", "Edit")}
          </Button>
        </ButtonGroup>
      }
    >
      <div className="flex flex-col gap-4">
        {config.client_id && (
          <MonospaceContent
            title={t("alexa.clientId", "Client ID")}
            value={config.client_id}
            color="mist"
          />
        )}
        {config.skill_id && (
          <MonospaceContent
            title={t("alexa.skillId", "Skill ID")}
            value={config.skill_id}
            color="mist"
          />
        )}
        {config.manufacturer_name && (
          <MonospaceContent
            title={t("alexa.manufacturerName", "Manufacturer Name")}
            value={config.manufacturer_name}
            color="mist"
          />
        )}
        <RedirectUrisCard
          title={t("alexa.redirectUris", "Redirect URIs")}
          uris={config.redirect_uris ?? []}
        />
      </div>
    </SectionCard>
  );
}
