/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { Download, Rocket } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";
import {
  Button,
  ButtonGroup,
} from "@espressif/dashboard-ui-components/components";
import { getFirmwareDownloadUrl } from "@/aws/services/firmware-upload.service";
import type { OtaImagesRowActionsProps } from "./ota-images-row-actions.props";

async function triggerDownload(imageKey: string, name: string) {
  const url = await getFirmwareDownloadUrl(imageKey);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = name;
  document.body.appendChild(anchor);
  anchor.click();
  document.body.removeChild(anchor);
}

/**
 * Hover-revealed row actions. Download is wired to a presigned S3 URL; Start
 * OTA deep-links into the Create OTA Job flow with the firmware's S3 key so the
 * new-job form pre-selects this firmware.
 */
export function OtaImagesRowActions({ imageKey, name }: OtaImagesRowActionsProps) {
  const { t } = useTranslation(["ota-images", "common"]);
  const navigate = useNavigate();

  const handleStartOta = useCallback(() => {
    const firmwareKey = imageKey.trim();
    if (!firmwareKey) {
      void navigate({ to: "/home/ota/jobs/new" });
      return;
    }
    void navigate({
      to: "/home/ota/jobs/new",
      search: { firmware_key: firmwareKey },
    });
  }, [imageKey, navigate]);

  return (
    <div
      className="opacity-0 group-hover:opacity-100 focus-within:opacity-100 transition-opacity"
      onClick={(e) => e.stopPropagation()}
    >
      <ButtonGroup>
        <Button
          type="button"
          color="gray"
          variant="outline"
          size="sm"
          fullWidth={false}
          onClick={handleStartOta}
          startIcon={<Rocket className="h-4 w-4" aria-hidden />}
          aria-label={t("columns.startOta", "Start OTA")}
        >
          {t("columns.startOta", "Start OTA")}
        </Button>
        <Button
          type="button"
          color="primary"
          variant="outline"
          size="sm"
          fullWidth={false}
          startIcon={<Download className="h-4 w-4" aria-hidden />}
          onClick={() => void triggerDownload(imageKey, name)}
          aria-label={t("common:actions.download", "Download")}
        >
          {t("common:actions.download", "Download")}
        </Button>
      </ButtonGroup>
    </div>
  );
}
