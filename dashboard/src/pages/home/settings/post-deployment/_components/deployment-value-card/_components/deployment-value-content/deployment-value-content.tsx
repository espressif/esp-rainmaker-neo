/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import FixedValueContent from "../fixed-value-content";
import LookedUpValueContent from "../looked-up-value-content";
import type { DeploymentValueContentProps } from "./deployment-value-content.props";

/**
 * Picks the body for one limit. Split out of the card so each branch is its own
 * component: only the looked-up branch mounts a query hook, so the fixed limits
 * never subscribe to anything.
 */
export default function DeploymentValueContent({
  value,
}: DeploymentValueContentProps) {
  if (value.credsFree) {
    return <FixedValueContent value={value} />;
  }

  return <LookedUpValueContent value={value} />;
}
