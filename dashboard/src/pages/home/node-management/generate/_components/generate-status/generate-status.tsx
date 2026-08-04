/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { ClipboardCheck, Download, RefreshCw } from "lucide-react";
import {
  AnimatedCard,
  Button,
  ProgressBar,
  Typography,
  SectionCard,
  ButtonGroup
} from "@espressif/dashboard-ui-components/components";
import type { GenerateStatusProps } from "./generate-status.props";

/**
 * Presents the node-generation status without owning any container. Rendered
 * inside a Dialog today, but decoupled so it can move to a Sheet later.
 */
export function GenerateStatus({
  status,
  errorMessage,
  downloaded,
  onDownload,
  onRegisterNodes,
  onRetry,
}: GenerateStatusProps) {
  const { t } = useTranslation(["generate", "common"]);

  if (status === "done") {
    return (
      <SectionCard
        variant="soft"
        color="mist"
        allowCollapse={false}
      >
        <AnimatedCard
          type="success"
          iconSize={96}
          actions={
            <ButtonGroup className="w-full">
              <Button
                variant="outline"
                startIcon={<Download className="h-4 w-4" />}
                onClick={onDownload}
                size="lg"
              >
                {t("common:actions.download", "Download")}
              </Button>
              <Button
                startIcon={<ClipboardCheck className="h-4 w-4" />}
                onClick={onRegisterNodes}
                disabled={!downloaded}
                tooltip={
                  downloaded
                    ? undefined
                    : t(
                        "status.registerHint",
                        "Download the package first",
                      )
                }
                size="lg"
              >
                {t("status.register", "Register nodes")}
              </Button>
            </ButtonGroup>
          }
        >
          <div className="flex flex-col gap-1 text-center">
            <Typography variant="h3">
              {t(
                "status.doneTitle",
                "Test nodes generated successfully",
              )}
            </Typography>
            <Typography
              variant="body1"
              className="text-muted-foreground tracking-tight"
            >
              {t(
                "status.doneDescription",
                "You can now download the generated node certificates.",
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
            <Button
              startIcon={<RefreshCw className="h-4 w-4" />}
              onClick={onRetry}
              size="lg"
              color="gray"
            >
              {t("common:actions.tryAgain", "Try again")}
            </Button>
          }
        >
          <div className="flex flex-col gap-1 text-center">
            <Typography variant="h3">
              {t("status.errorTitle", "Generation failed")}
            </Typography>
            <Typography
              variant="body1"
              className="text-muted-foreground tracking-tight"
            >
              {errorMessage ||
                t("status.errorFallback", "Generation failed.")}
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
            {t("status.generatingTitle", "Generating test nodes")}
          </Typography>
          <Typography
            variant="body1"
            className="text-muted-foreground tracking-tight"
          >
            {t(
              "status.generatingDescription",
              "This may take a while. Do not close this window.",
            )}
          </Typography>
        </div>
        <ProgressBar showFakeProgress className="w-full" />
      </div>
    </SectionCard>
  );
}
