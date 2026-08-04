/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Bolt } from "lucide-react";
import {
  SectionCard,
  type DynamicListEntry,
} from "@espressif/dashboard-ui-components/components";
import { useThingAttributes } from "../../_hooks/use-thing-attributes";
import type { AttributesSectionProps } from "./attributes-section.props";
import AttributesSectionContent from "./attributes-section-content";

function toSortedEntries(record: Record<string, string>): DynamicListEntry[] {
  return Object.entries(record)
    .sort(([a], [b]) =>
      a.localeCompare(b, undefined, { sensitivity: "base" }),
    )
    .map(([key, value]) => ({ key, value }));
}

export default function AttributesSection({
  thingName,
}: AttributesSectionProps) {
  const { t } = useTranslation("nodes");
  const { attributes, isLoading, isError, isRefetching, refetch } =
    useThingAttributes(thingName);

  const items = useMemo(() => toSortedEntries(attributes), [attributes]);

  return (
    <SectionCard
      icon={<Bolt />}
      primaryText={t("attributes.title", "Attributes")}
      secondaryText={t(
        "attributes.description",
        "AWS IoT Thing attributes for this node.",
      )}
      color="silver"
      variant="outline"
    >
      <AttributesSectionContent
        items={items}
        isLoading={isLoading}
        isError={isError}
        isRefetching={isRefetching}
        onRetry={() => void refetch()}
      />
    </SectionCard>
  );
}
