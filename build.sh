#!/bin/sh
# Builds release binaries into dist/. Go 1.22 is the newest toolchain that
# still targets macOS 10.15 and Windows 10; the go command fetches it.
# Windows 7 to 8.1 need go-legacy-win7, a Go fork downloaded into build/.
set -eu
cd "$(dirname "$0")"

export CGO_ENABLED=0 GOTOOLCHAIN=go1.22.12
name=jellyfin-to-plex-proxy
legacy_release=v1.26.9-1
legacy=go-legacy-win7-1.26.9-1

sha256() {
	if command -v sha256sum >/dev/null; then sha256sum -- "$@"; else shasum -a 256 -- "$@"; fi
}

# The fork's published checksum for this build host's toolchain.
legacy_checksum() {
	case $1 in
	darwin_arm64) echo 5755a45b507111b1e3583427a3d324ff893e42d254dea42075bb39a8f6018a28 ;;
	darwin_amd64) echo a3c425ed25811fe196c9c59fee60305dd0501f53de4a6a8be99ef410981220ce ;;
	linux_amd64) echo 31d6c5778d069fcc027dd4b3eb1f26f089579b98cccc07ac54da038124ece99a ;;
	linux_arm64) echo 07365f00605a71346d2179088ae77e1ed1a847db7778af5eabc8a52ff28eb9a3 ;;
	*) echo "no go-legacy-win7 toolchain for $1" >&2 && exit 1 ;;
	esac
}

# legacy_go downloads and verifies the fork once, and prints its go command.
legacy_go() {
	host=$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
	root=build/$legacy
	if [ ! -x "$root/go-legacy-win7/bin/go" ]; then
		archive=build/$legacy.$host.tar.gz
		mkdir -p "$root"
		curl -fsSL -o "$archive" "https://github.com/thongtech/go-legacy-win7/releases/download/$legacy_release/$legacy.$host.tar.gz"
		if [ "$(sha256 "$archive" | cut -d' ' -f1)" != "$(legacy_checksum "$host")" ]; then
			echo "$archive: checksum mismatch" >&2
			exit 1
		fi
		tar -xzf "$archive" -C "$root"
	fi
	echo "$root/go-legacy-win7/bin/go"
}

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
legacy_go=$(legacy_go)
for arch in amd64 386; do
	GOOS=windows GOARCH=$arch GOTOOLCHAIN=local "$legacy_go" build -trimpath -ldflags='-s -w' -o "dist/$name-windows-legacy-$arch.exe" .
done
cp "$name.service" dist/

cd dist
sums=$(sha256 *)
echo "$sums" >SHA256SUMS
ls -l
