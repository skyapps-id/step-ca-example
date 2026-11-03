# Contoh Implementasi

Tiga cara memakai sertifikat dari step-ca, dari yang paling sederhana sampai otomatis penuh.

Prasyarat:
- step-ca jalan: `docker compose up -d` di root
- dashboard jalan (untuk emqx & grpc): `cd dashboard/server && make run`

| Contoh | Pola | Cara dapat sertifikat | Siapa yang dibuktikan |
|---|---|---|---|
| [emqx/](emqx/README.md) | **TLS** (MQTT broker) | `gen-cert.sh` → API dashboard | Server saja |
| [grpc/](grpc/README.md) | **mTLS** (service Go) | `gen-cert.sh` → API dashboard | Server **dan** client |
| [caddy/](caddy/README.md) | **ACME** (web server) | Caddy langsung ke step-ca (provisioner ACME) | Server, issue + renew otomatis |

## gen-cert.sh

Helper untuk menerbitkan satu leaf certificate lewat **API dashboard**
(`POST /api/certificates`), bukan langsung ke step-ca. Dengan begitu setiap
sertifikat contoh tercatat di tab **Certificates** dashboard dan bisa di-renew dari sana,
sekaligus menjadi bukti bahwa alur issue dashboard (CSR → OTT → `/sign`) benar.

```bash
./gen-cert.sh <out-dir> <file-name> <common-name> [extra-san ...]

./gen-cert.sh emqx/certs emqx-server emqx.local emqx localhost 127.0.0.1
./gen-cert.sh grpc/certs client sensor-01
```

Env opsional: `DASHBOARD_URL` (default `http://localhost:8080`), `PROVISIONER`
(default `admin`), `DURATION` (mis. `24h`, `720h`).

Menghasilkan `<file-name>.crt` (bundle leaf + intermediate), `<file-name>.key`,
`root_ca.crt`, dan `ca-chain.crt` di `<out-dir>`. Folder `certs/` di setiap
contoh di-ignore git.

Caddy pengecualian: ACME adalah protokol antara Caddy dan step-ca, jadi
sertifikatnya tidak lewat (dan tidak tercatat di) dashboard.
