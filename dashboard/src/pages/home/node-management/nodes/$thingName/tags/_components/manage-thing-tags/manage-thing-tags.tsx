/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { SectionCard } from "@espressif/dashboard-ui-components/components";
import { useNodeTags } from "@/api/node-tags";
import AddThingTagPopover from "./add-thing-tag-popover";
import { MANAGE_THING_TAGS_CONFIG } from "./manage-thing-tags.config";
import { getManageThingTagsColumns } from "./manage-thing-tags-columns";
import { tagRecordToRows } from "./manage-thing-tags.utils";
import ManageThingTagsContent from "./manage-thing-tags-content";
import type { ManageThingTagsProps } from "./manage-thing-tags.props";

const INITIAL_VISIBLE_COUNT = 5;
const VIEW_MORE_STEP = 5;

export default function ManageThingTags({
  thingName,
  type,
  readOnly = false,
}: ManageThingTagsProps) {
  const { t } = useTranslation("nodes");
  const [visibleCount, setVisibleCount] = useState(INITIAL_VISIBLE_COUNT);

  const config = MANAGE_THING_TAGS_CONFIG[type];
  const { data, isLoading, isError, refetch, isRefetching } =
    useNodeTags(thingName);

  const rows = useMemo(() => tagRecordToRows(data?.[type]), [data, type]);
  const existingKeys = useMemo(() => rows.map((r) => r.key), [rows]);

  const columns = useMemo(
    () => getManageThingTagsColumns({ t, thingName, type, readOnly }),
    [t, thingName, type, readOnly],
  );

  const visibleRows = useMemo(
    () => rows.slice(0, visibleCount),
    [rows, visibleCount],
  );
  const remainingCount = Math.max(0, rows.length - visibleRows.length);

  const handleViewMore = useCallback(() => {
    setVisibleCount((current) => current + VIEW_MORE_STEP);
  }, []);

  const handleRetry = useCallback(() => {
    void refetch();
  }, [refetch]);

  const IconComponent = config.Icon;

  const actions =
    !readOnly && !isError ? (
      <AddThingTagPopover
        thingName={thingName}
        type={type}
        existingKeys={existingKeys}
      />
    ) : null;

  return (
    <SectionCard
      icon={<IconComponent className="h-5 w-5" />}
      primaryText={t(config.titleKey, config.titleFallback)}
      secondaryText={t(config.descriptionKey, config.descriptionFallback)}
      color="silver"
      variant="outline"
      actions={actions}
    >
      <ManageThingTagsContent
        rows={rows}
        visibleRows={visibleRows}
        columns={columns}
        isLoading={isLoading}
        isError={isError}
        isRefetching={isRefetching}
        onRetry={handleRetry}
        emptyHeading={t(config.emptyHeadingKey, config.emptyHeadingFallback)}
        emptyDescription={t(
          config.emptyDescriptionKey,
          config.emptyDescriptionFallback,
        )}
        remainingCount={remainingCount}
        onViewMore={handleViewMore}
        pageSize={INITIAL_VISIBLE_COUNT}
      />
    </SectionCard>
  );
}
