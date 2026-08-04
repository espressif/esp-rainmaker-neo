/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ArrowRightLeft } from "lucide-react";
import { Alert } from "@espressif/dashboard-ui-components/components";
import { UrlListManager } from "@/components/url-list-manager";
import type { UrlListManagerLabels } from "@/components/url-list-manager";
import type { GvaRedirectUrisFieldProps } from "./gva-redirect-uris-field.props";

const noop = () => {};

/**
 * Read-only display of the server-computed redirect URIs, with an info note
 * explaining they are derived from the project ID. The note lives here (above
 * the list), not inside the reusable {@link UrlListManager}.
 */
export default function GvaRedirectUrisField({
  uris,
}: GvaRedirectUrisFieldProps) {
  const { t } = useTranslation("voice-assistants");

  // Only empty-state + delete aria-label are used in read-only mode; the add/edit
  // strings are required by the type but never rendered.
  const labels = useMemo<UrlListManagerLabels>(
    () => ({
      addAction: "",
      cancelAction: "",
      inputLabel: "",
      inputPlaceholder: "",
      emptyState: t(
        "gva.form.redirectUris.emptyState",
        "Redirect URIs will appear here once the configuration is saved.",
      ),
      deleteAriaLabel: t(
        "gva.form.redirectUris.deleteAriaLabel",
        "Remove redirect URI",
      ),
      requiredError: "",
      duplicateError: "",
    }),
    [t],
  );

  return (
    <div className="flex flex-col gap-3">
      <Alert type="info" variant="soft">
        {t(
          "gva.form.redirectUris.info",
          "Redirect URIs are automatically calculated from the project ID.",
        )}
      </Alert>
      <UrlListManager
        value={uris}
        onChange={noop}
        readOnly
        cardTitle={t("gva.form.redirectUris.title", "Redirect URIs")}
        icon={<ArrowRightLeft className="h-5 w-5" aria-hidden />}
        labels={labels}
      />
    </div>
  );
}
