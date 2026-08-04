/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Cpu } from "lucide-react";
import {
  ContentContainer,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import AddGroupNodesButton from "../add-group-nodes-button";
import type { GroupNodesCardShellProps } from "./group-nodes-card-shell.props";

/**
 * Card chrome for the group Nodes tab. Extracted so the content can pick its branch with early
 * returns without duplicating the container, heading and actions in each one.
 */
export default function GroupNodesCardShell({
  isDynamic,
  children,
}: GroupNodesCardShellProps) {
  const { t } = useTranslation("node-groups");

  return (
    <ContentContainer maxWidth="lg" noGutters>
      <SectionCard
        icon={<Cpu className="h-5 w-5" />}
        primaryText={t("details.nodes.title", "Nodes in this group")}
        secondaryText={t(
          "details.nodes.description",
          "Nodes assigned to this group, including sub-groups.",
        )}
        color="silver"
        variant="outline"
        actions={isDynamic ? undefined : <AddGroupNodesButton />}
        allowCollapse={false}
      >
        {children}
      </SectionCard>
    </ContentContainer>
  );
}
