/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw } from "lucide-react";
import { Alert, Button } from "@espressif/dashboard-ui-components/components";
import {
  CredentialsUnavailableError,
  useDeploymentValueReading,
  useRefreshDeploymentValue,
} from "@/api/post-deployment/deployment-values.queries";
import { awsReadErrorMessageKey } from "../../../../post-deployment.utils";
import ValueDetailList from "../value-detail-list";
import ValueReadingSkeleton from "../value-reading-skeleton";
import type { LookedUpValueContentProps } from "./looked-up-value-content.props";

export default function LookedUpValueContent({
  value,
}: LookedUpValueContentProps) {
  const { t } = useTranslation(["post-deployment", "common"]);
  const { data: reading, isPending, error } = useDeploymentValueReading(value);
  const refreshValue = useRefreshDeploymentValue();
  const [isRetrying, setIsRetrying] = useState(false);

  const handleRetry = useCallback(async () => {
    setIsRetrying(true);
    try {
      await refreshValue(value);
    } finally {
      setIsRetrying(false);
    }
  }, [refreshValue, value]);

  const retryButton = (
    <Button
      variant="ghost"
      size="sm"
      color="gray"
      fullWidth={false}
      startIcon={<RefreshCw />}
      loading={isRetrying}
      onClick={() => void handleRetry()}
    >
      {t("common:actions.retry", "Retry")}
    </Button>
  );

  // `isPending` is trustworthy because this query is never disabled — credentials
  // are fetched inside the queryFn rather than gated on a hook.
  if (isPending) {
    return <ValueReadingSkeleton />;
  }

  if (error instanceof CredentialsUnavailableError) {
    return (
      <Alert
        hideIcon
        type="error"
        title={t("credsError", "Could not obtain scoped AWS credentials")}
        description={t(
          "credsErrorDescription",
          "The deployment could not vend read-only credentials for this account. Reload the page, and check the deployment's permissions if it keeps failing.",
        )}
        action={retryButton}
      />
    );
  }

  if (error) {
    return (
      <Alert
        hideIcon
        type="error"
        title={t("readFailed", "Could not read this value")}
        description={t(awsReadErrorMessageKey(error))}
        action={retryButton}
      />
    );
  }

  return <ValueDetailList reading={reading} noteKey={value.noteKey} />;
}
