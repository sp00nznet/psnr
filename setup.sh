#!/bin/sh
# psnr quick start: checks for Go, builds the server exactly as README
# "Step by step" does, and leaves a launcher here.
cd "$(dirname "$0")" || exit 1
LOG="$(pwd)/setup.log"
echo "psnr setup $(date)" > "$LOG"

if ! command -v go >/dev/null 2>&1; then
  echo "Go is needed to build the server. Install it from https://go.dev/dl/"
  echo "(or your package manager: apt install golang / brew install go) and run ./setup.sh again."
  exit 1
fi

echo "Building the server..."
if ! (cd server && go build -o psnr . >> "$LOG" 2>&1); then
  echo "The build failed. Details are in $LOG"
  exit 1
fi

cat > start-psnr.sh <<'EOS'
#!/bin/sh
cd "$(dirname "$0")/server" && exec ./psnr
EOS
chmod +x start-psnr.sh
echo "Done. Run ./start-psnr.sh, then open http://127.0.0.1:36101/"
