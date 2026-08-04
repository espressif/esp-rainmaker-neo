/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  AdvancedSearchBox,
  Button,
  type SearchMeta,
} from "@espressif/dashboard-ui-components/components";
import type {
  NodeGroupsPageHeaderProps,
  NodeGroupsSearchValue,
} from "./node-groups-page-header.props";
import type { NodeGroupsSearchField } from "@/api/node-groups";

const SEARCH_FIELD_IDS: NodeGroupsSearchField[] = ["groupName", "description"];

export function NodeGroupsPageHeader({
  onSearch,
  onCreateClick,
}: NodeGroupsPageHeaderProps) {
  const { t } = useTranslation(["node-groups", "common"]);

  const searchMeta: SearchMeta[] = useMemo(
    () => [
      {
        id: "groupName",
        label: t("common:columns.name", "Name"),
        type: "text",
        placeholder: t("search.groupName.placeholder", "Search by name"),
      },
      {
        id: "description",
        label: t("search.description.label", "Description"),
        type: "text",
        placeholder: t("search.description.placeholder", "Search by description"),
      },
    ],
    [t],
  );

  const handleSearch = (query: { id: string; value: string }) => {
    const field = SEARCH_FIELD_IDS.find((id) => id === query.id);
    if (!field || !query.value) {
      onSearch(null);
      return;
    }
    onSearch({ id: field, value: query.value } satisfies NodeGroupsSearchValue);
  };

  return (
    <div className="flex items-center justify-between gap-4 p-5 bg-accent/10 w-full">
      <div className="min-w-0 w-full max-w-md">
        <AdvancedSearchBox
          searchMeta={searchMeta}
          onSearch={handleSearch}
          searchOnEnter
        />
      </div>
      <Button
        variant="default"
        fullWidth={false}
        startIcon={<Plus className="h-4 w-4" aria-hidden />}
        onClick={onCreateClick}
        size="sm"
      >
        {t("createNodeGroupButton", "Create Node Group")}
      </Button>
    </div>
  );
}
