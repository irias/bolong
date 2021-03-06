#!/bin/sh
set -e

exec env GOOS=$GOOS GOARCH=$GOARCH \
NAME=$(basename $PWD) \
VERSION=$(git describe --tags | sed 's/^v//') \
GOVERSION=$(go version | cut -f3 -d' ') \
sh -c '
	set -e
	export CGO_ENABLED=0
	SUFFIX=""
	if test $GOOS = windows; then
		SUFFIX=.exe
	fi
	DEST=local/${NAME}-${VERSION:-x}-${GOOS:-x}-${GOARCH:-x}-${GOVERSION:-x}-${BUILDID:-0}${SUFFIX}
	go build -ldflags "-X main.version=${VERSION:-x}"
	mv $NAME${SUFFIX} $DEST
	echo release: $NAME $VERSION $GOOS $GOARCH $GOVERSION $DEST
'
