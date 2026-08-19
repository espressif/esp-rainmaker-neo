# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

# --- make behaviour ----------------------------------------------------------
# Prefer bash for its `pipefail`, which stops a failing producer in `gocover-cobertura < coverage.out | sed ...` from being masked by a succeeding sed. Detected rather than hardcoded: the dashboard_check CI job runs make on node:22-alpine, which ships busybox sh and no bash. Every recipe below is POSIX sh, so plain sh loses only pipefail. Deliberately no -e/-u either: recipes rely on `|| true`, and every loop carries its own `set -e`.
BASH := $(shell command -v bash 2>/dev/null)
ifneq ($(BASH),)
SHELL := $(BASH)
.SHELLFLAGS := -o pipefail -c
endif
# An interrupted `go build -o $@` must not leave a truncated binary that later looks up-to-date.
.DELETE_ON_ERROR:
.DEFAULT_GOAL := help

# Discover every Lambda entrypoint anywhere in the tree. go.work stitches
# submodule Go modules (cloud-components, src/mcp_stack, ...) into one
# workspace, so `go build` from rmng's root resolves them all uniformly.
# Generated and vendored trees are pruned so a stray *_main.go under cdk.out/ or node_modules/ can never mint a phantom lambda target.
MAIN_FILES := $(shell find . \( -name .git -o -name node_modules -o -name build -o -name 'cdk.out*' \) -prune -o -type f -path '*/*_main.go' -print)
BINARY_NAME := bootstrap

# Shared by the in-tree lambda rule and the optional-module loop, so the two can never drift.
GO_LAMBDA_ENV := GOOS=linux GOARCH=arm64 CGO_ENABLED=0
GO_LAMBDA_FLAGS := -ldflags "-s -w" -tags "lambda.norpc" -trimpath

# Assign env variables if they are not empty. If empty, use default.
REGION := $(or $(AWS_REGION), $(shell aws configure get region))
PROFILE := $(or $(AWS_PROFILE), default)

# Publish Version - required when running make publish (e.g. make publish PUBLISH_VERSION=1.0.0)
PUBLISH_VERSION ?= $(shell head -n1 VERSION)
PUBLISH_REGION ?= us-east-1

# Deployment group ordering derived from cdk/Stackfile.yaml. Cached on first use so targets that need no group list (test, lint, clean, ...) never shell out. stderr is intentionally not silenced: a parse error used to collapse this to an empty list, turning `make deploy-all` into a no-op that still exited 0.
CDK_STACK_GROUPS = $(eval CDK_STACK_GROUPS := $(shell python3 scripts/cfn_stack_parser.py --stackfile cdk/Stackfile.yaml --format groups))$(CDK_STACK_GROUPS)

# Submodules whose unit tests must run as part of `make test`.
# Each submodule listed must expose a `test` target in its own Makefile.
# Why this list exists: ginkgo's package discovery (./src/..., etc.) stops at Go module boundaries, so submodules with their own go.mod are invisible to rmng's `ginkgo` invocation even when their imports resolve via go.work. Builds don't need this list — `go build` crosses module boundaries via go.work just fine.
TEST_SUBMODULES := src/esp-cloud-common src/mcp/proxy

# --- Optional add-on modules (auto-detected) ---------------------------------
# Separately-distributed optional modules live in a sibling folder next to this repo (../addon_modules by default; overridable). They are absent from
# open-source checkouts. When the folder is present, the same rmng Makefile also
# builds/tests them — so there is only ONE Makefile to maintain.
OPTIONAL_MODULES_DIR ?= ../addon_modules
SUPERPROJECT_GOWORK ?= $(abspath $(CURDIR)/../go.work)
OPTIONAL_MODULES := $(wildcard $(OPTIONAL_MODULES_DIR)/go.mod)
OPTIONAL_MAIN_FILES := $(if $(OPTIONAL_MODULES),$(shell find $(OPTIONAL_MODULES_DIR) -type f -path '*/*_main.go'))
OPTIONAL_ITEST_DIRS := $(if $(OPTIONAL_MODULES),$(wildcard $(OPTIONAL_MODULES_DIR)/*/itest))

# --- Admin dashboard ---------------------------------------------------------
DASHBOARD_DIR := dashboard
DASHBOARD_DIST := $(DASHBOARD_DIR)/dist
DASHBOARD_STAMP := build/.dashboard-build.stamp
# npm ci writes this file, so it doubles as the install stamp.
DASHBOARD_NPM_STAMP := $(DASHBOARD_DIR)/node_modules/.package-lock.json
DASHBOARD_SRCS := $(shell find $(DASHBOARD_DIR) \( -name node_modules -o -name dist \) -prune -o -type f -print)

# --- rmng-lint ---------------------------------------------------------------
RMNG_LINT := build/rmng-lint
LINT_SRCS := $(shell find src/tools/rmng-lint -name '*.go')

all: go_build  ## Build every Lambda binary

help:  ## List the targets that carry a description
	@awk 'BEGIN{FS=":.*##"} /^[a-zA-Z][a-zA-Z0-9_-]*:.*##/{printf "  \033[36m%-24s\033[0m %s\n",$$1,$$2}' $(MAKEFILE_LIST)

## Convert main file to target
## For example, node_main.go -> build/node/$(BINARY_NAME)
define main_to_target
$(patsubst %_main.go,build/%/$(BINARY_NAME),$(notdir $(1)))
endef


# Define TARGETs based on MAIN_FILES
BUILD_TARGETS:=$(foreach main_go,$(MAIN_FILES),$(call main_to_target,$(main_go)))

go_build: $(BUILD_TARGETS) optional-build  ## Build all Lambda binaries into build/<fn>/bootstrap

# Build optional-module Lambdas (no-op when the folder is absent). Uses the
# superproject workspace so optional-module packages resolve their core imports.
optional-build:
ifneq ($(OPTIONAL_MODULES),)
	@echo "--- building optional-module lambdas ---"
	@set -e; for m in $(OPTIONAL_MAIN_FILES); do \
		out="build/$$(basename $$(dirname $$m))/$(BINARY_NAME)"; \
		mkdir -p "$$(dirname $$out)"; \
		echo "GOWORK=$(SUPERPROJECT_GOWORK) go build -> $$out"; \
		GOWORK=$(SUPERPROJECT_GOWORK) $(GO_LAMBDA_ENV) go build $(GO_LAMBDA_FLAGS) -o "$$out" "./$$(dirname $$m)"; \
	done
endif

## Create the rule:
##  build/foo/$(BINARY_NAME): foo_main.go build/foo/$(BINARY_NAME).deps
$(foreach main_go,$(MAIN_FILES),$(eval $(call main_to_target,$(main_go)): $(main_go) $(call main_to_target,$(main_go)).deps))

## Rule to create the dependencies file for each main file
##  build/%/$(BINARY_NAME).deps: %_main.go
##      go_deps.sh ...
# go_deps.sh is a prerequisite as well as the recipe, so changing how dependencies
# are computed invalidates the .deps files it previously wrote.
define get_deps
$(call main_to_target,$(1)).deps: $(1) scripts/go_deps.sh
	@./scripts/go_deps.sh $(1) $(call main_to_target,$(1))
endef

# Generate the dependencies files for each main file
$(foreach main_go,$(MAIN_FILES),$(eval $(call get_deps,$(main_go))))
# Include the dependencies files in the build rules (use -include to ignore missing files)
$(foreach main_go,$(MAIN_FILES),$(eval -include $(call main_to_target,$(main_go)).deps))

$(BUILD_TARGETS):
	mkdir -p $(dir $@)
	$(GO_LAMBDA_ENV) go build $(GO_LAMBDA_FLAGS) -o $@ ./$(dir $<)

# Real file targets, not phony: `npm ci` reruns only when the lockfile moves and `npm run build` only when a dashboard source changes. Both used to rerun on every deploy/setup/synth/publish.
# All dependencies come from public npm, so no registry configuration or credentials are needed.
$(DASHBOARD_NPM_STAMP): $(DASHBOARD_DIR)/package-lock.json $(DASHBOARD_DIR)/package.json
	cd $(DASHBOARD_DIR) && npm ci
	@touch $@

$(DASHBOARD_STAMP): $(DASHBOARD_NPM_STAMP) $(DASHBOARD_SRCS)
	cd $(DASHBOARD_DIR) && npm run build
	@mkdir -p $(dir $@) && touch $@

# .gitlab-ci.yml (dashboard_check) invokes this name directly.
admin-dashboard-build: $(DASHBOARD_STAMP)  ## Build admin dashboard static assets

# These source files are inputs, never something make should try to build. Empty recipes say so, and that matters here: when matching a slash-free pattern make strips the target's directory first, so a real file such as src/admin/dashboard/src/utils/matter-gen/setup-payload.ts matches the `setup-%` verb rule below. Its phony go_build prerequisite would then make that file look perpetually out of date, firing one bogus scripts/deploy.sh sweep per shadowed file.
# patsubst normalises the leading ./ that `find .` emits but `find src/tools/rmng-lint` does not, so the one file in both lists is not declared twice.
$(sort $(patsubst ./%,%,$(MAIN_FILES) $(DASHBOARD_SRCS) $(LINT_SRCS))): ;

# --- Group-scoped verbs ------------------------------------------------------
# deploy / setup / diff / destroy / synth / publish all have the same shape: resolve the pattern
# stem to a list of Stackfile groups, then run scripts/deploy.sh once per group with the matching
# flag. `all` means every group; any other stem means just that group.
#
# $(call groups_for,<stem>[,<groups to skip when the stem is `all`>])
groups_for = $(if $(filter all,$1),$(or $(strip $(filter-out $2,$(CDK_STACK_GROUPS))),$(error Stackfile yielded no deployment groups — see the cfn_stack_parser error above)),$1)

# make has no builtin reverse; destroy tears groups down in reverse dependency order.
reverse = $(strip $(call reverse_,$1))
reverse_ = $(if $1,$(call reverse_,$(wordlist 2,$(words $1),$1)) $(firstword $1))

# Deploying `claim` stands up a billable KMS CA key, so the all-groups deploy sweep skips it.
# `make deploy-claim` still deploys it on request, and `make publish` still ships its template.
DEPLOY_SKIP_GROUPS := claim

# Only cdk/apps/rmng.py synthesises AdminDashboardStack (s3deploy.Source.asset -> $(DASHBOARD_DIST)),
# so only rmng-group verbs depend on the dashboard. Every other group deploys without waiting on npm,
# and under `make -j` the dashboard build overlaps go_build instead of blocking it.
needs_dashboard = $(if $(filter rmng all,$1),$(DASHBOARD_STAMP))

# The single sweep body, parameterised through target-specific variables rather than $(call) so
# `$$g` needs no extra escaping. SWEEP_POST runs after each group.
SWEEP_FLAGS =
SWEEP_REGION = $(REGION)
SWEEP_POST = :
define sweep
@set -e; for g in $(SWEEP_GROUPS); do \
	./scripts/deploy.sh $(SWEEP_FLAGS) --region $(SWEEP_REGION) --profile $(PROFILE) --stack-group $$g; \
	$(SWEEP_POST); \
done
endef

deploy: deploy-all      ## Deploy every stack group (skips claim)
setup: setup-all        ## cdk bootstrap every stack group
diff: diff-all          ## cdk diff every stack group
destroy: destroy-all    ## Destroy every stack group, reverse dependency order
synth: synth-all        ## cdk synth every stack group
publish: publish-all    ## Synth and publish templates/assets for every stack group

# Prerequisites are second-expanded so needs_dashboard can see the pattern stem. Placed after the
# generated lambda rules above, which must keep single-expansion semantics.
.SECONDEXPANSION:

deploy-%: SWEEP_GROUPS = $(call groups_for,$*,$(DEPLOY_SKIP_GROUPS))
deploy-%: SWEEP_POST = AWS_REGION=$(REGION) AWS_PROFILE=$(PROFILE) python3 ./scripts/generate_stack_outputs.py
# The gather loop deliberately uses no skip list: prompts for every group are collected up front, claim included, so a long sweep never pauses to ask.
deploy-%: go_build $$(call needs_dashboard,$$*)
	@set -e; for g in $(call groups_for,$*); do \
		python3 scripts/gather_stack_inputs.py --stack-group $$g; \
	done
	$(sweep)
	./scripts/deploy.sh --fetch-and-upload --region $(REGION) --profile $(PROFILE)

setup-%: SWEEP_GROUPS = $(call groups_for,$*)
setup-%: SWEEP_FLAGS = --setup
setup-%: go_build $$(call needs_dashboard,$$*)
	$(sweep)

diff-%: SWEEP_GROUPS = $(call groups_for,$*)
diff-%: SWEEP_FLAGS = --diff
diff-%: go_build $$(call needs_dashboard,$$*)
	$(sweep)

synth-%: SWEEP_GROUPS = $(call groups_for,$*)
synth-%: SWEEP_FLAGS = --synth
synth-%: go_build $$(call needs_dashboard,$$*)
	$(sweep)

publish-%: SWEEP_GROUPS = $(call groups_for,$*)
publish-%: SWEEP_FLAGS = --publish --version $(PUBLISH_VERSION)
publish-%: SWEEP_REGION = $(PUBLISH_REGION)
publish-%: go_build $$(call needs_dashboard,$$*)
	@test -n "$(PUBLISH_VERSION)" || { echo "Error: PUBLISH_VERSION is empty (missing VERSION file?). Example: make publish PUBLISH_VERSION=1.0.0" >&2; exit 1; }
	$(sweep)
	AWS_REGION=$(PUBLISH_REGION) python3 scripts/upload_stackfile_to_s3.py --version $(PUBLISH_VERSION)

destroy-%: SWEEP_GROUPS = $(call reverse,$(call groups_for,$*))
destroy-%: SWEEP_FLAGS = --destroy
destroy-%:
	python3 cli/morpheus.py --destroy-test-data || true
	$(sweep)

# --- Lint / vulnerability scanning / tests ------------------------------------
$(RMNG_LINT): $(LINT_SRCS)
	@mkdir -p build
	go build -o $(RMNG_LINT) ./src/tools/rmng-lint/

lint: $(RMNG_LINT)  ## Run the rmng-lint static analysers over ./src/... and ./test/...
	$(RMNG_LINT) ./src/... ./test/...

# govulncheck analyses one module at a time and does not cross go.work boundaries
# (same constraint as TEST_SUBMODULES above), so each module is scanned in its own
# directory. Every module is scanned even if an earlier one reports findings, so the
# full picture surfaces in one run; the target exits non-zero if any module is affected
# or the scan errors — this is a blocking gate.
GOVULNCHECK_VERSION ?= latest
GOPATH_BIN = $(eval GOPATH_BIN := $(shell go env GOPATH)/bin)$(GOPATH_BIN)
VULNCHECK_MODULES := . src/mcp/proxy src/esp-cloud-common

vulncheck:  ## Scan every go.work module with govulncheck (blocking gate)
	@command -v govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@rc=0; export PATH="$(GOPATH_BIN):$$PATH"; \
	for m in $(VULNCHECK_MODULES); do \
		echo "=== govulncheck: $$m ==="; \
		( cd "$$m" && govulncheck ./... ) || rc=1; \
	done; \
	if [ "$$rc" -ne 0 ]; then echo "govulncheck: vulnerabilities found or scan error"; exit 1; fi; \
	echo "govulncheck: all modules clean"

TEST_ARGS ?=
# Scope ginkgo to rmng's own module. Submodules with their own go.mod (src/esp-cloud-common, src/mcp/proxy) are unreachable here — see TEST_SUBMODULES at the top.
TEST_PACKAGES := ./src/... ./test/infra/...

test: $(TEST_SUBMODULES:%=%-test)  ## Run ginkgo unit tests with coverage
	@mkdir -p build/tests
	@rm -f build/tests/*.txt
	@go clean -testcache && TEST_OUTPUT_DIR=$(CURDIR)/build/tests ginkgo --tags "$(GO_BUILD_TAGS)" $(TEST_ARGS) --cover --output-dir=$(CURDIR)/build/coverage --coverprofile=coverage.out $(TEST_PACKAGES)
	@gocover-cobertura < build/coverage/coverage.out | sed "s|<source>$$(pwd)\(.*\)</source>|<source>.\1</source>|g" > build/coverage/coverage.xml
	@cat build/tests/*.txt
ifneq ($(OPTIONAL_MODULES),)
	@echo "--- optional-module tests ---"
	@GOWORK=$(SUPERPROJECT_GOWORK) TEST_OUTPUT_DIR=$(CURDIR)/build/tests ginkgo --tags "$(GO_BUILD_TAGS)" $(TEST_ARGS) $(OPTIONAL_MODULES_DIR)/...
endif

# Generate one <name>-test target per TEST_SUBMODULES entry, each forwarding to that submodule's own `make test`.
$(TEST_SUBMODULES:%=%-test): %-test: ; $(MAKE) -C $* test

ITEST_ARGS ?= -n 12 -m "not unsafe and not cognito"
itest:  ## Run the pytest integration suite against a deployed stack
	pytest test/itest/ $(OPTIONAL_ITEST_DIRS) -v -s --capture=tee-sys --html=build/tests/report.html --self-contained-html --dist=loadgroup $(ITEST_ARGS)

# --- Test setup / teardown ---------------------------------------------------
# itest-setup deploys the standalone test webhook mock (cdk/apps/test_infra.py), captures its
# API Gateway URL, and seeds the test users/devices via cli/morpheus.py. Assumes the
# rmng stack group is already deployed; the notification itest points
# rmng-notifications at this mock.
itest-setup: go_build  ## Deploy the itest webhook mock and seed test data
	cdk bootstrap --qualifier rmng-test --app "python3 cdk/apps/test_infra.py" \
		--toolkit-stack-name CDKToolkit-rmng-test \
		aws://$(shell aws sts get-caller-identity --query Account --output text)/$(REGION)
	cdk deploy --all --app "python3 cdk/apps/test_infra.py" --require-approval never \
		--asset-parallelism true --outputs-file build/cdk/cdk-outputs-test.json
	@echo "Test infra deployed. API Gateway URL:"
	@python3 -c "import json; print(json.load(open('build/cdk/cdk-outputs-test.json'))['rmng-test-infra-base']['ApiGatewayUrl'])"
	python3 cli/morpheus.py --setup-test-data

test-infra-destroy:  ## Destroy the itest webhook mock
	cdk destroy --all --app "python3 cdk/apps/test_infra.py" --force

plantuml:  ## Render misc/aws_resources.puml to PNG
	plantuml -tpng misc/aws_resources.puml

clean:  ## Remove build artifacts (leaves cdk-outputs*.json deploy state alone)
	rm -rf build/ cdk.out/ cdk.out.*/ $(DASHBOARD_DIST)

	$(foreach s,$(TEST_SUBMODULES),rm -rf $(s)/build/;)

githooks:  ## Point git at .githooks so the pre-commit secret scan runs
	@git config core.hooksPath .githooks
	@command -v gitleaks >/dev/null 2>&1 \
		|| echo "note: install gitleaks to make the hook effective (CI gates on it regardless)"
	@echo "Git hooks path set to .githooks — pre-commit secret scan active."

# Pattern targets are deliberately absent: `%` is a literal filename in .PHONY, so listing
# `deploy-%` there did nothing. They are phony in practice because no such file is produced.
.PHONY: all help go_build optional-build admin-dashboard-build \
	deploy setup diff destroy synth publish \
	lint vulncheck test itest itest-setup test-infra-destroy plantuml clean githooks \
	$(TEST_SUBMODULES:%=%-test)
