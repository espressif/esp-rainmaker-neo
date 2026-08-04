/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { RefreshCw, ShieldCheck, Trash2 } from "lucide-react";
import {
  Button,
  ConfirmationDialog,
} from "@espressif/dashboard-ui-components/components";
import { isSandboxNumberVerified } from "@/config/sandbox-number-status.config";
import { cn } from "@/utils/utils";
import VerifySandboxNumberForm from "../verify-sandbox-number-form";
import type { SandboxNumberRowActionsProps } from "./sandbox-number-row-actions.props";

export default function SandboxNumberRowActions({
  phoneNumber,
  status,
  isResending,
  isDeleting,
  isVerifying,
  onResend,
  onStartVerify,
  onCancelVerify,
  onVerifySubmit,
  onVerified,
  onDelete,
  onCancelDelete,
}: SandboxNumberRowActionsProps) {
  const { t } = useTranslation(["post-deployment", "common"]);
  const isVerified = isSandboxNumberVerified(status);

  // Hiding a control mid-request would strand the user with no feedback, so anything
  // in flight (or an open code form) pins the row's actions visible.
  const isBusy = isResending || isDeleting || isVerifying;

  return (
    <div className="flex flex-col items-end gap-2">
      <div
        className={cn(
          "flex items-center justify-end gap-2 transition-opacity",
          "group-hover:opacity-100 focus-within:opacity-100",
          isBusy ? "opacity-100" : "opacity-0",
        )}
      >
        {!isVerified && (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            color="gray"
            fullWidth={false}
            loading={isResending}
            startIcon={<RefreshCw className="h-4 w-4" aria-hidden />}
            tooltip={t("smsSandbox.resendCodeTooltip", "AWS texts the code when a number is added; this sends a fresh one (up to 5 times in 24 hours).")}
            onClick={onResend}
          >
            {t("smsSandbox.resendCode", "Resend code")}
          </Button>
        )}

        {!isVerified && !isVerifying && (
          <Button
            type="button"
            size="sm"
            variant="outline"
            fullWidth={false}
            startIcon={<ShieldCheck className="h-4 w-4" aria-hidden />}
            onClick={onStartVerify}
          >
            {t("smsSandbox.verify", "Verify")}
          </Button>
        )}

        <ConfirmationDialog
          title={t("smsSandbox.deleteTitle", "Remove destination number")}
          description={t("smsSandbox.deleteDescription", "{{phoneNumber}} will stop receiving SMS from this deployment. You can add it again and verify it with a new code.", { phoneNumber })}
          confirmButtonText={t("common:actions.remove", "Remove")}
          cancelButtonText={t("common:actions.cancel", "Cancel")}
          isLoading={isDeleting}
          onConfirm={onDelete}
          onCancel={onCancelDelete}
        >
          <Button
            type="button"
            size="icon"
            variant="ghost"
            color="error"
            fullWidth={false}
            tooltip={t("smsSandbox.delete", "Remove number")}
            aria-label={t("smsSandbox.delete", "Remove number")}
          >
            <Trash2 className="h-4 w-4" aria-hidden />
          </Button>
        </ConfirmationDialog>
      </div>

      {/* Outside the hover wrapper on purpose: once the code form is open it must stay
          on screen when the pointer leaves the row. */}
      {isVerifying && (
        <VerifySandboxNumberForm
          onSubmit={onVerifySubmit}
          onSuccess={onVerified}
          onCancel={onCancelVerify}
        />
      )}
    </div>
  );
}
