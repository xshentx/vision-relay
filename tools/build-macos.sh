#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION=""
ARCH="$(go env GOARCH)"
OUTPUT="$PROJECT_ROOT/dist/Vision Relay.app"
SKIP_TESTS=0
CREATE_ARCHIVE=1

usage() {
  cat <<'USAGE'
Usage: tools/build-macos.sh [options]

Options:
  --version VERSION       Version injected into the binary (default: git describe)
  --arch ARCH             arm64, amd64, or universal (default: current Go architecture)
  --output PATH           Output .app bundle (default: dist/Vision Relay.app)
  --skip-tests            Skip go test ./...
  --no-archive            Do not create the release ZIP and SHA-256 file
  -h, --help              Show this help
USAGE
}

while (($#)); do
  case "$1" in
    --version)
      VERSION="${2:?--version requires a value}"
      shift 2
      ;;
    --arch)
      ARCH="${2:?--arch requires a value}"
      shift 2
      ;;
    --output)
      OUTPUT="${2:?--output requires a value}"
      shift 2
      ;;
    --skip-tests)
      SKIP_TESTS=1
      shift
      ;;
    --no-archive)
      CREATE_ARCHIVE=0
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "The macOS application bundle must be built on macOS (Xcode Command Line Tools are required)." >&2
  exit 1
fi
if [[ "$ARCH" != "arm64" && "$ARCH" != "amd64" && "$ARCH" != "universal" ]]; then
  echo "Unsupported architecture '$ARCH'; use arm64, amd64, or universal." >&2
  exit 2
fi
if [[ "$OUTPUT" != /* ]]; then
  OUTPUT="$PROJECT_ROOT/$OUTPUT"
fi
if [[ "$OUTPUT" != *.app ]]; then
  echo "--output must end with .app" >&2
  exit 2
fi
if [[ -z "$VERSION" ]]; then
  VERSION="$(git -C "$PROJECT_ROOT" describe --tags --always --dirty 2>/dev/null || true)"
  VERSION="${VERSION:-dev}"
fi

PLIST_VERSION="${VERSION#v}"
PLIST_VERSION="${PLIST_VERSION%%-*}"
if [[ ! "$PLIST_VERSION" =~ ^[0-9]+(\.[0-9]+){1,2}$ ]]; then
  PLIST_VERSION="0.0.0"
fi

BUILD_DIR="$(mktemp -d "${TMPDIR:-/tmp}/vision-relay-macos.XXXXXX")"
cleanup() { rm -rf "$BUILD_DIR"; }
trap cleanup EXIT

cd "$PROJECT_ROOT"
if ((SKIP_TESTS == 0)); then
  go test ./...
fi

build_arch() {
  local target_arch="$1"
  local target="$BUILD_DIR/vision-relay-$target_arch"
  env GOOS=darwin GOARCH="$target_arch" CGO_ENABLED=1 \
    go build -trimpath \
      "-ldflags=-s -w -X=vision-relay/backend/internal/server.Version=$VERSION" \
      -o "$target" ./backend/cmd/vision-relay
  printf '%s\n' "$target"
}

if [[ "$ARCH" == "universal" ]]; then
  ARM_BINARY="$(build_arch arm64)"
  AMD_BINARY="$(build_arch amd64)"
  BINARY="$BUILD_DIR/vision-relay"
  lipo -create -output "$BINARY" "$ARM_BINARY" "$AMD_BINARY"
  ASSET_ARCH="universal"
else
  BINARY="$(build_arch "$ARCH")"
  ASSET_ARCH="$ARCH"
fi

rm -rf "$OUTPUT"
mkdir -p "$OUTPUT/Contents/MacOS" "$OUTPUT/Contents/Resources"
install -m 0755 "$BINARY" "$OUTPUT/Contents/MacOS/vision-relay"

cat >"$OUTPUT/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key><string>zh_CN</string>
  <key>CFBundleDisplayName</key><string>Vision Relay</string>
  <key>CFBundleExecutable</key><string>vision-relay</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundleIdentifier</key><string>com.xshentx.vision-relay</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>CFBundleName</key><string>Vision Relay</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$PLIST_VERSION</string>
  <key>CFBundleVersion</key><string>$PLIST_VERSION</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>LSUIElement</key><true/>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

ICON_SOURCE="$PROJECT_ROOT/backend/internal/server/assets/app.png"
ICONSET="$BUILD_DIR/AppIcon.iconset"
mkdir -p "$ICONSET"
make_icon() {
  local pixels="$1"
  local name="$2"
  sips -z "$pixels" "$pixels" "$ICON_SOURCE" --out "$ICONSET/$name" >/dev/null
}
make_icon 16 icon_16x16.png
make_icon 32 icon_16x16@2x.png
make_icon 32 icon_32x32.png
make_icon 64 icon_32x32@2x.png
make_icon 128 icon_128x128.png
make_icon 256 icon_128x128@2x.png
make_icon 256 icon_256x256.png
make_icon 512 icon_256x256@2x.png
make_icon 512 icon_512x512.png
make_icon 1024 icon_512x512@2x.png
iconutil -c icns "$ICONSET" -o "$OUTPUT/Contents/Resources/AppIcon.icns"

# Ad-hoc signing avoids an invalid bundle after assembly. Release maintainers can
# replace this with Developer ID signing and notarization before distribution.
codesign --force --deep --sign - "$OUTPUT"

echo "Built macOS application: $OUTPUT (version $VERSION, architecture $ASSET_ARCH)"

if ((CREATE_ARCHIVE == 1)); then
  ARCHIVE="$(dirname "$OUTPUT")/vision-relay-darwin-$ASSET_ARCH.zip"
  rm -f "$ARCHIVE" "$ARCHIVE.sha256"
  ditto -c -k --sequesterRsrc --keepParent "$OUTPUT" "$ARCHIVE"
  HASH="$(shasum -a 256 "$ARCHIVE" | awk '{print $1}')"
  printf '%s  %s\n' "$HASH" "$(basename "$ARCHIVE")" >"$ARCHIVE.sha256"
  echo "Release archive: $ARCHIVE"
  echo "SHA-256: $ARCHIVE.sha256"
fi
