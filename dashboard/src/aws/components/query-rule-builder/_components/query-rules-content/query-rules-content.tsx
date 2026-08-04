/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  Alert,
  Button,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@espressif/dashboard-ui-components/components";
import { Trash2 } from "lucide-react";
import type { QueryRulesContentProps } from "./query-rules-content.props";

export default function QueryRulesContent({
  rules,
  onDelete,
}: QueryRulesContentProps) {
  const { t } = useTranslation("common");

  if (rules.length === 0) {
    return (
      <Alert type="info" color="gray" variant="soft" hideIcon>
        {t("queryRuleBuilder.emptyState", "No rules added yet")}
      </Alert>
    );
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t("queryRuleBuilder.table.type", "Type")}</TableHead>
          <TableHead>{t("common:columns.value", "Value")}</TableHead>
          <TableHead className="w-16 text-right">
            {t("queryRuleBuilder.table.action", "Action")}
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rules.map((rule, index) => (
          <TableRow key={rule.id} className="group">
            <TableCell className="font-medium">{rule.type}</TableCell>
            <TableCell>{rule.value}</TableCell>
            <TableCell className="text-right">
              <Button
                type="button"
                size="icon"
                variant="ghost"
                color="error"
                className="opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
                aria-label={t(
                  "queryRuleBuilder.table.deleteAriaLabel",
                  "Delete rule",
                )}
                onClick={() => onDelete(index)}
              >
                <Trash2 className="h-4 w-4" aria-hidden />
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
