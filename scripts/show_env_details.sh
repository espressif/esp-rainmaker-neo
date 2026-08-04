#!/bin/bash
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

# Script to display build environment details locally
# Mirrors the show_build_env_details() function from Jenkins utils.groovy

echo "=== Build Environment Details ==="
echo ""

echo "Operating System:"
if [ -f /etc/os-release ]; then
    cat /etc/os-release | grep PRETTY_NAME
else
    echo "macOS $(sw_vers -productVersion 2>/dev/null || echo 'Unknown')"
fi
echo ""

echo "Kernel Version:"
uname -r
echo ""

echo "Architecture:"
uname -m
echo ""

echo "Python Version:"
python3 --version 2>/dev/null || echo "Python3 not found"
echo ""

echo "Python Path:"
which python3 2>/dev/null || echo "Python3 not found"
echo ""

echo "Go Version:"
go version 2>/dev/null || echo "Go not found"
echo ""

echo "Go Path:"
if command -v go &> /dev/null; then
    echo "GOPATH: ${GOPATH:-Not set}"
    echo "GO Binary: $(which go)"
else
    echo "Go not found"
fi
echo ""

echo "Node.js Version:"
node --version 2>/dev/null || echo "Node.js not found"
echo ""

echo "Node.js Path:"
which node 2>/dev/null || echo "Node.js not found"
echo ""

echo "NPM Version:"
npm --version 2>/dev/null || echo "NPM not found"
echo ""

echo "AWS CLI Version:"
aws --version 2>/dev/null || echo "AWS CLI not found"
echo ""

echo "AWS CLI Path:"
which aws 2>/dev/null || echo "AWS CLI not found"
echo ""

echo "CDK Version:"
cdk --version 2>/dev/null || echo "CDK not found"
echo ""

echo "CDK Path:"
which cdk 2>/dev/null || echo "CDK not found"
echo ""

echo "Git Version:"
git --version 2>/dev/null || echo "Git not found"
echo ""

echo "Make Version:"
make --version 2>/dev/null | head -n 1 || echo "Make not found"
echo ""

echo "Disk Space:"
df -h
echo ""

echo "Memory Usage:"
if [[ "$OSTYPE" == "darwin"* ]]; then
    # macOS
    echo "Memory Info:"
    vm_stat | perl -ne '/page size of (\d+)/ and $size=$1; /Pages\s+([^:]+)[^\d]+(\d+)/ and printf("%-16s % 16.2f Mi\n", "$1:", $2 * $size / 1048576);'
else
    # Linux
    free -h
fi
echo ""

echo "Python Packages:"
pip3 list 2>/dev/null || echo "pip3 not found"
echo ""

echo "Go Modules (if go.mod exists):"
if [ -f "go.mod" ]; then
    go list -m all 2>/dev/null || echo "Go modules not yet initialized"
else
    echo "No go.mod file found in current directory"
fi
echo ""

echo "NPM Global Packages:"
npm list -g --depth=0 2>/dev/null || echo "NPM not found"
echo ""

echo "=== End Build Environment Details ==="

