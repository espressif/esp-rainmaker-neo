/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType, ReactNode } from "react";
import { Link } from "@tanstack/react-router";

export type RouterLinkComponentProps = {
  to: string;
  className?: string;
  children: ReactNode;
};

export type RouterLinkComponent = ComponentType<RouterLinkComponentProps>;

/** TanStack Router `Link` cast for dashboard-ui-components `LinkComponent` / `linkComponent` props. */
export const TanstackRouterLink = Link as unknown as RouterLinkComponent;
