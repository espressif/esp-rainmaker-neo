# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

# mcp_stack: nested Go module inside rmng. Its Lambda binaries are built
# by rmng's top-level Makefile (via the workspace-wide `find` over
# *_main.go), so no `build` target is needed here. This Makefile exists
# only so rmng's TEST_SUBMODULES pattern can delegate tests across the
# Go module boundary that ginkgo's package discovery can't cross.

.PHONY: test
test:
	go clean -testcache && ginkgo ./...
