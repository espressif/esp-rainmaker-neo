/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Button } from "@espressif/dashboard-ui-components/components";
import { cn } from "@/utils/utils";
import type {
  FormFooterAction,
  FormFooterActionsProps,
} from "./form-footer-actions.props";

const ACTION_BUTTON_CLASS = "w-full sm:w-auto";

/**
 * Reusable footer action row for forms hosted in a sheet, dialog or page.
 * Destructive action sits left; soft (Cancel) + primary (submit) align right.
 * Buttons are full-width on mobile and auto-width from the `sm` breakpoint.
 */
export default function FormFooterActions({
  destructiveAction,
  softAction,
  primaryAction,
  className,
}: FormFooterActionsProps) {
  return (
    <div
      className={cn(
        "flex w-full flex-col gap-3 border-t border-border pt-4 sm:flex-row sm:items-center sm:justify-between",
        className,
      )}
    >
      <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:flex-wrap">
        {destructiveAction}
      </div>

      <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center sm:justify-end">
        {softAction ? renderAction(softAction, "mist") : null}
        {renderAction(primaryAction, "primary")}
      </div>
    </div>
  );
}

function renderAction(action: FormFooterAction, color: "mist" | "primary") {
  return (
    <Button
      type={action.type ?? "button"}
      variant="default"
      color={color}
      size="lg"
      fullWidth={false}
      className={ACTION_BUTTON_CLASS}
      startIcon={action.startIcon}
      onClick={action.onClick}
      disabled={action.disabled}
      loading={action.loading}
    >
      {action.label}
    </Button>
  );
}
