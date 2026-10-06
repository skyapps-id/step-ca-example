# EMQX + mTLS dengan step-ca

Dokumentasi langkah-langkah membuat sertifikat untuk EMQX (MQTT broker) dan client mTLS menggunakan step-ca di project ini.

## Arsitektur

```
                        ┌─────────────────────-────┐
                        │  step-ca (localhost:9000)│
                        │  Root CA → Intermediate  │
                        └───────────┬─────-────────┘
                                    │ issue (sekali, via step CLI)
                    ┌───────────────┼───────────────────┐
                    ▼               ▼                   ▼
            emqx-server.crt   client-alice.crt
            (SAN: emqx.local,  (CN: client-alice)
             localhost,
             127.0.0.1)
                    │
                    ▼
        ┌──────────────────────┐        mTLS        ┌────────────────┐
        │  EMQX listener :8883 │◄──────────────────►│  MQTT client   │
        │  verify_peer         │   cert + key       │  (alice, dsb)  │
        │  wajib client cert   │                    └────────────────┘
        └──────────────────────┘
```

Konsep:
- **Server cert** → membuktikan identitas broker ke client (ServerAuth)
- **Client cert** → membuktikan identitas device ke broker (ClientAuth). `verify = verify_peer` + `fail_if_no_peer_cert = true` membuat koneksi TANPA client cert ditolak = **mTLS**
- `peer_cert_as_username = cn` → username MQTT otomatis diambil dari CN client cert

---

## Langkah 1 — Naikkan batas durasi sertifikat

Secara default provisioner JWK hanya boleh menerbitkan sertifikat **24 jam**. Saat mencoba minta 30 hari (`--not-after 720h`), CA menolak:

```
The request was forbidden by the certificate authority:
requested duration of 720h1m0s is more than the authorized maximum
certificate duration of 24h1m0s.
```

Ini fitur keamanan (policy enforcement), bukan bug. Untuk demo ini batasnya dinaikkan menjadi 30 hari dengan mengedit `data/step/config/ca.json` — tambahkan `claims` di provisioner `admin`:

```json
{
  "type": "JWK",
  "name": "admin",
  "claims": {
    "defaultTLSCertDuration": "720h",
    "maxTLSCertDuration": "720h"
  },
  "key": { ... }
}
```

Lalu restart CA:

```bash
docker compose restart step-ca
```

> Catatan produksi: kebijakan step-ca adalah sertifikat **pendek + auto-renew**, bukan umur panjang. Untuk produksi gunakan 24 jam + cron `step ca renew`.

## Langkah 2 — Terbitkan sertifikat

`step` CLI tidak ada di host macOS, jadi semua perintah dijalankan **di dalam container** step-ca, lalu hasilnya di-copy keluar.

```bash
# Server cert untuk EMQX (SAN harus cocok dengan hostname yang dipakai client)
docker exec step-ca step ca certificate emqx.local /tmp/emqx-server.crt /tmp/emqx-server.key \
  --provisioner admin \
  --password-file /home/step/secrets/password \
  --ca-url https://localhost:9000 \
  --root /home/step/certs/root_ca.crt \
  --san localhost --san 127.0.0.1 \
  --not-after 720h --force

# Client cert (CN = identitas device)
docker exec step-ca step ca certificate client-alice /tmp/client-alice.crt /tmp/client-alice.key \
  --provisioner admin --password-file /home/step/secrets/password \
  --ca-url https://localhost:9000 --root /home/step/certs/root_ca.crt \
  --not-after 720h --force

# Untuk device lain, ulangi pola yang sama dengan CN berbeda
# (satu cert per device — jangan share satu cert ke banyak device)

# Copy root CA + hasil ke folder mqtt/certs/
docker cp step-ca:/tmp/emqx-server.crt  certs/ && docker cp step-ca:/tmp/emqx-server.key certs/
docker cp step-ca:/tmp/client-alice.crt certs/ && docker cp step-ca:/tmp/client-alice.key certs/
docker cp step-ca:/tmp/root_ca.crt      certs/
chmod 600 certs/*.key
```

Penjelasan flag penting:

| Flag | Arti |
|---|---|
| `--provisioner admin` | Pakai pintu masuk JWK `admin` |
| `--password-file` | Password untuk dekripsi kunci provisioner (file `secrets/password`) |
| `--san` | Tambah Subject Alternative Name (wajib untuk validasi hostname TLS) |
| `--not-after 720h` | Masa berlaku 30 hari |
| `--force` | Timpa file jika sudah ada (saat re-issue/renew manual) |

Yang terjadi di balik layar: CLI membuat key + CSR lokal → minta OTT (JWT) dari provisioner → `POST /sign` ke CA → CA verifikasi token & sign → sertifikat jadi. Private key **tidak pernah** dikirim ke CA.

## Cara Alternatif — Issue via HTTP API (tanpa `step ca certificate`)

Bagian penerbitan sertifikat bisa dilakukan murni lewat HTTP API (pakai `curl` atau collection Postman `step-ca.postman_collection.json` di root project). Yang berbeda hanya pembuatan token-nya:

```bash
# 1. Bikin key + CSR pakai openssl (atau step, atau library apa pun)
openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
  -keyout api-demo.key -out api-demo.csr -subj "/CN=api-demo.local"

# 2. Buat OTT (JWT) — satu-satunya step yang butuh CLI/library
TOKEN=$(docker exec step-ca step ca token api-demo.local \
  --provisioner admin --password-file /home/step/secrets/password \
  --ca-url https://localhost:9000 --root /home/step/certs/root_ca.crt)

# 3. POST /sign — murni API
python3 -c "import json,sys; print(json.dumps({'csr': open('api-demo.csr').read(), 'ott': sys.argv[1]}))" \
  "$TOKEN" > sign-body.json

curl -sk -X POST https://localhost:9000/sign \
  -H 'Content-Type: application/json' -d @sign-body.json -o sign-resp.json

# 4. Ambil cert dari response
python3 -c "import json; open('api-demo.crt','w').write(json.load(open('sign-resp.json'))['crt'])"
```

Response `POST /sign` (HTTP 201):

```json
{ "crt": "PEM", "ca": "PEM", "certChain": ["PEM", ...], "tlsOptions": {...} }
```

### Kenapa tidak 100% API?

Pembuatan OTT **sengaja tidak dijadikan endpoint CA**. OTT adalah JWT yang di-sign pakai **private key provisioner**, dan private key itu tidak pernah ada di CA — CA hanya menyimpan public key-nya di `ca.json`. Kalau CA punya endpoint "buatkan token", CA harus memegang private key tersebut, dan siapa pun yang bisa akses network bisa menerbitkan sertifikat apa pun. Jadi pembagian tugasnya memang:

| Tugas | Di mana jalan | Alasan |
|---|---|---|
| Bikin private key + CSR | Client | Private key tidak boleh keluar dari pemiliknya |
| Sign OTT (JWT) | Client | Butuh private key provisioner (rahasia) |
| Verifikasi token + issue cert | API `POST /sign` | Tugas CA |

Isi payload OTT (setelah di-decode):

```json
{
  "aud": "https://localhost:9000/1.0/sign",
  "exp": 1791211495,
  "iss": "admin",
  "jti": "3e485d37...",
  "sans": ["api-demo.local"],
  "sub": "api-demo.local"
}
```

- `exp` → token hanya berlaku **5 menit**
- `jti` → ID unik sekali pakai, dicatat di database CA (replay ditolak)
- `sans` → CSR hanya boleh meminta domain yang tercantum di sini

> Pola produksi: script/CI menjalankan `step ca token` lalu `POST /sign` (atau request ke Postman dengan variable `{{ott}}` + `{{csr}}`). Jika ingin alur tanpa CLI sama sekali, gunakan provisioner lain — **ACME** (otomatis penuh, dipakai certbot/cert-manager), **OIDC** (token dari SSO), atau **Cloud** (instance identity AWS/GCP/Azure).

## Langkah 3 — Struktur file

```
mqtt/
├── docker-compose.yml   # service EMQX (project terpisah dari step-ca)
├── emqx.conf            # konfigurasi listener mTLS
└── certs/
    ├── root_ca.crt        # ROOT — trust anchor, dipasang di semua CLIENT
    ├── ca-chain.crt       # bundle intermediate + root → dipasang di EMQX utk verifikasi client cert
    ├── emqx-server.crt    # cert broker, sudah bundle leaf + intermediate (SAN: emqx.local, localhost, 127.0.0.1)
    ├── emqx-server.key
    ├── client-alice.crt   # cert client (CN: client-alice)
    └── client-alice.key
```

> Catatan: `step ca certificate` menulis cert dalam bentuk **bundle** — file
> `emqx-server.crt` sudah berisi leaf + intermediate sekaligus, jadi tidak perlu
> file fullchain terpisah. Intermediate asli tersedia di `data/step/certs/intermediate_ca.crt`.

### Kenapa ada file bundle (ca-chain)?

Dua sisi koneksi TLS butuh rantai yang berbeda:

| Sisi | File | Isinya | Fungsi |
|---|---|---|---|
| EMQX → verifikasi **client cert** | `ca-chain.crt` | intermediate + root | Client cert di-sign intermediate; tanpa intermediate di trust store, verifikasi gagal `unknown CA`. Root tetap wajib sebagai anchor. |
| EMQX → **dikirim** ke client | `emqx-server.crt` | server cert + intermediate (bundle bawaan step CLI) | Client harus bisa memanjat rantai sampai root-nya untuk verifikasi server. |
| Client → trust | `root_ca.crt` | root saja | Trust anchor — tidak boleh dikirim server saat handshake, harus didapat client dari sumber terpercaya. |

## Langkah 4 — Konfigurasi EMQX

Isi `emqx.conf` (EMQX 5.x):

```hocon
listeners.ssl.default {
  bind = "0.0.0.0:8883"
  ssl_options {
    cacertfile = "/opt/emqx/etc/certs/ca-chain.crt"        # verifikasi client cert (intermediate + root)
    certfile   = "/opt/emqx/etc/certs/emqx-server.crt"    # bundle leaf + intermediate (dikirim ke client)
    keyfile    = "/opt/emqx/etc/certs/emqx-server.key"
    verify = verify_peer              # wajib verifikasi client cert
    fail_if_no_peer_cert = true       # tolak koneksi tanpa client cert
  }
}

mqtt {
  peer_cert_as_username = cn          # username = CN client cert
}
```

## Langkah 5 — Jalankan EMQX

```bash
docker compose -f mqtt/docker-compose.yml up -d
```

Port yang dipublish: `1883` (TCP polos), `8883` (TLS/mTLS), `18083` (dashboard, default login `admin/public`).

## Langkah 6 — Test mTLS

```bash
# 1. Tanpa client cert → HARUS DITOLAK (fail_if_no_peer_cert)
mosquitto_pub -h localhost -p 8883 \
  --cafile mqtt/certs/root_ca.crt \
  -t test -m "harusnya gagal"

# 2. Dengan client cert → HARUS SUKSES
mosquitto_pub -h localhost -p 8883 \
  --cafile mqtt/certs/root_ca.crt \
  --cert mqtt/certs/client-alice.crt \
  --key  mqtt/certs/client-alice.key \
  -t test -m "hello dari alice"

# 3. Alternatif tanpa mosquitto — lihat handshake TLS-nya
openssl s_client -connect localhost:8883 \
  -CAfile mqtt/certs/root_ca.crt \
  -cert mqtt/certs/client-alice.crt \
  -key  mqtt/certs/client-alice.key
```

Kalau pakai `mosquitto_sub` untuk subscribe, lihat di dashboard EMQX (http://localhost:18083) → Clients: username-nya akan `client-alice` (dari CN).

---

## Troubleshooting

| Gejala | Penyebab | Solusi |
|---|---|---|
| `certificate verify failed` di client | Client tidak percaya cert broker | Pastikan `--cafile root_ca.crt` dan hostname = salah satu SAN (`localhost` / `emqx.local`) |
| `alert certificate required` / langsung disconnect | Koneksi tanpa client cert | Normal — mTLS wajib. Tambahkan `--cert` + `--key` |
| `certificate unknown` / `unknown CA` saat mTLS | Server tidak bisa membangun rantai cert client (intermediate tidak ada di `cacertfile`) | Pastikan `cacertfile` = `ca-chain.crt` (intermediate + root), bukan root saja |
| Client cert ditolak padahal valid | EMQX belum reload cert / cert expired | Restart EMQX, cek `Not After` dengan `step certificate inspect` |
| `requested duration ... more than authorized maximum` | Batas claim provisioner | Lihat Langkah 1 |
| Port 8883 tidak terbuka | EMQX gagal load cert | `docker logs emqx` — biasanya path cert salah |
| Crash `wss:default ... no_cert` saat start | Listener WSS bawaan EMQX kehilangan cert default (folder `certs/` tertimpa mount) | Matikan: tambah `listeners.wss.default { enable = false }` di `emqx.conf` |

## Renewal

Sertifikat berlaku 30 hari. Cara renew manual:

```bash
# 1. Masukkan cert lama ke container (renew butuh cert+key lama sebagai bukti)
docker cp certs/emqx-server.crt  step-ca:/tmp/
docker cp certs/emqx-server.key  step-ca:/tmp/

# 2. Renew — TIDAK perlu password/OTT, cukup cert lama yang masih valid
docker exec step-ca step ca renew --force \
  --ca-url https://localhost:9000 --root /home/step/certs/root_ca.crt \
  /tmp/emqx-server.crt /tmp/emqx-server.key

# 3. Ambil hasilnya & restart EMQX
docker cp step-ca:/tmp/emqx-server.crt certs/
docker restart emqx
```

> Cara yang benar untuk produksi: mount `certs/` ke sebuah cron/job yang menjalankan `step ca renew` secara berkala (mis. tiap jam, karena step-ca hanya mengizinkan renew setelah ~2/3 masa aktif), lalu EMQX akan memuat ulang cert.
