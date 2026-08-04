/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  ScrollableSections,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import DeploymentSectionContent from "../deployment-section-content";
import { POST_DEPLOYMENT_SECTIONS } from "../../post-deployment.constants";
import { buildPostDeploymentSections } from "./post-deployment-sections.config";

/**
 * Enough to clear PageContainer's sticky heading band, which holds the production
 * note (~4rem tall).
 *
 * Measured from the top of the scrollport, not the window: the app's fixed header
 * sits outside `<main class="overflow-y-auto">`, so it is already excluded and must
 * not be added in here.
 */
const STICKY_TOP = "5.5rem";

export default function PostDeploymentSections() {
  const { t } = useTranslation("post-deployment");
  const sections = useMemo(() => buildPostDeploymentSections(t), [t]);

  return (
    <ScrollableSections
      defaultValue={POST_DEPLOYMENT_SECTIONS.ses}
      stickyTop={STICKY_TOP}
      className="w-full"
    >
      <ScrollableSections.Tabs>
        {sections.map(({ id, Icon, label }) => (
          <ScrollableSections.Tab key={id} id={id} label={label}>
            <span className="flex items-center gap-2">
              <Icon className="h-4 w-4 shrink-0" aria-hidden />
              <span>{label}</span>
            </span>
          </ScrollableSections.Tab>
        ))}
      </ScrollableSections.Tabs>

      {sections.map(({ id, Icon, label, description }) => (
        <ScrollableSections.Content key={id} id={id}>
          <SectionCard
            allowCollapse={false}
            size="lg"
            variant="outline"
            color="silver"
            icon={<Icon className="h-6 w-6" />}
            primaryText={label}
            secondaryText={description}
          >
            <DeploymentSectionContent section={id} />
          </SectionCard>
        </ScrollableSections.Content>
      ))}
    </ScrollableSections>
  );
}
