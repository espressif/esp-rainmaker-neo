/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import {
  Button,
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@espressif/dashboard-ui-components/components";
import type { QueryRule } from "../../query-rule-builder.props";
import { QueryRuleForm } from "../query-rule-form";
import type { QueryRulePopoverProps } from "./query-rule-popover.props";

export default function QueryRulePopover({ onAdd }: QueryRulePopoverProps) {
  const { t } = useTranslation("common");
  const [open, setOpen] = useState(false);

  const handleSubmit = (rule: QueryRule) => {
    onAdd(rule);
    setOpen(false);
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          size="sm"
          variant="link"
          fullWidth={false}
          startIcon={<Plus className="h-4 w-4 shrink-0" aria-hidden />}
        >
          {t("queryRuleBuilder.addRule", "Add rule")}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80">
        <QueryRuleForm onSubmit={handleSubmit} onClear={() => setOpen(false)} />
      </PopoverContent>
    </Popover>
  );
}
