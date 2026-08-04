/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { AppLogo } from "@espressif/dashboard-ui-components";

export default function PublicHeader() {
  return (
    <header
      className="fixed top-0 left-0 right-0 z-50 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60"
      style={{ height: "var(--espd-header-height)" }}
    >
      <div className="flex h-full items-center justify-between px-4 max-w-7xl mx-auto">
        {/* Left section: Logo */}
        <div className="flex items-center">
          <AppLogo name="rainmaker" />
        </div>

        {/* Right section: Can add login button or other public actions */}
        <div className="flex items-center gap-2">
          {/* Placeholder for future public header actions */}
        </div>
      </div>
    </header>
  );
}
