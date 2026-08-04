/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export type NodeTypeModelFilterValue = {
  type: string;
  model?: string;
};

export interface NodeTypeModelFilterProps {
  value: NodeTypeModelFilterValue | null;
  onChange: (value: NodeTypeModelFilterValue | null) => void;
}
