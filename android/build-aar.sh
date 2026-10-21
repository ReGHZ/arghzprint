#!/usr/bin/env sh
# builds the Go core into app/libs/arghzprint.aar — run before building the APK
set -e
cd "$(dirname "$0")/.."
# amd64 is only there so the emulator works; tablets are arm/arm64
# -s -w drops DWARF + symbol table (aar 37MB → 17MB); panic traces still name functions
gomobile bind -target=android/arm64,android/arm,android/amd64 -androidapi 26 -ldflags="-s -w" \
  -o android/app/libs/arghzprint.aar ./mobile
echo "built android/app/libs/arghzprint.aar"
