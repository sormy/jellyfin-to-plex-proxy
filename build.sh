#!/bin/sh
# Builds release binaries into dist/. Go 1.22 is the newest toolchain that
# still targets macOS 10.15 and Windows 10; the go command fetches it.
set -eu
cd "$(dirname "$0")"

export CGO_ENABLED=0 GOTOOLCHAIN=go1.22.12
name=jellyfin-to-plex-proxy

go vet ./...
go test -count=1 ./... >/dev/null

rm -rf dist
mkdir dist
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
	os=${target%/*}
	arch=${target#*/}
	label=$(echo "$os" | sed 's/darwin/macos/')
	suffix=$([ "$os" = windows ] && echo .exe || true)
	GOOS=$os GOARCH=$arch go build -trimpath -ldflags='-s -w' -o "dist/$name-$label-$arch$suffix" .
done
cp "$name.service" dist/

cd dist
sums=$(if command -v sha256sum >/dev/null; then sha256sum -- *; else shasum -a 256 -- *; fi)
echo "$sums" >SHA256SUMS
ls -l
