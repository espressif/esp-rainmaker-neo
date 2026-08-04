/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { ChevronDown } from "lucide-react";
import { Alert, Button } from "@espressif/dashboard-ui-components/components";
import { QueryRulesPopover } from "@/aws/components/query-rule-builder";
import type { DynamicMembershipNoticeProps } from "./dynamic-membership-notice.props";

/**
 * Explains why a dynamic group's Nodes tab has no add/remove controls, with the governing query
 * rules one click away.
 */
export default function DynamicMembershipNotice({
  queryString,
}: DynamicMembershipNoticeProps) {
  const { t } = useTranslation("node-groups");

  return (
    <Alert
      type="info"
      variant="soft"
      className="mb-4"
      title={t(
        "details.nodes.dynamicNotice.title",
        "Membership is managed automatically",
      )}
      description={t(
        "details.nodes.dynamicNotice.description",
        "Nodes join and leave this group automatically based on its query rules, so they can't be added or removed manually.",
      )}
      actions={
        <QueryRulesPopover
          queryString={queryString}
          align="end"
          trigger={
            <Button
              type="button"
              variant="link"
              size="sm"
              color="primary"
              fullWidth={false}
              endIcon={<ChevronDown className="h-4 w-4" aria-hidden />}
            >
              {t("details.nodes.dynamicNotice.viewRules", "View rules")}
            </Button>
          }
        />
      }
    />
  );
}
