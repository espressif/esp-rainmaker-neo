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
import type { CreateNodeGroupStatusProps } from "./create-node-group-status.props";

/**
 * Presents the node group creation status without owning any container.
 * Rendered inside a Dialog today, but decoupled so it can move to a Sheet later.
 */
export function CreateNodeGroupStatus({
  status,
  errorMessage,
  onBackToGroups,
  onViewGroupDetails,
  onEditAndRetry,
}: CreateNodeGroupStatusProps) {
  const { t } = useTranslation(["node-groups", "common"]);

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
                onClick={onBackToGroups}
                size="lg"
                color="success"
              >
                {t("new.backToGroups", "Back to node groups")}
              </Button>
              <Button
                variant="outline"
                startIcon={<Eye className="h-4 w-4" />}
                onClick={onViewGroupDetails}
                size="lg"
                color="secondary"
              >
                {t("new.status.viewDetails", "View node group details")}
              </Button>
            </ButtonGroup>
          }
        >
          <div className="flex flex-col gap-1 text-center">
            <Typography variant="h3">
              {t(
                "new.status.successTitle",
                "Node group created successfully.",
              )}
            </Typography>
            <Typography
              variant="body1"
              className="text-muted-foreground tracking-tight"
            >
              {t(
                "new.status.successDescription",
                "Your node group is ready. Open its details to add nodes or start an OTA job.",
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
                onClick={onBackToGroups}
                size="lg"
                color="secondary"
                variant="outline"
              >
                {t("new.backToGroups", "Back to node groups")}
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
              {t("new.status.errorTitle", "Failed to create node group.")}
            </Typography>
            <Typography
              variant="body1"
              className="text-muted-foreground tracking-tight"
            >
              {errorMessage ||
                t("new.status.errorDescription", "Please try again.")}
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
            {t("new.status.creatingTitle", "Creating node group")}
          </Typography>
          <Typography
            variant="body1"
            className="text-muted-foreground tracking-tight"
          >
            {t(
              "new.status.creatingDescription",
              "This may take a while. Do not close this window.",
            )}
          </Typography>
        </div>
        <ProgressBar showFakeProgress className="w-full" />
      </div>
    </SectionCard>
  );
}
