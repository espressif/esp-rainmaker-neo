/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Trash2, Upload } from "lucide-react";
import {
  Button,
  ConfirmationDialog,
  toast,
} from "@espressif/dashboard-ui-components/components";
import { useDeleteBrandMutation } from "@/api/branding";
import { getBrandingErrorMessage } from "../../branding.utils";
import type { BrandActionsProps } from "./brand-actions.props";

const ICON_CLASS = "h-4 w-4 shrink-0";

export default function BrandActions({ onReplace }: BrandActionsProps) {
  const { t } = useTranslation(["branding", "common"]);
  const deleteMutation = useDeleteBrandMutation();

  // Errors are surfaced as a toast rather than rethrown so the dialog closes.
  const handleDelete = async () => {
    try {
      await deleteMutation.mutateAsync();
      toast.success(
        t(
          "delete.success",
          "Brand deleted. Phones go back to the default look from the launch after they next open the app.",
        ),
      );
    } catch (error) {
      toast.error(
        getBrandingErrorMessage(
          error,
          t,
          t("delete.failed", "Could not delete the brand. Try again."),
        ),
      );
    }
  };

  return (
    <div className="flex flex-wrap items-center gap-2">
      <ConfirmationDialog
        title={t("delete.confirmTitle", "Delete the brand?")}
        description={t(
          "delete.confirmDescription",
          "Phones pick this up the next time ESP RainMaker Home opens and go back to its default look from the launch after.",
        )}
        confirmButtonText={t("common:actions.delete", "Delete")}
        confirmButtonColor="error"
        onConfirm={handleDelete}
        onCancel={() => {}}
        isLoading={deleteMutation.isPending}
      >
        <Button
          type="button"
          variant="outline"
          color="error"
          fullWidth={false}
          startIcon={<Trash2 className={ICON_CLASS} aria-hidden />}
        >
          {t("actions.delete", "Delete brand")}
        </Button>
      </ConfirmationDialog>
      <Button
        type="button"
        variant="default"
        fullWidth={false}
        startIcon={<Upload className={ICON_CLASS} aria-hidden />}
        onClick={onReplace}
      >
        {t("actions.replace", "Replace brand")}
      </Button>
    </div>
  );
}
