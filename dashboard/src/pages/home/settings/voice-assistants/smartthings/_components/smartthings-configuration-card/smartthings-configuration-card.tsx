/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { CustomIcon } from "@/components/custom-icon";
import { Pencil } from "lucide-react";
import {
  Button,
  DynamicList,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import type { SmartThingsConfigurationCardProps } from "./smartthings-configuration-card.props";

export default function SmartThingsConfigurationCard({
  config,
  onEdit,
}: SmartThingsConfigurationCardProps) {
  const { t } = useTranslation(["voice-assistants", "common"]);

  return (
    <SectionCard
      icon={<CustomIcon type="smartthings" size={24} aria-hidden />}
      primaryText={t("smartthings.heading", "SmartThings Configuration")}
      secondaryText={t(
        "smartthings.secondaryText",
        "Schema App credentials issued by the SmartThings Developer Workspace",
      )}
      allowCollapse={false}
      color="mist"
      variant="outline"
      size="lg"
      actions={
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
      }
    >
      <DynamicList
        items={[{ key: "client_id", value: config.client_id ?? "" }]}
        meta={{
          client_id: { type: "mono", label: t("smartthings.clientId", "Client ID") },
        }}
        direction="row"
        hideIcon={true}
        keyWidth={35}
        simple={true}
        size="default"
      />
    </SectionCard>
  );
}
