/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Copy } from "lucide-react";
import {
  Alert,
  Button,
  toast,
} from "@espressif/dashboard-ui-components/components";
import { cn } from "@/utils/utils";
import { QueryRulesListContent } from "./_components/query-rules-list-content";
import type { QueryRulesListProps } from "./query-rules-list.props";
import {
  buildQueryRuleItems,
  resolveCombinatorCaption,
} from "./query-rules-list.utils";

/**
 * Read-only view of an AWS IoT fleet-index query string: the rules that define a
 * dynamic group's membership, rendered as a label/value list with the raw query
 * available to copy. Field labels and types come from the shared
 * `advancedSearchFieldsData` catalog.
 */
export default function QueryRulesList({
  queryString,
  className,
}: QueryRulesListProps) {
  const { t } = useTranslation("common");

  const parsedRules = useMemo(() => {
    if (!queryString?.trim()) {
      return null;
    }
    return buildQueryRuleItems(queryString, t);
  }, [queryString, t]);

  const handleCopyQueryString = async (value: string) => {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(
        t("queryRulesList.copySuccess", "Query string copied to clipboard"),
      );
    } catch {
      toast.error(t("queryRulesList.copyError", "Failed to copy query string"));
    }
  };

  if (!queryString?.trim()) {
    return (
      <Alert type="info" color="gray" variant="soft" hideIcon>
        {t("queryRulesList.emptyState", "No query rules defined")}
      </Alert>
    );
  }

  const combinatorCaption = resolveCombinatorCaption(parsedRules, t);

  return (
    <div className={cn("flex flex-col gap-3", className)}>
      <Alert
        color="info"
        hideIcon
        title={t("queryRulesList.title", "Query rules")}
        description={
          combinatorCaption && (
            <span className="text-xs">{combinatorCaption}</span>
          )
        }
      />
      <QueryRulesListContent
        queryString={queryString}
        parsedRules={parsedRules}
      />
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="self-start"
        startIcon={<Copy className="h-4 w-4" />}
        onClick={() => void handleCopyQueryString(queryString)}
      >
        {t("queryRulesList.copyQueryString", "Copy query string")}
      </Button>
    </div>
  );
}
