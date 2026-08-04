/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { SimplifiedDate } from "@espressif/dashboard-ui-components";
import { Badge } from "@espressif/dashboard-ui-components/components";
import { OtaImageNameCell } from "../_components/ota-image-name-cell";
import { OtaImageTypeModelCell } from "../_components/ota-image-type-model-cell";
import { OtaImagesRowActions } from "../_components/ota-images-row-actions";
import type { OtaImageRow } from "../ota-images.props";

export function getOtaImagesColumns(t: TFunction): ColumnDef<OtaImageRow>[] {
  return [
    {
      accessorKey: "name",
      header: t("columns.nameMd5", "Name / MD5"),
      enableHiding: false,
      cell: ({ row }) => (
        <OtaImageNameCell
          name={row.original.name}
          size={row.original.size}
          md5={row.original.md5}
          fwType={row.original.type}
        />
      ),
    },
    {
      id: "typeModel",
      accessorKey: "type",
      header: t("columns.typeModel", "Type / Model"),
      cell: ({ row }) => (
        <OtaImageTypeModelCell
          type={row.original.type}
          model={row.original.model}
        />
      ),
    },
    {
      accessorKey: "platform",
      header: t("columns.platform", "Platform"),
      cell: ({ row }) => {
        const platform = row.original.platform;
        if (!platform) {
          return null;
        }
        return (
          <span className="text-muted-foreground text-sm">{platform}</span>
        );
      },
    },
    {
      accessorKey: "version",
      header: t("columns.fwVersion", "FW Version"),
      cell: ({ row }) => {
        const value = row.original.version;
        if (!value) {
          return null;
        }
        return (
          <Badge
            color="info"
            variant="outline"
            className="font-normal tracking-wide rounded-full"
          >
            {value}
          </Badge>
        );
      },
    },
    {
      accessorKey: "lastModified",
      header: t("columns.lastModified", "Last Modified"),
      cell: ({ row }) => (
        <SimplifiedDate relative ts={row.original.lastModified?.getTime()} />
      ),
    },
    {
      id: "actions",
      header: "",
      enableHiding: false,
      cell: ({ row }) => (
        <div className="flex justify-end">
          <OtaImagesRowActions
            imageKey={row.original.key}
            name={row.original.name}
          />
        </div>
      ),
    },
  ];
}
