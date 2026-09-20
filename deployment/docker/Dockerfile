# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

FROM amazonlinux:2023

# Build arguments
# GO_VERSION must track go.mod. CI passes it explicitly; this default is what a local
# `docker build` gets, and anything below go.mod's version cannot compile the module.
ARG GO_VERSION=1.26.6
ARG NODE_VERSION=24
ARG VERSION

# Install system packages
RUN yum update -y --allowerasing && \
    yum install -y --allowerasing \
        git \
        curl \
        wget \
        tar \
        gzip \
        make \
        gcc \
        gcc-c++ \
        openssl-devel \
        libffi-devel \
        bzip2-devel \
        xz-devel \
        zlib-devel \
        jq \
        unzip \
        which && \
    yum clean all

# Install AWS CLI
ARG AWSCLI_VERSION=2.36.10
RUN curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64-${AWSCLI_VERSION}.zip" -o "awscliv2.zip" && \
    unzip awscliv2.zip && \
    ./aws/install && \
    rm -rf awscliv2.zip aws

# Install Golang
RUN wget https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz && \
    rm go${GO_VERSION}.linux-amd64.tar.gz

# Install Node.js. npm is deliberately NOT upgraded separately -- we use the npm
# bundled with Node (24.x ships npm 11.x), so NODE_VERSION alone pins both and the
# committed package-lock.json is resolved by the same npm major that built it.
RUN curl -fsSL https://rpm.nodesource.com/setup_${NODE_VERSION}.x | bash - && \
    yum install -y nodejs

# Install Python 3.12
# Stay on the 3.12 line (Lambda runtime target); bump the patch for security fixes.
ARG PYTHON_VERSION=3.12.13
RUN cd /usr/src && \
    wget https://www.python.org/ftp/python/${PYTHON_VERSION}/Python-${PYTHON_VERSION}.tgz && \
    tar xzf Python-${PYTHON_VERSION}.tgz && \
    cd Python-${PYTHON_VERSION} && \
    ./configure --enable-optimizations && \
    make -j$(nproc) && \
    make altinstall && \
    ln -sf /usr/local/bin/python3.12 /usr/bin/python3 && \
    ln -sf /usr/local/bin/python3.12 /usr/bin/python && \
    ln -sf /usr/local/bin/pip3.12 /usr/bin/pip3 && \
    cd / && rm -rf /usr/src/Python-${PYTHON_VERSION}*

# Set environment variables
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/go"
ENV PATH="${GOPATH}/bin:${PATH}"
ENV REPOS_PATH="/root"

# Create workspace directories
RUN mkdir -p ${GOPATH}/src ${GOPATH}/bin ${GOPATH}/pkg
WORKDIR ${REPOS_PATH}

# Copy requirements files if they exist
COPY requirements*.txt ./

# Install Python requirements
# Editable entries are stripped: requirements.txt carries `-e ./cli`, whose source is not
# in the image, and every job that needs it installs it from its own checkout anyway.
RUN for f in requirements.txt requirements-dev.txt requirements-test.txt; do \
        if [ -f "$f" ]; then grep -v '^[[:space:]]*-e[[:space:]]' "$f" > /tmp/req.txt && pip3 install -r /tmp/req.txt; fi; \
    done && rm -f /tmp/req.txt

# Install common Go tools
# ginkgo/gomega are pinned to the versions in go.mod — a newer CLI than the
# library it drives produces confusing failures, so bump these together.
ARG GINKGO_VERSION=v2.28.3
ARG GOMEGA_VERSION=v1.40.0
ARG GOCOVER_COBERTURA_VERSION=v1.5.0
RUN go install github.com/onsi/ginkgo/v2/ginkgo@${GINKGO_VERSION} && \
    go install github.com/onsi/gomega/...@${GOMEGA_VERSION} && \
    go install github.com/boumenot/gocover-cobertura@${GOCOVER_COBERTURA_VERSION}

ARG GOVULNCHECK_VERSION=latest
RUN go install golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}

COPY go.work go.work.sum go.mod go.sum ./
COPY src/esp-cloud-common/go.mod src/esp-cloud-common/go.sum ./src/esp-cloud-common/
COPY src/mcp/proxy/go.mod src/mcp/proxy/go.sum ./src/mcp/proxy/
RUN go mod download && rm -rf go.work go.work.sum go.mod go.sum src

RUN GITLEAKS_VERSION=$(curl -sSfL https://api.github.com/repos/gitleaks/gitleaks/releases/latest \
      | python3 -c "import sys,json;print(json.load(sys.stdin)['tag_name'].lstrip('v'))") && \
    curl -sSfL "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz" \
      | tar -xz -C /usr/local/bin gitleaks && \
    gitleaks version

ARG SWAGGER_CLI_VERSION=4.0.4
RUN npm install -g @apidevtools/swagger-cli@${SWAGGER_CLI_VERSION}

ARG ASYNCAPI_CLI_VERSION=6.0.0
RUN mkdir -p /opt/asyncapi && cd /opt/asyncapi && \
    npm init --yes >/dev/null && \
    npm pkg set overrides.@asyncapi/generator-hooks=0.1.0 && \
    npm install --no-save @asyncapi/cli@${ASYNCAPI_CLI_VERSION}
ENV PATH="/opt/asyncapi/node_modules/.bin:${PATH}"

RUN pip3 install pip-audit build twine

COPY docs/requirements.txt ./docs/
RUN pip3 install -r docs/requirements.txt && rm -rf docs


# Install CDK
# Must match aws-cdk-cli in requirements.txt: pip installs a `cdk` binary too,
# and whichever lands earlier on PATH wins. Bump both together or they diverge.
ARG AWS_CDK_VERSION=2.1133.0
RUN npm install -g aws-cdk@${AWS_CDK_VERSION}

COPY dashboard/package.json dashboard/package-lock.json ./dashboard/
RUN cd dashboard && npm ci --no-audit --no-fund && cd .. && rm -rf dashboard

# Set default command
CMD ["/bin/bash"] 
