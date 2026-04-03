#!/usr/bin/env bash
set -e

REPO="alexli18/claude-king"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
BINARIES=("king" "king-vassal" "kingctl")

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Get latest release tag
TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
if [ -z "$TAG" ]; then
  echo "Could not fetch latest release."
  echo "Build from source: git clone https://github.com/$REPO && cd claude-king && make install-user"
  exit 1
fi

echo "Installing Claude King $TAG ($OS/$ARCH) to $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR"

for BIN in "${BINARIES[@]}"; do
  URL="https://github.com/$REPO/releases/download/$TAG/${BIN}_${OS}_${ARCH}"
  curl -fsSL "$URL" -o "$INSTALL_DIR/$BIN"
  chmod +x "$INSTALL_DIR/$BIN"
  echo "  ✓ $BIN"
done

# Patch PATH if needed
if ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
  PATCHED=0
  for RC in "$HOME/.zshrc" "$HOME/.bashrc"; do
    if [ -f "$RC" ]; then
      if ! grep -qF "$INSTALL_DIR" "$RC"; then
        echo "export PATH=\"$INSTALL_DIR:\$PATH\"" >> "$RC"
        echo "Added $INSTALL_DIR to PATH in $RC"
        PATCHED=1
      fi
    fi
  done
  if [ "$PATCHED" -eq 0 ]; then
    echo "Add to your shell config: export PATH=\"$INSTALL_DIR:\$PATH\""
  else
    echo "Restart your shell or run: export PATH=\"$INSTALL_DIR:\$PATH\""
  fi
fi

echo ""
echo "Claude King $TAG installed successfully."
echo "Run: king up"
