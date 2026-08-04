/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Loader2, Shield, Cpu, User } from "lucide-react";
import {
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  Badge,
  IconAvatar,
} from "@espressif/dashboard-ui-components/components";
import type { FieldType, IndexField } from "../advanced-indices-search.types";
import { fieldLabel } from "../field-label";
import { FIELD_TYPE_ICONS } from "../field-type-icons";

function FieldTypeIcon({ type }: { type: FieldType }) {
  const Icon = FIELD_TYPE_ICONS[type];
  return <Icon className="h-3 w-3" />;
}

const TAG_SOURCES = [
  {
    key: "admin",
    labelKey: "nodes:advancedIndicesSearch.fieldSelector.tagSources.admin",
    labelFallback: "Admin Tag",
    prefix: "shadow.name.iparams.reported.data.admin.t.",
    icon: <Shield className="h-3 w-3" />,
  },
  {
    key: "device",
    labelKey: "nodes:advancedIndicesSearch.fieldSelector.tagSources.device",
    labelFallback: "Device Tag",
    prefix: "shadow.name.iparams.reported.data.device.t.",
    icon: <Cpu className="h-3 w-3" />,
  },
  {
    key: "user",
    labelKey: "nodes:advancedIndicesSearch.fieldSelector.tagSources.user",
    labelFallback: "User Tag",
    prefix: "shadow.name.iparams.reported.data.user.t.",
    icon: <User className="h-3 w-3" />,
  },
];

interface FieldSelectorProps {
  fields: IndexField[];
  fieldFilter: string;
  isLoading: boolean;
  onSelect: (field: IndexField) => void;
}

export function FieldSelector({
  fields,
  fieldFilter,
  isLoading,
  onSelect,
}: FieldSelectorProps) {
  const { t } = useTranslation("nodes");
  const filteredFields = fields.filter((f) => {
    const q = fieldFilter.toLowerCase();
    return (
      f.name.toLowerCase().includes(q) ||
      (fieldLabel(f, t).toLowerCase().includes(q) ?? false)
    );
  });

  if (isLoading) {
    return (
      <div className="flex items-center justify-center gap-2 py-6 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        {t("advancedIndicesSearch.fieldSelector.loadingFields", "Loading fields...")}
      </div>
    );
  }

  const trimmed = fieldFilter.trim();
  const hasExactMatch = filteredFields.some(
    (f) => fieldLabel(f, t).toLowerCase() === trimmed.toLowerCase() || f.name === trimmed,
  );
  const showCustomOptions = trimmed.length > 0 && !hasExactMatch;

  if (filteredFields.length === 0 && !showCustomOptions) {
    return (
      <div className="py-6 text-center text-sm text-muted-foreground">
        {t("advancedIndicesSearch.fieldSelector.noFieldsMatch", 'No fields match "{{query}}"', { query: fieldFilter })}
      </div>
    );
  }

  return (
    <DropdownMenuGroup>
      {filteredFields.map((field) => (
        <DropdownMenuItem
          key={field.name}
          className="justify-between"
          onSelect={(e) => {
            e.preventDefault();
            onSelect(field);
          }}
        >
          <div className="flex items-center gap-2 min-w-0">
            <IconAvatar size={28} variant="soft" className="shrink-0">
              {field.icon}
            </IconAvatar>
            <div className="flex flex-col min-w-0">
              <span className="truncate text-sm text-foreground">
                {fieldLabel(field, t)}
              </span>
              {field.label && (
                <span className="truncate text-xs text-muted-foreground">
                  {field.name}
                </span>
              )}
            </div>
          </div>
          <Badge
            variant="outline"
            className="shrink-0 gap-1 text-[9px] tracking-wide font-normal text-muted-foreground uppercase rounded-md p-1"
          >
            <FieldTypeIcon type={field.type} />
            {field.type}
          </Badge>
        </DropdownMenuItem>
      ))}
      {showCustomOptions && (
        <>
          {filteredFields.length > 0 && <DropdownMenuSeparator />}
          <DropdownMenuLabel className="text-xs text-muted-foreground">
            {t("advancedIndicesSearch.fieldSelector.searchByCustomTag", 'Search by custom tag "{{query}}"', { query: trimmed })}
          </DropdownMenuLabel>
          {TAG_SOURCES.map((source) => {
            const sourceLabel = t(source.labelKey, source.labelFallback);
            return (
            <DropdownMenuItem
              key={source.key}
              className="justify-between"
              onSelect={(e) => {
                e.preventDefault();
                onSelect({
                  name: source.prefix + trimmed,
                  type: "String",
                  label: `${trimmed} (${sourceLabel})`,
                  icon: source.icon,
                });
              }}
            >
              <div className="flex items-center gap-2 min-w-0">
                <IconAvatar size={28} variant="soft" className="shrink-0">
                  {source.icon}
                </IconAvatar>
                <div className="flex flex-col min-w-0">
                  <span className="truncate text-sm text-foreground">
                    {sourceLabel}: {trimmed}
                  </span>
                </div>
              </div>
              <Badge
                variant="outline"
                className="shrink-0 gap-1 text-[9px] tracking-wide font-normal text-muted-foreground uppercase rounded-md p-1"
              >
                <FieldTypeIcon type="String" />
                String
              </Badge>
            </DropdownMenuItem>
            );
          })}
        </>
      )}
    </DropdownMenuGroup>
  );
}
