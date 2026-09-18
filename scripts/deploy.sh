#!/bin/bash
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

# Default stack group to deploy (rmng, espuser, alexa, or smartthings)
STACK_GROUP="rmng"
OUTPUTS_FILE="build/cdk/cdk-outputs.json"
PUBLISH_VERSION=""
AWS_REGION="us-east-1"

# How many stacks CDK may deploy at once within one app. The CLI honours the stack
# dependency graph, so this only overlaps stacks that are genuinely independent — in the
# rmng app that is rmng-core, rmng-admin-dashboard and the add-on bases, all of which
# depend on nothing but rmng-base.
CDK_CONCURRENCY="${CDK_CONCURRENCY:-4}"

LOG_DIR="build/cdk/logs"

# Parse common arguments
while [[ $# -gt 0 ]]; do
    key="$1"
    case $key in
        --region)
            export AWS_REGION="$2"
            shift
            shift
            ;;
        --profile)
            export AWS_PROFILE="$2"
            shift
            shift
            ;;
        --version)
            PUBLISH_VERSION="$2"
            shift
            shift
            ;;
        --stack-group)
            STACK_GROUP="$2"
            shift
            shift
            ;;
        *)
            command=$1
            shift #For command parameters, these will be parsed later
            ;;
    esac
done

# Each stack group's CDK app is cdk/apps/<group>.py. A group with no such app (presentation-only or not yet implemented, e.g. bridge/support) is skipped — not failed — so a `--stack-group all` sweep passes over it.

mkdir -p build/cdk "$LOG_DIR"

APP_FILE="cdk/apps/$STACK_GROUP.py"
if [ ! -f "$APP_FILE" ]; then
    echo "Stack group '$STACK_GROUP' has no CDK app at $APP_FILE — skipping."
    exit 0
fi

if [ "$STACK_GROUP" != "rmng" ]; then
    OUTPUTS_FILE="build/cdk/cdk-outputs-$STACK_GROUP.json"
fi

# cdk.json points every app at one shared cdk/cdk.out. The Makefile deploys independent
# groups concurrently and fans the multi-region groups out over their regions, so each
# invocation needs its own cloud assembly directory or they overwrite each other's
# manifests mid-deploy. Under build/ so `make clean` reaps them.
cdk_out_dir() {
    if [ -n "${2:-}" ]; then
        echo "build/cdk/cdk.out.$1.$2"
    else
        echo "build/cdk/cdk.out.$1"
    fi
}

# The regions each multi-region group deploys into. Mirrors `regions.explicit` for
# rmng-alexa-core / rmng-st-core in cdk/Stackfile.yaml — keep the two in step.
#
# The backend region is appended when absent: the apps emit rmng-{alexa,st}-cfg-core
# only on the pass where AWS_REGION == RMNG_REGION, so without it that stack is never
# synthesised. Bootstrap and deploy both fan out over this list, so both inherit it.
regions_for_group() {
    local regions
    case "$1" in
        alexa)       regions="us-east-1 eu-west-1 us-west-2" ;;
        smartthings) regions="us-east-1 eu-west-1 ap-northeast-1" ;;
        *)           echo ""; return ;;
    esac
    [[ " $regions " == *" $AWS_REGION "* ]] || regions="$regions $AWS_REGION"
    echo "$regions"
}

# Run one command per region, concurrently, each logging to its own file. Reports every
# region's outcome rather than stopping at the first failure, and dumps the log of any
# region that failed so the cause is visible without hunting through build/cdk/logs.
#
# $1 is a function name taking (region); it must write its own output to
# "$LOG_DIR/$STACK_GROUP-<region>.log". Remaining arguments are the regions.
run_per_region() {
    local runner="$1"; shift
    local regions=("$@")
    local pids=() failed=0 i

    for region in "${regions[@]}"; do
        "$runner" "$region" &
        pids+=("$!")
    done

    for i in "${!pids[@]}"; do
        if wait "${pids[$i]}"; then
            echo "  ok   ${STACK_GROUP}/${regions[$i]}"
        else
            failed=1
            echo "  FAIL ${STACK_GROUP}/${regions[$i]} — log follows:"
            cat "$LOG_DIR/$STACK_GROUP-${regions[$i]}.log"
        fi
    done

    return $failed
}

# Parse command parameters
if [ "$command" == "--setup" ]; then
    # Bootstrap deliberately does NOT pass --app. Passing it makes the CDK execute
    # the app, and the alexa/smartthings apps read rmng-outputs.json to resolve
    # cross-stack parameters — a file that does not exist yet on a first
    # deployment. That made `make setup` fail for exactly the groups that need a
    # separate bootstrap, and the failure only surfaced later as
    # "SSM parameter /cdk-bootstrap/<qualifier>/version not found" during deploy.
    # An explicit environment is what bootstrap actually needs.
    set -e
    ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)

    # Qualifier is "sthing" for smartthings (CDK caps qualifiers at 10 chars), matching
    # that app's synthesizer; every other group uses its own name.
    BOOTSTRAP_QUALIFIER="$STACK_GROUP"
    [ "$STACK_GROUP" == "smartthings" ] && BOOTSTRAP_QUALIFIER="sthing"

    bootstrap_env() {
        local bootstrap_region="$1"
        local log="$LOG_DIR/$STACK_GROUP-$bootstrap_region.log"
        echo "Bootstrapping ${STACK_GROUP} (qualifier ${BOOTSTRAP_QUALIFIER}) in region: ${bootstrap_region}"
        AWS_REGION="$bootstrap_region" cdk bootstrap --qualifier "$BOOTSTRAP_QUALIFIER" \
            --toolkit-stack-name "CDKToolkit-${STACK_GROUP}" \
            "aws://${ACCOUNT_ID}/${bootstrap_region}" > "$log" 2>&1
    }

    SETUP_REGIONS=$(regions_for_group "$STACK_GROUP")
    if [ -n "$SETUP_REGIONS" ]; then
        # Bootstrapping one region tells us nothing about another, so do them at once.
        run_per_region bootstrap_env $SETUP_REGIONS
    else
        # $AWS_REGION, not $REGION: the Makefile never exports REGION, it passes it as
        # --region, which this script parses into AWS_REGION. Bootstrapping used to be
        # handed an empty region here.
        bootstrap_env "$AWS_REGION"
        cat "$LOG_DIR/$STACK_GROUP-$AWS_REGION.log"
    fi
    exit 0
elif [ "$command" == "--diff" ]; then
    # Use context to point to the custom asset bucket
    cdk diff --all --app "python3 $APP_FILE" --output "$(cdk_out_dir "$STACK_GROUP")"
    exit 0
elif [ "$command" == "--destroy" ]; then
    # Destroy the test resources (only for rmng stack)
    if [ "$STACK_GROUP" == "rmng" ]; then
        morpheus test-data destroy || true
    fi
    # Destroy the stack
    cdk destroy --all --app "python3 $APP_FILE" --output "$(cdk_out_dir "$STACK_GROUP")"
    exit 0
elif [ "$command" == "--synth" ]; then
    export CDK_PUBLISH=true
    rm -rf "cdk/cdk.out.$STACK_GROUP"
    cdk synth --all --app "python3 $APP_FILE" --output "$(cdk_out_dir "$STACK_GROUP")" > build/cdk/cdk-output-$STACK_GROUP.yaml
    exit 0
elif [ "$command" == "--fetch-and-upload" ]; then
    python3 ./scripts/generate_stack_outputs.py || exit 1
    python3 ./scripts/upload_rmng_outputs.py || exit 1
    exit 0
elif [ "$command" == "--publish" ]; then
    if [ -z "$PUBLISH_VERSION" ]; then
        echo "Error: --version is required for --publish. Example: scripts/deploy.sh --publish --version 1.0.0 --stack-group rmng"
        exit 1
    fi
    export CDK_PUBLISH=true
    export AWS_REGION
    rm -rf cdk.out.$STACK_GROUP
    cdk synth --all --app "python3 $APP_FILE" --output cdk.out.$STACK_GROUP
    python3 scripts/publish_cdk_assets.py --stack "$STACK_GROUP" --version "$PUBLISH_VERSION" 
    exit 0
fi

# TODO: do this cleanly later
DEPLOY_PARAMS=()
if [ "$STACK_GROUP" == "espuser" ]; then
    ADMIN_EMAILS=$(python3 -c "import json; print(json.load(open('rmng-inputs.json')).get('espuser-core', {}).get('admin_emails', ''))" 2>/dev/null)
    if [ -n "$ADMIN_EMAILS" ]; then
        DEPLOY_PARAMS+=(--parameters "espuser-core:AdminEmails=$ADMIN_EMAILS")
    fi
fi

DEPLOY_REGIONS=$(regions_for_group "$STACK_GROUP")
if [ -n "$DEPLOY_REGIONS" ]; then
    # The regional stacks are one Lambda + role + log group each, and all cross-region
    # wiring is string-built from the RmngRegion CFN parameter (IAM role names carry both
    # the rmng region and Aws.REGION), so no region's stack can collide with another's.
    # Each pass gets its own assembly dir and outputs file — they used to share one
    # outputs file, so only the last region's outputs survived.
    RMNG_REGION="$AWS_REGION"

    deploy_region() {
        local target_region="$1"
        echo "Deploying $STACK_GROUP stack to region: $target_region (rmng region: $RMNG_REGION)"
        RMNG_REGION="$RMNG_REGION" AWS_REGION="$target_region" cdk deploy --all \
            --app "python3 $APP_FILE" --require-approval never --asset-parallelism true \
            --concurrency "$CDK_CONCURRENCY" \
            --output "$(cdk_out_dir "$STACK_GROUP" "$target_region")" \
            --outputs-file "build/cdk/cdk-outputs-$STACK_GROUP-$target_region.json" \
            > "$LOG_DIR/$STACK_GROUP-$target_region.log" 2>&1
    }

    run_per_region deploy_region $DEPLOY_REGIONS
else
    cdk deploy --all --app "python3 $APP_FILE" --require-approval never --asset-parallelism true \
        --concurrency "$CDK_CONCURRENCY" --output "$(cdk_out_dir "$STACK_GROUP")" \
        --outputs-file $OUTPUTS_FILE ${DEPLOY_PARAMS[@]+"${DEPLOY_PARAMS[@]}"}
fi
