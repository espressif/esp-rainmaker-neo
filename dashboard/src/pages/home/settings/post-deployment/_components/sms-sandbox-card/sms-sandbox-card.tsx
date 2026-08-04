/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Rocket } from "lucide-react";
import {
  Alert,
  Pagination,
  SectionCard,
  SimpleList,
  Skeleton,
  Typography,
  type SimpleListItem,
} from "@espressif/dashboard-ui-components/components";
import { SmsSandboxStatusBadge } from "@/components/sms-sandbox/sms-sandbox-status-badge";
import AddSandboxNumberForm from "./_components/add-sandbox-number-form";
import SandboxNumbersTable from "./_components/sandbox-numbers-table";
import { SANDBOX_PAGE_SIZE, useSmsSandbox } from "./use-sms-sandbox";
import type { SmsSandboxCardProps } from "./sms-sandbox-card.props";

/**
 * SMS sandbox card. Unlike the other post-deployment values this one is writable: while the
 * account is in the sandbox, only verified destinations receive SMS, and both registering and
 * verifying a destination are plain SNS calls the page's scoped credentials already allow.
 */
export default function SmsSandboxCard({
  Icon,
  titleKey,
  noteKey,
}: SmsSandboxCardProps) {
  const { t } = useTranslation("post-deployment");
  const {
    credsError,
    accountStatus,
    numbers,
    isListLoading,
    listErrorKey,
    hasPreviousPage,
    hasNextPage,
    goToNextPage,
    goToPreviousPage,
    reloadCurrentPage,
    reloadFromFirstPage,
    addNumber,
    resendCode,
    verifyNumber,
    deleteNumber,
  } = useSmsSandbox();

  const note = t(noteKey, "");
  const guidanceItems: SimpleListItem[] = [
    {
      key: "guidance",
      label: t("raisingThisLimit", "Raising this limit"),
      icon: Rocket,
      content: note ? (
        <Typography variant="body2" as="p" className="text-foreground">
          {note}
        </Typography>
      ) : undefined,
    },
  ];

  return (
    <SectionCard
      allowCollapse={false}
      size="default"
      variant="soft"
      color="silver"
      icon={<Icon className="h-6 w-6" />}
      primaryText={t(titleKey)}
      actions={
        accountStatus === "loading" ? null : (
          <SmsSandboxStatusBadge status={accountStatus} />
        )
      }
    >
      <div className="space-y-4">
        <SimpleList items={guidanceItems} size="sm" />

        {credsError && (
          <Alert
            type="error"
            variant="outline"
            title={t("credsError", "Could not obtain scoped AWS credentials")}
            description={t(
              "credsErrorDescription",
              "The deployment could not vend read-only credentials for this account. Reload the page, and check the deployment's permissions if it keeps failing.",
            )}
          />
        )}

        {accountStatus === "loading" && !credsError && (
          <div className="space-y-2" aria-busy>
            <Skeleton className="h-4 w-48" />
            <Skeleton className="h-4 w-full" />
          </div>
        )}

        {accountStatus === "production" && (
          <Alert
            type="success"
            variant="soft"
            title={t(
              "smsSandbox.outOfSandboxTitle",
              "This account is out of the SMS sandbox",
            )}
            description={t(
              "smsSandbox.outOfSandboxDescription",
              "SMS reaches any phone number, so destination numbers no longer need to be verified. There is nothing to manage here.",
            )}
          />
        )}

        {accountStatus === "unknown" && (
          <Alert
            hideIcon
            type="warning"
            variant="soft"
            description={t(
              "smsSandbox.statusFailed",
              "Could not read the SMS sandbox status for this account.",
            )}
          />
        )}

        {accountStatus === "sandbox" && (
          <>
            {listErrorKey ? (
              <Alert type="error" variant="soft" description={t(listErrorKey)} />
            ) : (
              <SandboxNumbersTable
                numbers={numbers}
                isLoading={isListLoading}
                onResend={resendCode}
                onVerify={verifyNumber}
                onDelete={deleteNumber}
                onVerified={reloadCurrentPage}
                onDeleted={reloadFromFirstPage}
              />
            )}

            <Pagination
              size="sm"
              previousLabel={t("smsSandbox.previousPage", "Previous")}
              nextLabel={t("smsSandbox.nextPage", "Next")}
              hasPrevPage={hasPreviousPage}
              hasNextPage={hasNextPage}
              onPrevPage={goToPreviousPage}
              onNextPage={goToNextPage}
              disabled={isListLoading}
              currentPageSize={SANDBOX_PAGE_SIZE}
              // No pageSizeOptions are offered, so the selector is hidden and this never fires.
              onPageSizeChange={() => undefined}
            />

            <AddSandboxNumberForm
              onSubmit={addNumber}
              onSuccess={reloadFromFirstPage}
              disabled={isListLoading}
            />
          </>
        )}
      </div>
    </SectionCard>
  );
}
