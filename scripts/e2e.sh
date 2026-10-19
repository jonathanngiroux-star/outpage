#!/bin/sh
# e2e.sh — Outpage end-to-end script: builds the binary and walks the
# whole product through the CLI, then verifies the public page over
# real HTTP, cross-checks the TUI's fixtures, and asserts the donation
# gates. Exit 0 = the v0.1 release gate passes. Zero dependencies
# beyond go, curl, jq.
set -eu

cd "$(dirname "$0")/.."
BIN=$(mktemp -d)/outpage
DB=$(mktemp -d)/e2e.db
PORT=18765

echo "== build =="
go build -o "$BIN" ./cmd/outpage

echo "== donate gate =="
OUT=$("$BIN" donate)
echo "$OUT" | grep -Fx "Ethereum / USDC (ERC-20): 0x85ee7E71f762d772599cbF1EC20E651B30657521"
echo "$OUT" | grep -Fx "Bitcoin: bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg"
test "$(echo "$OUT" | grep -c "0x85ee7E71")" -eq 1
test "$(echo "$OUT" | grep -c "bc1qxe")" -eq 1
echo "   both addresses exact-once: OK"

echo "== lifecycle =="
"$BIN" component add --db "$DB" --name "API" --description "Public REST" >/dev/null
"$BIN" component add --db "$DB" --name "Web App" >/dev/null
"$BIN" subscriber add --db "$DB" --kind email --address ops@example.net >/dev/null
"$BIN" subscriber add --db "$DB" --kind webhook --address https://hooks.example.net/x --secret s3cr3t >/dev/null
"$BIN" incident open --db "$DB" --name "Elevated API errors" --impact minor --components "API, Web App" >/dev/null
"$BIN" incident update --db "$DB" --id 1 --status identified --body "Bad deploy rolled back." >/dev/null
"$BIN" incident update --db "$DB" --id 1 --status resolved --body "Error rates at baseline." >/dev/null
"$BIN" maintenance add --db "$DB" --name "DB upgrade" --components "API" \
  --start 2026-10-25T01:00:00Z --end 2026-10-25T03:00:00Z >/dev/null
echo "   component/incident/maintenance/subscribers created: OK"

echo "== illegal transition refused =="
if "$BIN" incident update --db "$DB" --id 1 --status monitoring --body "zombie" 2>/dev/null; then
  echo "FAIL: update after resolve accepted"; exit 1
fi
echo "   resolved-is-terminal: OK"

echo "== serve over real HTTP =="
"$BIN" serve --db "$DB" --http "127.0.0.1:$PORT" &
SRV=$!
trap 'kill $SRV 2>/dev/null || true' EXIT
for i in $(seq 1 15); do curl -sf "http://127.0.0.1:$PORT/" >/dev/null && break; sleep 1; done
PAGE=$(curl -sf "http://127.0.0.1:$PORT/")
echo "$PAGE" | grep -q "API"
echo "$PAGE" | grep -q "Web App"
echo "$PAGE" | grep -q "Elevated API errors"
echo "$PAGE" | grep -q "Rollback complete\|at baseline"
echo "$PAGE" | grep -q "DB upgrade"
echo "$PAGE" | grep -q "0x85ee7E71f762d772599cbF1EC20E651B30657521"
if echo "$PAGE" | grep -q "ops@example.net"; then
  echo "FAIL: subscriber email leaked into public HTML"; exit 1
fi
echo "$PAGE" | grep -q "data-status"
DONATE=$(curl -sf "http://127.0.0.1:$PORT/donate")
echo "$DONATE" | grep -q "bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg"
curl -sf -o /dev/null -w "%{http_code}" "http://127.0.0.1:$PORT/admin" | grep -q 404 || {
  echo "FAIL: /admin must 404"; exit 1; }
echo "   page + donate + 404 + no email leak: OK"

echo "== migrate contract =="
"$BIN" migrate --from=statuspage --dry-run --format=json \
  --input testdata/fixtures/statuspage/sample-export.json > /tmp/e2e-report.json
jq -e '.source=="statuspage" and .dry_run==true
       and .counts.mapped==7 and .counts.partial==3 and .counts.unmapped==2
       and (.mappings | map(select(.result=="unmapped") | .key)) == ["subscriber.sms","metrics.uptime"]
       and ((.risks | map(.key)) | index("webhook.secrets")) != null' /tmp/e2e-report.json >/dev/null
diff -u testdata/golden/statuspage-report.json /tmp/e2e-report.json >/dev/null \
  || { echo "FAIL: golden drift"; exit 1; }
echo "   fixture contract + golden: OK"

echo "== TUI non-TTY never hangs =="
timeout 5 "$BIN" tui --db "$DB" | grep -q "interactive terminal UI"
echo "   non-TTY hint: OK"

echo
echo "E2E PASS — v0.1 release gate green."
