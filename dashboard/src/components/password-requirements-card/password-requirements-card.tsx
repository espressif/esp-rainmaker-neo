/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  RequirementList,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import type { PasswordRequirementsCardProps } from "./password-requirements-card.props";

/**
 * The "Password requirements" checklist rendered beneath every new-password input —
 * the change-password form, the first-time set-password form, and the reset flow.
 *
 * The heading and met/unmet labels are identical across those flows, so this
 * component owns the translations (`common:passwordRequirements.*`) rather than
 * asking each caller to pass them in. Callers only supply the current pass/fail
 * `items`, derived from `evaluatePasswordPolicy`.
 */
export default function PasswordRequirementsCard({
  items,
  className = "mt-4",
}: PasswordRequirementsCardProps) {
  const { t } = useTranslation("common");

  return (
    <SectionCard
      className={className}
      primaryText={t("passwordRequirements.label", "Password requirements")}
      allowCollapse={false}
      color="silver"
      variant="soft"
      size="sm"
    >
      <RequirementList
        items={items}
        metLabel={t("passwordRequirements.met", "Requirement met")}
        unmetLabel={t("passwordRequirements.unmet", "Requirement not met")}
      />
    </SectionCard>
  );
}
