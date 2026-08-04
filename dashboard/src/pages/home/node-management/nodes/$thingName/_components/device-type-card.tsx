/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Microchip } from "lucide-react";
import {
  Card,
  CardContent,
  DeviceIcon,
  IconAvatar,
} from "@espressif/dashboard-ui-components/components";
import { cn } from "@/utils/utils";

interface DeviceTypeCardProps {
  type: string | undefined;
  model: string | undefined;
  version: string | undefined;
  className?: string;
}

interface DeviceTypeCardField {
  id: string;
  label: string;
  value: string;
}

function displayOrDash(value: string | undefined): string {
  if (typeof value !== "string") {
    return "-";
  }
  const trimmed = value.trim();
  return trimmed ? trimmed : "-";
}

export default function DeviceTypeCard({
  type,
  model,
  version,
  className,
}: DeviceTypeCardProps) {
  const { t } = useTranslation("nodes");

  const deviceIconType = type?.trim() || model?.trim() || "";

  const fields: DeviceTypeCardField[] = useMemo(
    () => [
      {
        id: "type",
        label: t("details.overview.deviceInfo.type", "Type"),
        value: displayOrDash(type),
      },
      {
        id: "model",
        label: t("details.overview.deviceInfo.model", "Model"),
        value: displayOrDash(model),
      },
      {
        id: "version",
        label: t("details.overview.deviceInfo.version", "Version"),
        value: displayOrDash(version),
      },
    ],
    [t, type, model, version],
  );

  return (
    <Card
      className={cn("w-full min-w-0 border-1 shadow-none", className)}
    >
      <CardContent className="flex flex-col min-w-0 items-center gap-2 p-2">
        <div className="shrink-0">
          <IconAvatar size={100} color="primary">
            <DeviceIcon
              type={deviceIconType}
              size={56}
              fallback={Microchip}
            />
          </IconAvatar>
        </div>
        <div className="grid w-full min-w-0 grid-cols-3 divide-x divide-border rounded-lg">
          {fields.map((field) => (
            <div key={field.id} className="min-w-0 px-4 py-2">
              <p className="text-center text-xs leading-normal text-muted-foreground">
                {field.label}
              </p>
              <p className="truncate text-center text-base font-semibold leading-normal text-foreground">
                {field.value}
              </p>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}
