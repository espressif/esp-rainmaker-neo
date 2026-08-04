/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { SectionCard } from "@espressif/dashboard-ui-components/components";
import DeploymentValueContent from "./_components/deployment-value-content";
import type { DeploymentValueCardProps } from "./deployment-value-card.props";

/** One account limit, reported and never changed from here. */
export default function DeploymentValueCard({
  value,
}: DeploymentValueCardProps) {
  const { t } = useTranslation("post-deployment");
  const { Icon } = value;

  return (
    <SectionCard
      allowCollapse={false}
      size="default"
      variant="soft"
      color="silver"
      icon={<Icon className="h-6 w-6" />}
      primaryText={t(value.titleKey)}
    >
      <DeploymentValueContent value={value} />
    </SectionCard>
  );
}
