/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import DeploymentValueCard from "../deployment-value-card";
import SmsSandboxCard from "../sms-sandbox-card";
import { SMS_SANDBOX_CARD, VALUES } from "../../values.config";
import type { DeploymentSectionContentProps } from "./deployment-section-content.props";

/** The limits belonging to one AWS service, plus the SMS sandbox manager for SNS. */
export default function DeploymentSectionContent({
  section,
}: DeploymentSectionContentProps) {
  const values = useMemo(
    () => VALUES.filter((value) => value.section === section),
    [section],
  );

  return (
    <div className="space-y-4">
      {values.map((value) => (
        <DeploymentValueCard key={value.id} value={value} />
      ))}

      {section === SMS_SANDBOX_CARD.section && (
        <SmsSandboxCard
          Icon={SMS_SANDBOX_CARD.Icon}
          titleKey={SMS_SANDBOX_CARD.titleKey}
          noteKey={SMS_SANDBOX_CARD.noteKey}
        />
      )}
    </div>
  );
}
