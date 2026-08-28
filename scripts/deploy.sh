#!/bin/bash
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

# Default stack group to deploy (rmng, espuser, alexa, or smartthings)
STACK_GROUP="rmng"
OUTPUTS_FILE="build/cdk/cdk-outputs.json"
PUBLISH_VERSION=""
AWS_REGION="us-east-1"
SEQUENTIAL_ASSETS="false"

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

mkdir -p build/cdk

APP_FILE="cdk/apps/$STACK_GROUP.py"
if [ ! -f "$APP_FILE" ]; then
    echo "Stack group '$STACK_GROUP' has no CDK app at $APP_FILE — skipping."
    exit 0
fi

if [ "$STACK_GROUP" != "rmng" ]; then
    OUTPUTS_FILE="build/cdk/cdk-outputs-$STACK_GROUP.json"
fi

# Regions hosting each assistant's skill/Schema-App Lambda. The backend region is
# appended when absent: the apps emit rmng-{alexa,st}-cfg-core only on the pass where
# AWS_REGION == RMNG_REGION, so without it that stack is never synthesised. Bootstrap
# reads the same lists.
ALEXA_REGIONS=("us-east-1" "eu-west-1" "us-west-2")
ST_REGIONS=("us-east-1" "eu-west-1" "ap-northeast-1")
[[ " ${ALEXA_REGIONS[*]} " == *" $AWS_REGION "* ]] || ALEXA_REGIONS+=("$AWS_REGION")
[[ " ${ST_REGIONS[*]} " == *" $AWS_REGION "* ]] || ST_REGIONS+=("$AWS_REGION")

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

    bootstrap_env() {
        local qualifier="$1" bootstrap_region="$2"
        echo "Bootstrapping ${STACK_GROUP} (qualifier ${qualifier}) in region: ${bootstrap_region}"
        AWS_REGION="$bootstrap_region" cdk bootstrap --qualifier "$qualifier" \
            --toolkit-stack-name "CDKToolkit-${STACK_GROUP}" \
            "aws://${ACCOUNT_ID}/${bootstrap_region}"
    }

    if [ "$STACK_GROUP" == "alexa" ]; then
        for ALEXA_REGION in "${ALEXA_REGIONS[@]}"; do
            bootstrap_env "$STACK_GROUP" "$ALEXA_REGION"
        done
    elif [ "$STACK_GROUP" == "smartthings" ]; then
        # Qualifier is "sthing" (CDK caps qualifiers at 10 chars), matching the app's synthesizer.
        for ST_REGION in "${ST_REGIONS[@]}"; do
            bootstrap_env "sthing" "$ST_REGION"
        done
    else
        bootstrap_env "$STACK_GROUP" "$AWS_REGION"
    fi
    exit 0
elif [ "$command" == "--diff" ]; then
    # Use context to point to the custom asset bucket
    cdk diff --all --app "python3 $APP_FILE"
    exit 0
elif [ "$command" == "--destroy" ]; then
    # Destroy the test resources (only for rmng stack)
    if [ "$STACK_GROUP" == "rmng" ]; then
        python3 cli/morpheus.py --destroy-test-data || true
    fi
    # Destroy the stack
    cdk destroy --all --app "python3 $APP_FILE"
    exit 0
elif [ "$command" == "--synth" ]; then
    export CDK_PUBLISH=true
    cdk synth --all --app "python3 $APP_FILE" > build/cdk/cdk-output.yaml
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

# Use context to point to the custom asset bucket for deployment
if [ "$STACK_GROUP" == "alexa" ]; then
    RMNG_REGION="$AWS_REGION"
    for ALEXA_REGION in "${ALEXA_REGIONS[@]}"; do
        echo "Deploying alexa stack to region: $ALEXA_REGION (rmng region: $RMNG_REGION)"
        RMNG_REGION="$RMNG_REGION" AWS_REGION="$ALEXA_REGION" cdk deploy --all --app "python3 $APP_FILE" --require-approval never --asset-parallelism true --outputs-file $OUTPUTS_FILE
    done
elif [ "$STACK_GROUP" == "smartthings" ]; then
    RMNG_REGION="$AWS_REGION"
    for ST_REGION in "${ST_REGIONS[@]}"; do
        echo "Deploying smartthings stack to region: $ST_REGION (rmng region: $RMNG_REGION)"
        RMNG_REGION="$RMNG_REGION" AWS_REGION="$ST_REGION" cdk deploy --all --app "python3 $APP_FILE" --require-approval never --asset-parallelism true --outputs-file $OUTPUTS_FILE
    done
else
    cdk deploy --all --app "python3 $APP_FILE" --require-approval never --asset-parallelism true --outputs-file $OUTPUTS_FILE ${DEPLOY_PARAMS[@]+"${DEPLOY_PARAMS[@]}"}
fi
