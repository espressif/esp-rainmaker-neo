/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import {
  PageContainer,
  PageContainerSkeleton,
} from "@espressif/dashboard-ui-components/components";
import { OtaImagesMainContent } from "./_components/ota-images-main-content";
import { OtaImagesPageHeader } from "./_components/ota-images-page-header";
import { useOtaImages } from "./use-ota-images";

const OTA_IMAGES_ROOT_PATHS = new Set([
  "/home/ota/images",
  "/home/ota/images/",
]);

function OtaImages() {
  const navigate = useNavigate();
  const location = useLocation();
  const isChildRoute = !OTA_IMAGES_ROOT_PATHS.has(location.pathname);

  const {
    pagination,
    rows,
    isLoading,
    error,
    isFetching,
    hasNextPage,
    hasPrevPage,
    handleNextPage,
    handlePrevPage,
    handlePageSizeChange,
    searchTerm,
    hasActiveSearch,
    handleSearch,
    handleSearchClear,
  } = useOtaImages();

  const handleUploadClick = () => {
    void navigate({ to: "/home/ota/images/new" });
  };

  if (isChildRoute) {
    return <Outlet />;
  }

  if (isLoading) {
    return <PageContainerSkeleton maxWidth="xl" showHeader showActions={false} />;
  }

  return (
    <PageContainer
      noGutters
      className="p-0"
      elevateHeading
      heading={
        <OtaImagesPageHeader
          onUploadClick={handleUploadClick}
          onSearch={handleSearch}
          onSearchClear={handleSearchClear}
        />
      }
    >
      <div className="px-5 pb-5">
        <OtaImagesMainContent
          rows={rows}
          error={error}
          isFetching={isFetching}
          pagination={pagination}
          hasNextPage={hasNextPage}
          hasPrevPage={hasPrevPage}
          hasActiveSearch={hasActiveSearch}
          searchTerm={searchTerm}
          onNextPage={handleNextPage}
          onPrevPage={handlePrevPage}
          onPageSizeChange={handlePageSizeChange}
        />
      </div>
    </PageContainer>
  );
}

export default OtaImages;
