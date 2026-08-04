/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Trans, useTranslation } from "react-i18next";
import { ExternalLink } from "lucide-react";
import {
  Alert,
  Button,
  DynamicList,
  InlineError,
  Spinner,
} from "@espressif/dashboard-ui-components/components";
import { stripOtaPrefix } from "@/aws/services/ota.service";
import {
  buildExecutionItems,
  buildExecutionMeta,
} from "../../ota-node-execution-details.utils";
import type { OtaNodeExecutionDetailsBodyProps } from "./ota-node-execution-details-body.props";

export default function OtaNodeExecutionDetailsBody({
  jobId,
  thingName,
  execution,
  isPending,
  isError,
}: OtaNodeExecutionDetailsBodyProps) {
  const { t } = useTranslation("ota-jobs");
  const navigate = useNavigate();

  const items = useMemo(
    () => (execution ? buildExecutionItems(execution) : []),
    [execution],
  );
  const meta = useMemo(
    () => (execution ? buildExecutionMeta(execution, t) : {}),
    [execution, t],
  );

  const handleViewDetails = () => {
    void navigate({
      to: "/home/node-management/nodes/$thingName/overview",
      params: { thingName },
    });
  };

  if (isPending) {
    return (
      <div className="flex min-h-[20vh] items-center justify-center">
        <Spinner />
      </div>
    );
  }

  if (isError) {
    return (
      <InlineError
        title={t(
          "details.nodes.executionDetail.error.title",
          "Failed to load execution details",
        )}
      >
        {t(
          "details.nodes.executionDetail.error.description",
          "An unexpected error occurred while loading this node's execution details. Please try again later.",
        )}
      </InlineError>
    );
  }

  if (!execution) {
    return (
      <Alert variant="soft" color="info" type="info" hideIcon>
        {t(
          "details.nodes.executionDetail.empty",
          "No execution details found for this node.",
        )}
      </Alert>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <Alert variant="soft" color="gray" hideIcon>
        <Trans
          t={t}
          i18nKey="details.nodes.executionDetail.contextInfo"
          values={{ nodeId: thingName, jobId: stripOtaPrefix(jobId) }}
          components={{ strong: <span className="font-semibold" /> }}
        />
      </Alert>
      <DynamicList
        items={items}
        meta={meta}
        direction="row"
        keyWidth={40}
        hideIcon
        simple
      />
      <div className="flex justify-end">
        <Button
          type="button"
          variant="link"
          color="primary"
          size="sm"
          fullWidth={false}
          startIcon={<ExternalLink className="h-4 w-4 shrink-0" />}
          onClick={handleViewDetails}
        >
          {t("details.nodes.executionDetail.viewDetails", "View node details")}
        </Button>
      </div>
    </div>
  );
}
