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


def generate_cdk_outputs() {
    sh '''
    . ./aws_creds
    cd /root/esp-rainmaker-neo
    AWS_REGION="${AWS_REGION}" python3 ./scripts/generate_stack_outputs.py
    '''
    println('CDK outputs generated from CloudFormation')
}


def install_requirements() {
    sh '''
    cd /root/esp-rainmaker-neo
    pip3 install -r requirements.txt
    pip3 install -e ./cli --no-deps
    '''
    println('Python requirements installed')
}


def build_and_deploy() {
    println("Building and deploying in mode: ${env.DEPLOY_MODE}")

    // make deploy gathers Stackfile prompt inputs (e.g. AdminEmails) from the environment.
    withEnv(["RMNG_ADMIN_EMAILS=${params.RMNG_ADMIN_EMAILS ?: ''}"]) {
        if (env.DEPLOY_MODE != "Don't deploy") {
            install_requirements()
        }

        if (env.DEPLOY_MODE == 'New deployment') {
            sh '''
            . ./aws_creds
            cd /root/esp-rainmaker-neo
            make setup
            make deploy
            '''
        } else if (env.DEPLOY_MODE == 'Upgrade deployment') {
            sh '''
            . ./aws_creds
            cd /root/esp-rainmaker-neo
            make deploy
            '''
        } else {
            println("Skipping deployment as per DEPLOY_MODE: ${env.DEPLOY_MODE}")
        }

        if (env.DEPLOY_MODE != "Don't deploy" && params.DEPLOY_CLAIM) {
            println('Deploying the claim group (DEPLOY_CLAIM checked)')
            sh '''
            . ./aws_creds
            cd /root/esp-rainmaker-neo
            make deploy-claim
            '''
        } else {
            println('Skipping the claim group')
        }
    }

    deploy_test_infra()

    println('Deploy done')
}


def deploy_test_infra() {
    println('Deploying itest webhook mock and seeding test data')
    sh '''
    . ./aws_creds
    cd /root/esp-rainmaker-neo
    make itest-setup
    '''
    println('Test infra setup done')
}

def deploy_test() {
    println('Run deployment test')
    sh '''
    . ./aws_creds
    cd /root/esp-rainmaker-neo

    if [ "$RUN_TEST" = "true" ]; then
        echo "Running deployment tests"
        make itest
    else
        echo "Skipping deployment test"
    fi
    '''
    println('Deployment test done')
}

return this
