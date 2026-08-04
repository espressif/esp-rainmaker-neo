/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useFormContext } from "react-hook-form";
import {
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  SelectableCardList,
  type SelectableCardListItem,
} from "@espressif/dashboard-ui-components/components";
import { HardDrive, Network, Video } from "lucide-react";
import { isBridgeEnabled } from "@/lib/config";
import type { RegisterNodesFormValues } from "../../_schema/register-nodes-form.schema";

export function CapabilitiesSection() {
  const { t } = useTranslation("register");
  const { control } = useFormContext<RegisterNodesFormValues>();

  const cards = useMemo<SelectableCardListItem[]>(
    () => [
      {
        id: "s3",
        icon: <HardDrive className="h-5 w-5" />,
        primaryText: t(
          "new.capabilities.s3.label",
          "File Storage (S3)",
        ),
        secondaryText: t(
          "new.capabilities.s3.description",
          "Enable S3-backed file storage on the device.",
        ),
      },
      {
        id: "kvs",
        icon: <Video className="h-5 w-5" />,
        primaryText: t(
          "new.capabilities.kvs.label",
          "Camera Streaming (KVS)",
        ),
        secondaryText: t(
          "new.capabilities.kvs.description",
          "Enable Kinesis Video Streams for camera nodes.",
        ),
      },
      ...(isBridgeEnabled()
        ? [
            {
              id: "bridge",
              icon: <Network className="h-5 w-5" />,
              primaryText: t(
                "new.capabilities.bridge.label",
                "Bridge",
              ),
              secondaryText: t(
                "new.capabilities.bridge.description",
                "Enable bridge behavior for gateway nodes.",
              ),
            },
          ]
        : []),
    ],
    [t],
  );

  return (
    <FormField
      control={control}
      name="capabilities"
      render={({ field }) => (
        <FormItem>
          <FormControl>
            <SelectableCardList
              allowMultiple
              value={field.value ?? []}
              onChange={(next) =>
                field.onChange(next)
              }
              data={cards}
              aria-label={t(
                "new.capabilities.ariaLabel",
                "Choose capabilities",
              )}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  );
}
