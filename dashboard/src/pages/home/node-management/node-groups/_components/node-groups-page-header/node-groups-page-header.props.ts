/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { NodeGroupsSearchField } from "@/api/node-groups";

export interface NodeGroupsSearchValue {
  id: NodeGroupsSearchField;
  value: string;
}

export interface NodeGroupsPageHeaderProps {
  onSearch: (query: NodeGroupsSearchValue | null) => void;
  onCreateClick: () => void;
}
