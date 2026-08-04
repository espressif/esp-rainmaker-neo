// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

def setup_submodules() {
    // Workspace checkout does not init submodules; rewrite cloud-components with an
    // authenticated URL first (its .gitmodules URL is relative to the superproject remote).
    sh '''
    git submodule init
    git config submodule."src/esp-cloud-common".url \
        "https://${GIT_APPFW_USR}:${GIT_APPFW_PSW}@${JENKINS_RMNG_CLOUD_COMPONENTS_REPO_URL}"
    git submodule update --recursive
    '''
    println('Submodules ready')
}

def checkout_branch(branch) {
    // Injection guard: params.BRANCH flows into an sh step; require a git-ref-safe token (starts alphanumeric, no spaces/metacharacters/leading '-').
    if (!(branch ==~ /[A-Za-z0-9][A-Za-z0-9._\/-]*/)) {
        error("Invalid branch name: ${branch}")
    }
    // The docker-agent workspace origin has no credential helper, so authenticate the fetch with the GIT_APPFW credentials (same pattern as setup_submodules/setup_git; Jenkins masks the bound token in the log). Single-quoted sh so the shell expands the credential and branch env vars — no Groovy interpolation, no injection. setup_submodules re-inits submodules against the checked-out branch afterwards.
    withEnv(["CHECKOUT_BRANCH=${branch}"]) {
        sh '''
        set -e
        git fetch "https://${GIT_APPFW_USR}:${GIT_APPFW_PSW}@${JENKINS_RMNG_REPO_URL}" "+${CHECKOUT_BRANCH}:refs/remotes/origin/${CHECKOUT_BRANCH}"
        git checkout -B "${CHECKOUT_BRANCH}" "refs/remotes/origin/${CHECKOUT_BRANCH}"
        '''
    }
    println("Checked out ${branch}")
}

def prod_publish() {
    // Bucket is a full name (prod buckets are not account/region-suffixed); the
    // publish scripts honor the APPLICATION_PUBLISHER_BUCKET env override.
    if (!params.APPLICATION_PUBLISHER_BUCKET?.trim()) {
        error('APPLICATION_PUBLISHER_BUCKET is required (full prod bucket name).')
    }
    // Guard runs ONCE, pre-flight: make publish invokes publish_cdk_assets.py per
    // stack group into the same {version}/ prefix, so a per-invocation guard would
    // fail group 2 after group 1 uploads. Exit 2 aborts before anything uploads.
    def guard = params.ALLOW_VERSION_OVERWRITE ? '' : 'python3 scripts/publish_cdk_assets.py --guard-only --version "$(head -n1 VERSION)"'
    withEnv(["APPLICATION_PUBLISHER_BUCKET=${params.APPLICATION_PUBLISHER_BUCKET}"]) {
        sh """
        # set +x: sourcing aws_creds under Jenkins' default -x would trace the export lines (prod credentials) into the build log.
        set +x
        . ./aws_creds
        set -x
        ${guard}
        make publish
        """
    }
    println('Prod publish done')
}

def write_build_manifest() {
    // Publish provenance: superproject + all submodule commits (recursive), build id/url, version, branch, and target bucket. Single-quoted sh so every value comes from the environment (no Groovy interpolation, no injection); jq guarantees valid JSON escaping.
    sh '''
    set -e
    repo_commit=$(git rev-parse HEAD)
    submodule_status=$(git submodule status --recursive)
    submodules=$(printf '%s' "$submodule_status" | sed -E 's/^.//' | jq -R 'select(length > 0) | split(" ") | {path: .[1], commit: .[0]}' | jq -s '.')
    jq -n \
      --arg build_number "$BUILD_NUMBER" \
      --arg build_url "$BUILD_URL" \
      --arg version "$RMNG_VERSION" \
      --arg branch "$PUBLISH_BRANCH" \
      --arg bucket "$APPLICATION_PUBLISHER_BUCKET" \
      --arg repo_commit "$repo_commit" \
      --argjson submodules "$submodules" \
      '{build_number: $build_number, build_url: $build_url, version: $version, branch: $branch, application_publisher_bucket: $bucket, repository: {name: "esp-rainmaker-neo", commit: $repo_commit}, submodules: $submodules}' \
      > publish-manifest.json
    cat publish-manifest.json
    '''
    println('Build manifest written')
}

return this
