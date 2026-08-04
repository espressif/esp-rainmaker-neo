/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ArrowRightLeft } from "lucide-react";
import { UrlListField } from "@/components/url-list-field";
import type { UrlListFieldLabels } from "@/components/url-list-field";

/**
 * Alexa-specific adapter around the reusable {@link UrlListField}: it resolves
 * the `alexa` namespace copy and binds the shared manager to the form's
 * `redirect_uris` field array.
 */
export default function RedirectUrisField() {
  const { t } = useTranslation(["voice-assistants", "common"]);

  const labels = useMemo<UrlListFieldLabels>(
    () => ({
      addAction: t("alexa.addUri", "Add URI"),
      cancelAction: t("common:actions.cancel", "Cancel"),
      inputLabel: t("alexa.form.redirectUris.addLabel", "Redirect URI"),
      inputPlaceholder: t("alexa.redirectUriPlaceholder", "https://example.com/api/skill/link/..."),
      emptyState: t(
        "alexa.form.redirectUris.emptyState",
        "No redirect URIs added yet. Add at least one to save.",
      ),
      deleteAriaLabel: t(
        "alexa.form.redirectUris.deleteAriaLabel",
        "Remove redirect URI",
      ),
      requiredError: t(
        "alexa.form.errors.redirectUriRequired",
        "Redirect URI cannot be empty.",
      ),
      duplicateError: t(
        "alexa.form.errors.redirectUriDuplicate",
        "This redirect URI has already been added.",
      ),
    }),
    [t],
  );

  return (
    <UrlListField
      name="redirect_uris"
      icon={<ArrowRightLeft className="h-5 w-5" aria-hidden />}
      cardTitle={t("alexa.redirectUris", "Redirect URIs")}
      cardDescription={t(
        "alexa.form.redirectUris.sectionDescription",
        "Account-linking redirect URIs allowed for this Alexa skill.",
      )}
      labels={labels}
    />
  );
}
