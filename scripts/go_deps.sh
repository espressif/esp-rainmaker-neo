#!/bin/bash
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

# Usage go_deps.sh abc_main.go build/abc/bootstrap

DEPS_FILE=$2.deps
TMP_FILE=$DEPS_FILE.tmp

mkdir -p `dirname $2`
PKG_DIR=`dirname $1`
PKG_FILES=`ls $PKG_DIR/*.go 2>/dev/null | grep -v '_test\.go$' | tr '\n' ' '`
LIST=`echo -e "$2: $1 $DEPS_FILE $PKG_FILES"`

for i in `go list -f {{.Deps}} $PKG_DIR | tr ' ' '\n' | sort | uniq | grep rmng | sed -e 's/rmng\///g'`
do
	if [ -d $i ]; then
		LIST="$LIST `ls $i/*.go | tr '\n' ' '`"
	fi
done
echo $LIST > $TMP_FILE
mv $TMP_FILE $DEPS_FILE
