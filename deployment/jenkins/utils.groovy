// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

def show_build_env_details() {
    println('=== Build Environment Details ===')
    sh '''
    echo "Operating System:"
    cat /etc/os-release | grep PRETTY_NAME
    echo ""

    echo "Kernel Version:"
    uname -r
    echo ""

    echo "Architecture:"
    uname -m
    echo ""

    echo "Python Version:"
    python3 --version
    echo ""

    echo "Python Path:"
    which python3
    echo ""

    echo "Go Version:"
    go version
    echo ""

    echo "Go Path:"
    echo "GOPATH: $GOPATH"
    echo "GO Binary: $(which go)"
    echo ""

    echo "Node.js Version:"
    node --version
    echo ""

    echo "Node.js Path:"
    which node
    echo ""

    echo "NPM Version:"
    npm --version
    echo ""

    echo "AWS CLI Version:"
    aws --version
    echo ""

    echo "AWS CLI Path:"
    which aws
    echo ""

    echo "CDK Version:"
    cdk --version
    echo ""

    echo "CDK Path:"
    which cdk
    echo ""

    echo "Git Version:"
    git --version
    echo ""

    echo "Make Version:"
    make --version | head -n 1
    echo ""

    echo "Disk Space:"
    df -h
    echo ""

    echo "Memory Usage:"
    if command -v free &> /dev/null; then
        free -h
    else
        cat /proc/meminfo | grep -E 'MemTotal|MemFree|MemAvailable'
    fi
    echo ""

    echo "Python Packages:"
    pip3 list
    echo ""

    echo "NPM Global Packages:"
    npm list -g --depth=0
    echo ""
    '''
    println('=== End Build Environment Details ===')
}

def setup_aws_credentials() {
    sh '''
    echo "${AWS_ENV_CREDS}" > ./aws_creds
    . ./aws_creds

    mkdir -p /root/.aws

    echo "[default]" > /root/.aws/config
    echo "region = ${AWS_REGION}" >> /root/.aws/config
    echo "output = json" >> /root/.aws/config
    '''

    def accountId = sh(script: '. ./aws_creds && aws sts get-caller-identity --query Account --output text', returnStdout: true).trim()

    // Set environment variables for account ID
    env.ACCOUNT_ID = accountId

    println("AWS credentials setup done for region: ${AWS_REGION}, Account ID: ${accountId}")
}

def setup_git() {
    sh '''
    . ./aws_creds
    aws s3 ls
    cd /root
    # Explicit clone directory: the rest of the pipeline hardcodes /root/esp-rainmaker-neo, so it must not depend on the basename of the credential-supplied repo URL.
    git clone -b "${BRANCH}" "https://${GIT_APPFW_USR}:${GIT_APPFW_PSW}@${JENKINS_RMNG_REPO_URL}" esp-rainmaker-neo
    cd esp-rainmaker-neo

    git submodule init

    git config submodule."cloud-components".url \
        "https://${GIT_APPFW_USR}:${GIT_APPFW_PSW}@${JENKINS_RMNG_CLOUD_COMPONENTS_REPO_URL}"

    git submodule update --recursive
    '''
    println('Git set up done')
}


def validate_cdk_outputs(currentAccountId, currentRegion) {
    println('Validating CDK outputs against AWS credentials')

    def validationResult = sh(script: """
    . ./aws_creds
    cd /root/esp-rainmaker-neo

    # Use the account ID and region passed from functions
    CURRENT_ACCOUNT_ID=${currentAccountId}
    CURRENT_REGION=${currentRegion}

    echo "Current AWS Account ID: \${CURRENT_ACCOUNT_ID}"
    echo "Current AWS Region: \${CURRENT_REGION}"
    echo ""

    # Extract account and region from CDK outputs
    # First, try to get from StackAccountId and StackRegion fields
    CDK_ACCOUNT=\$(jq -r '.[] | .StackAccountId // empty' rmng-outputs.json 2>/dev/null | head -1)
    CDK_REGION=\$(jq -r '.[] | .StackRegion // empty' rmng-outputs.json 2>/dev/null | head -1)

    # Fallback: Extract from ARNs if StackAccountId/StackRegion not found
    if [ -z "\$CDK_ACCOUNT" ] || [ -z "\$CDK_REGION" ]; then
        # Get the first ARN from CDK outputs
        FIRST_ARN=\$(jq -r '.[] | to_entries[] | select(.key | contains("Arn")) | .value' rmng-outputs.json 2>/dev/null | head -1)

        if [ -n "\$FIRST_ARN" ]; then
            # Extract account ID from ARN (position 5 in arn:aws:service:region:account:resource)
            if [ -z "\$CDK_ACCOUNT" ]; then
                CDK_ACCOUNT=\$(echo "\$FIRST_ARN" | cut -d':' -f5)
            fi

            # Extract region from ARN (position 4 in arn:aws:service:region:account:resource)
            if [ -z "\$CDK_REGION" ]; then
                CDK_REGION=\$(echo "\$FIRST_ARN" | cut -d':' -f4)
            fi
        fi
    fi

    echo "CDK Outputs Account ID: \${CDK_ACCOUNT}"
    echo "CDK Outputs Region: \${CDK_REGION}"
    echo ""

    # Validation flags
    VALIDATION_PASSED=true

    # Validate Account ID
    if [ -n "\$CDK_ACCOUNT" ]; then
        if [ "\$CDK_ACCOUNT" != "\$CURRENT_ACCOUNT_ID" ]; then
            echo "ERROR: Account ID mismatch!"
            echo "  CDK Outputs Account: \${CDK_ACCOUNT}"
            echo "  Current AWS Account: \${CURRENT_ACCOUNT_ID}"
            VALIDATION_PASSED=false
        else
            echo "✓ Account ID validation passed"
        fi
    else
        echo "ERROR: Could not extract Account ID from CDK outputs"
        VALIDATION_PASSED=false
    fi

    # Validate Region
    if [ -n "\$CDK_REGION" ]; then
        if [ "\$CDK_REGION" != "\$CURRENT_REGION" ]; then
            echo "ERROR: Region mismatch!"
            echo "  CDK Outputs Region: \${CDK_REGION}"
            echo "  Current AWS Region: \${CURRENT_REGION}"
            VALIDATION_PASSED=false
        else
            echo "✓ Region validation passed"
        fi
    else
        echo "ERROR: Could not extract Region from CDK outputs"
        VALIDATION_PASSED=false
    fi

    if [ "\$VALIDATION_PASSED" = "false" ]; then
        echo ""
        echo "VALIDATION FAILED: CDK outputs do not match the provided AWS credentials!"
        exit 1
    fi

    echo ""
    echo "✓ CDK outputs validation successful"
    """, returnStatus: true)

    if (validationResult != 0) {
        error('CDK outputs validation failed! Please ensure the CDK outputs match the AWS account and region.')
    }

    println('CDK outputs validation passed')
}

def setup_cdk_outputs(currentAccountId, currentRegion) {
    println('Setting up CDK outputs from input parameter')
    sh '''
    cd /root/esp-rainmaker-neo
    echo "${RMNG_OUTPUTS_JSON}" > rmng-outputs.json

    if [ ! -s rmng-outputs.json ]; then
        echo "Error: CDK outputs file is empty"
        exit 1
    fi

    echo "CDK outputs file created successfully"
    cat rmng-outputs.json
    '''
    println('CDK outputs setup done')

    // Validate CDK outputs against AWS credentials
    validate_cdk_outputs(currentAccountId, currentRegion)
}

def build_and_deploy() {
    println("Building and deploying in mode: ${env.DEPLOY_MODE}")

    // make deploy gathers Stackfile prompt inputs (e.g. AdminEmails) from the environment.
    withEnv(["RMNG_ADMIN_EMAILS=${params.RMNG_ADMIN_EMAILS ?: ''}"]) {
        if (env.DEPLOY_MODE == 'New deployment') {
            sh '''
            . ./aws_creds
            cd /root/esp-rainmaker-neo
            pip3 install -r requirements.txt
            make setup
            make deploy
            '''
        } else if (env.DEPLOY_MODE == 'Upgrade deployment') {
            sh '''
            . ./aws_creds
            cd /root/esp-rainmaker-neo
            pip3 install -r requirements.txt
            make deploy
            '''
        } else {
            println("Skipping deployment as per DEPLOY_MODE: ${env.DEPLOY_MODE}")
        }
    }
    println('Deploy done')
}


def deploy_test() {
    println('Run deployment test')
    sh '''
    . ./aws_creds
    cd /root/esp-rainmaker-neo

    if [ "$RUN_TEST" = "true" ]; then
        echo "Running deployment tests"
        pytest test_api.py -v -s --capture=tee-sys --html=/root/esp-rainmaker-neo/report.html --self-contained-html
    else
        echo "Skipping deployment test"
    fi
    '''
    println('Deployment test done')
}

return this
