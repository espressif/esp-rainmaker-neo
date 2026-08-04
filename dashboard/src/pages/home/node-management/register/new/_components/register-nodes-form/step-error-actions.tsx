/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { RotateCwIcon, XIcon } from "lucide-react";
import { Button, ButtonGroup } from "@espressif/dashboard-ui-components/components";

export interface StepErrorActionsProps {
  onRetry: () => void;
  onClose: () => void;
  retryLabel: string;
  closeLabel: string;
}

export function StepErrorActions({
  onRetry,
  onClose,
  retryLabel,
  closeLabel,
}: StepErrorActionsProps) {
  return (
    <ButtonGroup className="w-full">
      <Button
        size="sm"
        variant="outline"
        startIcon={<RotateCwIcon />}
        onClick={onRetry}
        color="gray"
      >
        {retryLabel}
      </Button>
      <Button
        size="sm"
        variant="outline"
        startIcon={<XIcon />}
        onClick={onClose}
        color="gray"
      >
        {closeLabel}
      </Button>
    </ButtonGroup>
  );
}
