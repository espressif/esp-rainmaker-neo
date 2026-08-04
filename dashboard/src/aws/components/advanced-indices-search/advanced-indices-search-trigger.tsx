/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { DialogTrigger } from "@espressif/dashboard-ui-components/components";

interface AdvancedIndicesSearchTriggerProps {
  children: React.ReactNode;
}

/**
 * Trigger for the AdvancedIndicesSearch dialog.
 * Wraps any child element, passing it the dialog open handler via asChild.
 * Must be used as a child of <AdvancedIndicesSearch>.
 *
 * Usage:
 * ```tsx
 * <AdvancedIndicesSearchTrigger>
 *   <Button>Filter</Button>
 * </AdvancedIndicesSearchTrigger>
 * ```
 */
export function AdvancedIndicesSearchTrigger({
  children,
}: AdvancedIndicesSearchTriggerProps) {
  return <DialogTrigger asChild>{children}</DialogTrigger>;
}
