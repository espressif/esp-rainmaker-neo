/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  Alert,
  AnimatedCard,
  Button,
  DynamicList,
  Skeleton,
  type DynamicListEntry,
} from "@espressif/dashboard-ui-components/components";

interface AttributesSectionContentProps {
  items: DynamicListEntry[];
  isLoading: boolean;
  isError: boolean;
  isRefetching: boolean;
  onRetry: () => void;
}

export default function AttributesSectionContent({
  items,
  isLoading,
  isError,
  isRefetching,
  onRetry,
}: AttributesSectionContentProps) {
  const { t } = useTranslation(["nodes", "common"]);

  if (isLoading) {
    return (
      <div className="flex flex-col gap-3">
        <Skeleton className="h-6 w-full" />
        <Skeleton className="h-6 w-full" />
        <Skeleton className="h-6 w-full" />
      </div>
    );
  }

  if (isError) {
    return (
      <AnimatedCard
        type="errorSpreadOut"
        iconSize={96}
        actions={
          <Button
            type="button"
            variant="ghost"
            size="sm"
            color="primary"
            fullWidth={false}
            loading={isRefetching}
            onClick={onRetry}
          >
            {t("common:actions.tryAgain", "Try again")}
          </Button>
        }
      >
        {t("attributes.errorLoading", "Failed to load attributes")}
      </AnimatedCard>
    );
  }

  if (items.length === 0) {
    return (
      <Alert variant="soft" color="info" type="info">
        {t(
          "attributes.emptyDescription",
          "This node has no AWS IoT Thing attributes.",
        )}
      </Alert>
    );
  }

  return (
    <DynamicList
      items={items}
      direction="row"
      keyWidth={40}
      hideIcon
      simple
      verbatimKeyLabels
    />
  );
}
