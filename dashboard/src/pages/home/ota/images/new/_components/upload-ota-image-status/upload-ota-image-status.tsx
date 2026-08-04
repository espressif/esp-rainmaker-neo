/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { ArrowLeft, Pencil, Rocket } from "lucide-react";
import {
  AnimatedCard,
  Button,
  ButtonGroup,
  ProgressBar,
  SectionCard,
  Typography,
} from "@espressif/dashboard-ui-components/components";
import { OtaImageNameCell } from "../../../_components/ota-image-name-cell";
import type { UploadOtaImageStatusProps } from "./upload-ota-image-status.props";

/**
 * Presents the OTA image upload status without owning any container. Rendered
 * inside a Dialog today, but decoupled so it can move to a Sheet later.
 */
export function UploadOtaImageStatus({
  status,
  result,
  errorMessage,
  onBackToImages,
  onCreateOta,
  onEditAndRetry,
}: UploadOtaImageStatusProps) {
  const { t } = useTranslation(["ota-images", "common"]);

  if (status === "done") {
    return (
      <SectionCard variant="soft" color="mist" allowCollapse={false}>
        <AnimatedCard
          type="success"
          iconSize={96}
          actions={
            <ButtonGroup className="w-full">
              <Button
                startIcon={<Rocket className="h-4 w-4" />}
                onClick={onCreateOta}
                size="lg"
                color="success"
              >
                {t(
                  "upload.createOtaWithImage",
                  "Create OTA with this image",
                )}
              </Button>
              <Button
                variant="outline"
                startIcon={<ArrowLeft className="h-4 w-4" />}
                onClick={onBackToImages}
                size="lg"
                color="secondary"
              >
                {t("backToImages", "Back to OTA Images")}
              </Button>
            </ButtonGroup>
          }
        >
          <div className="flex w-full flex-col gap-4">
            <div className="flex flex-col gap-2 text-center">
              <Typography variant="h3">
                {t(
                  "upload.successTitle",
                  "OTA Image uploaded successfully.",
                )}
              </Typography>
              <Typography
                variant="body1"
                className="text-muted-foreground tracking-tight"
              >
                {t(
                  "upload.successDescription",
                  "You can now use this OTA image to update your devices.",
                )}
              </Typography>
              {result ? (
                <SectionCard size="sm" allowCollapse={false} variant="soft" color="info">
                  <OtaImageNameCell
                    name={result.name}
                    size={result.fileSize}
                    md5={result.md5}
                    fwType={result.fwType}
                  />
                </SectionCard>
              ) : null}
            </div>
          </div>
        </AnimatedCard>
      </SectionCard>
    );
  }

  if (status === "error") {
    return (
      <SectionCard
        variant="soft"
        color="error"
        className="border !border-error/20"
        allowCollapse={false}
      >
        <AnimatedCard
          type="error"
          iconSize={96}
          actions={
            <ButtonGroup className="w-full">
              <Button
                startIcon={<ArrowLeft className="h-4 w-4" />}
                onClick={onBackToImages}
                size="lg"
                color="secondary"
                variant="outline"
              >
                {t("backToImages", "Back to OTA Images")}
              </Button>
              <Button
                startIcon={<Pencil className="h-4 w-4" />}
                onClick={onEditAndRetry}
                size="lg"
                color="gray"
              >
                {t("common:actions.editAndRetry", "Edit & Retry")}
              </Button>
            </ButtonGroup>
          }
        >
          <div className="flex flex-col gap-1 text-center">
            <Typography variant="h3">
              {t(
                "upload.errorTitle",
                "Failed to upload OTA image.",
              )}
            </Typography>
            <Typography
              variant="body1"
              className="text-muted-foreground tracking-tight"
            >
              {errorMessage ||
                t(
                  "upload.errorDescription",
                  "Please try again.",
                )}
            </Typography>
          </div>
        </AnimatedCard>
      </SectionCard>
    );
  }

  return (
    <SectionCard
      variant="soft"
      color="info"
      className="border !border-info/20"
      allowCollapse={false}
    >
      <div className="flex flex-col gap-3 py-2">
        <div className="flex flex-col gap-1">
          <Typography variant="h3">
            {t("upload.uploadingTitle", "Uploading OTA image")}
          </Typography>
          <Typography
            variant="body1"
            className="text-muted-foreground tracking-tight"
          >
            {t(
              "upload.uploadingDescription",
              "This may take a while. Do not close this window.",
            )}
          </Typography>
        </div>
        <ProgressBar showFakeProgress className="w-full" />
      </div>
    </SectionCard>
  );
}
