#!/usr/bin/env sh
# Terbitkan satu leaf certificate lewat API dashboard (bukan langsung ke step-ca),
# sehingga setiap sertifikat contoh tercatat di tab Certificates dashboard.
# Dashboard harus jalan: cd dashboard/server && make run
#
#   ./gen-cert.sh <out-dir> <file-name> <common-name> [extra-san ...]
#
# Env opsional:
#   DASHBOARD_URL  default http://localhost:8080
#   PROVISIONER    default admin
#   DURATION       mis. 24h / 720h (default: default provisioner)
#
# Hasil di <out-dir>:
#   <file-name>.crt  bundle leaf + intermediate
#   <file-name>.key  private key
#   root_ca.crt      trust anchor
#   ca-chain.crt     intermediate + root (untuk memverifikasi client cert)
set -eu

if [ $# -lt 3 ]; then
  echo "usage: $0 <out-dir> <file-name> <common-name> [extra-san ...]" >&2
  exit 1
fi

OUT=$1 FILE=$2 CN=$3
shift 3
API=${DASHBOARD_URL:-http://localhost:8080}

mkdir -p "$OUT"

# body request: subject + SANs (dashboard otomatis menambahkan CN sebagai SAN)
BODY=$(python3 -c '
import json, os, sys
print(json.dumps({
    "subject": sys.argv[1],
    "sans": sys.argv[2:],
    "provisioner": os.environ.get("PROVISIONER", "admin"),
    "duration": os.environ.get("DURATION", ""),
}))' "$CN" "$@")

RESP=$(curl -sS -X POST "$API/api/certificates" \
  -H 'Content-Type: application/json' -d "$BODY")
ROOTS=$(curl -sS "$API/api/ca/roots")

# pecah response JSON ke file: bundle (crt + intermediate), key, root, chain
python3 -c '
import json, os, sys
out, name, resp, roots = sys.argv[1:5]
r = json.loads(resp)
if "crt" not in r:
    sys.exit("dashboard error: " + r.get("error", resp))
root = json.loads(roots)["crts"][0]
def write(fn, s, mode=0o644):
    p = os.path.join(out, fn)
    with open(p, "w") as f:
        f.write(s.strip() + "\n")
    os.chmod(p, mode)
write(name + ".crt", r["crt"].strip() + "\n" + r["ca"])
write(name + ".key", r["key"], 0o600)
write("root_ca.crt", root)
write("ca-chain.crt", r["ca"].strip() + "\n" + root)
print("✓ %s/%s.crt  CN=%s  serial=%s  exp=%s" % (out, name, r["subject"], r["serial"], r["notAfter"]))
' "$OUT" "$FILE" "$RESP" "$ROOTS"
