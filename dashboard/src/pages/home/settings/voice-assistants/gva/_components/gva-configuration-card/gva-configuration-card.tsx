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
  DynamicList,
  SectionCard,
  Separator,
  toast,
} from "@espressif/dashboard-ui-components/components";
import { CustomIcon } from "@/components/custom-icon";
import { RedirectUrisCard } from "../../../_components/redirect-uris-card";
import type { GvaConfigurationCardProps } from "./gva-configuration-card.props";
import {
  buildGvaConfigItems,
  buildGvaConfigMeta,
} from "./gva-configuration-card.utils";

/**
 * Read-only presentation of the saved GVA (Google Voice Assistant) service
 * account. Every service-account field is rendered via {@link DynamicList};
 * `redirect_uris` renders separately as links in {@link RedirectUrisCard}.
 * "Copy JSON" copies the full integration payload to the clipboard; "Edit"
 * opens the configuration sheet.
 */
export default function GvaConfigurationCard({
  config,
  onEdit,
}: GvaConfigurationCardProps) {
  const { t } = useTranslation(["voice-assistants", "common"]);

  const handleCopyJson = async () => {
    try {
      await navigator.clipboard.writeText(JSON.stringify(config, null, 2));
      toast.success(
        t("gva.copyJsonSuccess", "Configuration JSON copied to clipboard"),
      );
    } catch {
      toast.error(t("gva.copyJsonError", "Failed to copy configuration JSON"));
    }
  };

  return (
    <SectionCard
      icon={<CustomIcon type="google-assistant" size={24} aria-hidden />}
      primaryText={t("gva.heading", "GVA Configuration")}
      secondaryText={t("gva.secondaryText", "Google service account credentials and account-linking redirect URIs")}
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
            {t("gva.copyJson", "Copy JSON")}
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
        <DynamicList
          items={buildGvaConfigItems(config)}
          meta={buildGvaConfigMeta(t)}
          direction="row"
          hideIcon={true}
          keyWidth={35}
          simple={true}
          size="default"
        />
        <Separator />
        <RedirectUrisCard
          title={t("gva.redirectUris", "Redirect URIs")}
          uris={config.redirect_uris ?? []}
        />
      </div>
    </SectionCard>
  );
}
