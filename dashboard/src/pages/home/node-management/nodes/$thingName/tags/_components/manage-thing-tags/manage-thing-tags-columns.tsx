/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import {
  CopiableText,
  Tag,
} from "@espressif/dashboard-ui-components/components";
import type { ManageThingTagsType } from "./manage-thing-tags.config";
import type { TagRow } from "./manage-thing-tags.utils";
import ManageThingTagsRowActions from "./manage-thing-tags-row-actions";

interface GetManageThingTagsColumnsArgs {
  t: TFunction;
  thingName: string;
  type: ManageThingTagsType;
  readOnly: boolean;
}

export function getManageThingTagsColumns({
  t,
  thingName,
  type,
  readOnly,
}: GetManageThingTagsColumnsArgs): ColumnDef<TagRow>[] {
  const columns: ColumnDef<TagRow>[] = [
    {
      id: "tag",
      header: t("tags.columns.tag", "Tag"),
      cell: ({ row }) => (
        <Tag
          name={row.original.key}
          value={row.original.value}
          color="secondary"
          variant="outline"
          size="sm"
          rounded
        />
      ),
    },
    {
      id: "key",
      accessorKey: "key",
      header: t("tags.columns.key", "Key"),
      cell: ({ row }) => (
        <span className="block max-w-[16rem] truncate text-sm font-medium">
          {row.original.key}
        </span>
      ),
    },
    {
      id: "value",
      accessorKey: "value",
      header: t("common:columns.value", "Value"),
      cell: ({ row }) => (
        <div className="max-w-[20rem]">
          <CopiableText text={row.original.value} />
        </div>
      ),
    },
  ];

  if (!readOnly) {
    columns.push({
      id: "actions",
      header: "",
      cell: ({ row }) => (
        <ManageThingTagsRowActions
          thingName={thingName}
          type={type}
          row={row.original}
        />
      ),
    });
  }

  return columns;
}
