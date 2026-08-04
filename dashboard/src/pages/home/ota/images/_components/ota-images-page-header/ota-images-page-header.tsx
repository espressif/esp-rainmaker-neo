/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Upload } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  Button,
  SearchBox,
} from "@espressif/dashboard-ui-components/components";
import type { OtaImagesPageHeaderProps } from "./ota-images-page-header.props";

export function OtaImagesPageHeader({
  onUploadClick,
  onSearch,
  onSearchClear,
}: OtaImagesPageHeaderProps) {
  const { t } = useTranslation("ota-images");

  return (
    <div className="flex items-center justify-between gap-4 p-5 bg-accent/10 w-full">
      <div className="w-xs shrink-0">
        <SearchBox
          placeholder={t("search.placeholder", "Search by name prefix")}
          onSearch={onSearch}
          onClear={onSearchClear}
          className="font-normal"
          size="sm"
        />
      </div>
      <div className="flex shrink-0 items-center gap-3">
        <Button
          variant="default"
          fullWidth={false}
          startIcon={<Upload className="h-4 w-4" aria-hidden />}
          onClick={onUploadClick}
          size="sm"
        >
          {t("uploadButton", "Upload OTA Image")}
        </Button>
      </div>
    </div>
  );
}
