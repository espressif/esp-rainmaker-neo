# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

# Local convenience wrapper around esp-docs' build-docs.
#
# Mirrors esp-rainmaker-neo-firmware/docs/Makefile. Unlike that one this tree has
# no Doxygen step, so there is no `fast` target -- there is nothing to skip.
#
# Requires: pip install -r requirements.txt

LANGUAGE   ?= en
BUILDDIR   ?= _build
# Repo root, one level up from docs/.
PROJECTDIR ?= ..
BUILD_DOCS ?= build-docs

# Docs are target-agnostic -- no -t, so output lands in $(BUILDDIR)/$(LANGUAGE)/generic/.
BUILD_DOCS_ARGS = -l $(LANGUAGE) -pp $(PROJECTDIR) -b $(BUILDDIR)

.PHONY: help html linkcheck clean

help:
	@echo 'make html       - build the HTML docs into $(BUILDDIR)/$(LANGUAGE)/generic/html'
	@echo 'make linkcheck  - check external links'
	@echo 'make clean      - remove $(BUILDDIR)'

html:
	$(BUILD_DOCS) $(BUILD_DOCS_ARGS)

linkcheck:
	$(BUILD_DOCS) $(BUILD_DOCS_ARGS) linkcheck

clean:
	rm -rf $(BUILDDIR)
