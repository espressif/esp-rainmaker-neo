/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface AddNodeToGroupButtonProps {
  /** Group the node is being added to. */
  groupName: string;
  /** Node (AWS IoT thing) to add. */
  thingName: string;
}
