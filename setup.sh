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

# Other players connect on TCP 36100. Say how to open it if a firewall is on;
# changing the firewall is left to the admin.
if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
  echo "ufw is on. To let other players connect: sudo ufw allow 36100/tcp"
elif command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
  echo "firewalld is on. To let other players connect:"
  echo "  sudo firewall-cmd --permanent --add-port=36100/tcp && sudo firewall-cmd --reload"
fi
