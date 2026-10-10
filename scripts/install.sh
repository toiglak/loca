#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
mkdir -p build "$HOME/Applications/Loca.app/Contents/MacOS" "$HOME/.local/bin"
go build -trimpath -o build/Loca ./cmd/loca
# Atomic replacement also permits upgrading a running executable.
cp build/Loca "$HOME/Applications/Loca.app/Contents/MacOS/Loca.new"
mv "$HOME/Applications/Loca.app/Contents/MacOS/Loca.new" "$HOME/Applications/Loca.app/Contents/MacOS/Loca"
cat > "$HOME/Applications/Loca.app/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleExecutable</key><string>Loca</string><key>CFBundleIdentifier</key><string>dev.iglak.Loca</string><key>CFBundleName</key><string>Loca</string><key>CFBundleDisplayName</key><string>Loca</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleVersion</key><string>1</string><key>CFBundleShortVersionString</key><string>1.0.0</string><key>LSUIElement</key><true/></dict></plist>
PLIST
codesign --force --sign - "$HOME/Applications/Loca.app"
ln -sfn "$HOME/Applications/Loca.app/Contents/MacOS/Loca" "$HOME/.local/bin/loca"
/usr/bin/touch "$HOME/Applications/Loca.app"
printf 'Built Loca.app. Run: loca set feed 4173; loca start; loca trust\n'
