/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { TableRowDetailSheet } from "@/components/table-row-detail-sheet";
import OtaNodeExecutionDetails from "../ota-node-execution-details";
import type { OtaNodeExecutionDetailSheetProps } from "./ota-node-execution-detail-sheet.props";

export default function OtaNodeExecutionDetailSheet({
  jobId,
  thingName,
  onClose,
}: OtaNodeExecutionDetailSheetProps) {
  const { t } = useTranslation("ota-jobs");

  return (
    <TableRowDetailSheet
      contentClassName="w-full max-w-2xl"
      label={t("details.nodes.executionDetail.title", "Node execution details")}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <OtaNodeExecutionDetails jobId={jobId} thingName={thingName} />
    </TableRowDetailSheet>
  );
}
