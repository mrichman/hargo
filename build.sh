#!/bin/bash

VERSION=`git describe --tags`
CURRENT=`pwd`
BASENAME=`basename "$CURRENT"`
MAIN=cmd/${BASENAME}/main.go
DATE=`date --rfc-3339=seconds | sed 's/ /T/'`
LDFLAGS="-X main.Version=${VERSION} -X main.BuildTime=${DATE}"

go build -ldflags "${LDFLAGS}" -o ${BASENAME} ${MAIN}
