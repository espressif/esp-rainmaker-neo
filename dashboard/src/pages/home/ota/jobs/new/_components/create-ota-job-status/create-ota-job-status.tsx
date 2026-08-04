/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { ArrowLeft, Eye, Pencil } from "lucide-react";
import {
  AnimatedCard,
  Button,
  ButtonGroup,
  ProgressBar,
  SectionCard,
  Typography,
} from "@espressif/dashboard-ui-components/components";
import type { CreateOtaJobStatusProps } from "./create-ota-job-status.props";

/**
 * Presents the OTA job creation status without owning any container. Rendered
 * inside a Dialog today, but decoupled so it can move to a Sheet later.
 */
export function CreateOtaJobStatus({
  status,
  errorMessage,
  onBackToJobs,
  onViewJobDetails,
  onEditAndRetry,
}: CreateOtaJobStatusProps) {
  const { t } = useTranslation(["ota-jobs", "common"]);

  if (status === "done") {
    return (
      <SectionCard variant="soft" color="mist" allowCollapse={false}>
        <AnimatedCard
          type="success"
          iconSize={96}
          actions={
            <ButtonGroup className="w-full">
              <Button
                startIcon={<ArrowLeft className="h-4 w-4" />}
                onClick={onBackToJobs}
                size="lg"
                color="success"
              >
                {t("createOtaJobPage.backToJobs", "Back to OTA Jobs")}
              </Button>
              <Button
                variant="outline"
                startIcon={<Eye className="h-4 w-4" />}
                onClick={onViewJobDetails}
                size="lg"
                color="secondary"
              >
                {t("createOtaJobPage.status.viewJobDetails", "View OTA job details")}
              </Button>
            </ButtonGroup>
          }
        >
          <div className="flex flex-col gap-1 text-center">
            <Typography variant="h3">
              {t(
                "createOtaJobPage.status.successTitle",
                "OTA job created successfully.",
              )}
            </Typography>
            <Typography
              variant="body1"
              className="text-muted-foreground tracking-tight"
            >
              {t(
                "createOtaJobPage.status.successDescription",
                "The rollout has started. Track its progress from the job details.",
              )}
            </Typography>
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
                onClick={onBackToJobs}
                size="lg"
                color="secondary"
                variant="outline"
              >
                {t("createOtaJobPage.backToJobs", "Back to OTA Jobs")}
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
                "createOtaJobPage.status.errorTitle",
                "Failed to create OTA job.",
              )}
            </Typography>
            <Typography
              variant="body1"
              className="text-muted-foreground tracking-tight"
            >
              {errorMessage ||
                t("createOtaJobPage.status.errorDescription", "Please try again.")}
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
            {t("createOtaJobPage.status.creatingTitle", "Creating OTA job")}
          </Typography>
          <Typography
            variant="body1"
            className="text-muted-foreground tracking-tight"
          >
            {t(
              "createOtaJobPage.status.creatingDescription",
              "This may take a while. Do not close this window.",
            )}
          </Typography>
        </div>
        <ProgressBar showFakeProgress className="w-full" />
      </div>
    </SectionCard>
  );
}
