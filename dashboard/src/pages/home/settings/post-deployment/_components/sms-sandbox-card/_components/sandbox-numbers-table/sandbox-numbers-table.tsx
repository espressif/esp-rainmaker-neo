/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useSetState } from "react-use";
import {
  Alert,
  DataTable,
} from "@espressif/dashboard-ui-components/components";
import { SANDBOX_PAGE_SIZE } from "../../use-sms-sandbox";
import { snsErrorMessageKey } from "../../sms-sandbox-card.utils";
import { getSandboxNumbersColumns } from "./_columns/sandbox-numbers-columns";
import type { SandboxNumbersTableProps } from "./sandbox-numbers-table.props";

/** Which row is mid-request, plus the feedback shown above the table. */
interface SandboxNumbersTableState {
  verifyingNumber: string | null;
  deletingNumber: string | null;
  resendingNumber: string | null;
  deleteErrorKey: string | null;
  resendErrorKey: string | null;
  resentNumber: string | null;
}

const INITIAL_STATE: SandboxNumbersTableState = {
  verifyingNumber: null,
  deletingNumber: null,
  resendingNumber: null,
  deleteErrorKey: null,
  resendErrorKey: null,
  resentNumber: null,
};

export default function SandboxNumbersTable({
  numbers,
  isLoading,
  onResend,
  onVerify,
  onDelete,
  onVerified,
  onDeleted,
}: SandboxNumbersTableProps) {
  const { t } = useTranslation("post-deployment");
  const [state, setState] = useSetState<SandboxNumbersTableState>(INITIAL_STATE);

  const handleResend = useCallback(
    async (phoneNumber: string) => {
      setState({
        resendErrorKey: null,
        resentNumber: null,
        resendingNumber: phoneNumber,
      });
      try {
        await onResend(phoneNumber);
        // Revealing the code field here saves a second click: a code is now on its way.
        setState({ resentNumber: phoneNumber, verifyingNumber: phoneNumber });
      } catch (error) {
        setState({ resendErrorKey: snsErrorMessageKey(error) });
      } finally {
        setState({ resendingNumber: null });
      }
    },
    [onResend, setState],
  );

  // ConfirmationDialog closes itself once onConfirm settles, so a failure has to be reported
  // outside it — hence the alert above the table rather than the dialog's own error slot.
  const handleDelete = useCallback(
    async (phoneNumber: string) => {
      setState({ deleteErrorKey: null, deletingNumber: phoneNumber });
      try {
        await onDelete(phoneNumber);
        onDeleted();
      } catch (error) {
        setState({ deleteErrorKey: snsErrorMessageKey(error) });
      } finally {
        setState({ deletingNumber: null });
      }
    },
    [onDelete, onDeleted, setState],
  );

  const handleStartVerify = useCallback(
    (phoneNumber: string) => {
      setState({ verifyingNumber: phoneNumber });
    },
    [setState],
  );

  const handleCancelVerify = useCallback(() => {
    setState({ verifyingNumber: null });
  }, [setState]);

  const handleVerified = useCallback(() => {
    setState({ verifyingNumber: null });
    onVerified();
  }, [onVerified, setState]);

  const handleCancelDelete = useCallback(() => {
    setState({ deleteErrorKey: null });
  }, [setState]);

  const columns = useMemo(
    () =>
      getSandboxNumbersColumns({
        t,
        resendingNumber: state.resendingNumber,
        deletingNumber: state.deletingNumber,
        verifyingNumber: state.verifyingNumber,
        onResend: (phoneNumber) => void handleResend(phoneNumber),
        onStartVerify: handleStartVerify,
        onCancelVerify: handleCancelVerify,
        onVerifySubmit: onVerify,
        onVerified: handleVerified,
        onDelete: handleDelete,
        onCancelDelete: handleCancelDelete,
      }),
    [
      handleCancelDelete,
      handleCancelVerify,
      handleDelete,
      handleResend,
      handleStartVerify,
      handleVerified,
      onVerify,
      state.deletingNumber,
      state.resendingNumber,
      state.verifyingNumber,
      t,
    ],
  );

  return (
    <div className="space-y-3">
      {state.deleteErrorKey && (
        <Alert
          hideIcon
          type="error"
          variant="soft"
          description={t(state.deleteErrorKey)}
        />
      )}

      {state.resendErrorKey && (
        <Alert
          hideIcon
          type="error"
          variant="soft"
          description={t(state.resendErrorKey)}
        />
      )}

      {state.resentNumber && (
        <Alert
          hideIcon
          type="success"
          variant="soft"
          description={t("smsSandbox.codeResent", "A new code has been sent to {{phoneNumber}}. It can take a minute to arrive.", {
            phoneNumber: state.resentNumber,
          })}
        />
      )}

      <DataTable
        columns={columns}
        data={numbers}
        isFetching={isLoading}
        pageSize={SANDBOX_PAGE_SIZE}
        showBorder
        showColumnVisibilitySelector={false}
        // The card owns paging: SNS pages by opaque token, and DataTable's own controls
        // always offer a page-size selector this hook cannot honour.
        hidePaginationControls
        tableRowClassName="group"
        noResultsHeading={t(
          "smsSandbox.emptyHeading",
          "No destination numbers yet",
        )}
        noResultsDescription={t(
          "smsSandbox.emptyDescription",
          "Add one below, then confirm the code AWS texts to it.",
        )}
      />
    </div>
  );
}
