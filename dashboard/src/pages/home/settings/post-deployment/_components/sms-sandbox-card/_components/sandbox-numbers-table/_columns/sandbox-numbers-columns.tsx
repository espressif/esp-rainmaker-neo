/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ColumnDef } from "@tanstack/react-table";
import type { SMSSandboxPhoneNumber } from "@aws-sdk/client-sns";
import type { TFunction } from "i18next";
import { SandboxNumberStatusBadge } from "@/components/sms-sandbox/sandbox-number-status-badge";
import SandboxNumberRowActions from "../../sandbox-number-row-actions";

export interface SandboxNumbersColumnsArgs {
  t: TFunction<"post-deployment">;
  /** Which row currently has a request in flight, or its code form open. */
  resendingNumber: string | null;
  deletingNumber: string | null;
  verifyingNumber: string | null;
  onResend: (phoneNumber: string) => void;
  onStartVerify: (phoneNumber: string) => void;
  onCancelVerify: () => void;
  onVerifySubmit: (phoneNumber: string, oneTimePassword: string) => Promise<void>;
  onVerified: () => void;
  onDelete: (phoneNumber: string) => Promise<void>;
  onCancelDelete: () => void;
}

export function getSandboxNumbersColumns({
  t,
  resendingNumber,
  deletingNumber,
  verifyingNumber,
  onResend,
  onStartVerify,
  onCancelVerify,
  onVerifySubmit,
  onVerified,
  onDelete,
  onCancelDelete,
}: SandboxNumbersColumnsArgs): ColumnDef<SMSSandboxPhoneNumber>[] {
  return [
    {
      id: "phoneNumber",
      accessorKey: "PhoneNumber",
      header: t("smsSandbox.columnPhoneNumber", "Destination number"),
      enableHiding: false,
      cell: ({ row }) => (
        <span className="font-mono">{row.original.PhoneNumber ?? ""}</span>
      ),
    },
    {
      id: "status",
      accessorKey: "Status",
      header: t("common:columns.status", "Status"),
      cell: ({ row }) => <SandboxNumberStatusBadge status={row.original.Status} />,
    },
    {
      id: "actions",
      enableHiding: false,
      header: () => (
        <div className="text-right">
          {t("smsSandbox.columnActions", "Actions")}
        </div>
      ),
      cell: ({ row }) => {
        const phoneNumber = row.original.PhoneNumber ?? "";
        return (
          <SandboxNumberRowActions
            phoneNumber={phoneNumber}
            status={row.original.Status}
            isResending={resendingNumber === phoneNumber}
            isDeleting={deletingNumber === phoneNumber}
            isVerifying={verifyingNumber === phoneNumber}
            onResend={() => onResend(phoneNumber)}
            onStartVerify={() => onStartVerify(phoneNumber)}
            onCancelVerify={onCancelVerify}
            onVerifySubmit={(oneTimePassword) =>
              onVerifySubmit(phoneNumber, oneTimePassword)
            }
            onVerified={onVerified}
            onDelete={() => onDelete(phoneNumber)}
            onCancelDelete={onCancelDelete}
          />
        );
      },
    },
  ];
}
