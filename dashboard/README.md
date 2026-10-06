# step-ca Dashboard (Go + Svelte)

Prototipe dashboard untuk mengelola penerbitan sertifikat step-ca lewat HTTP API — **tanpa step CLI** sama sekali.

## Arsitektur

```
┌──────────────┐   POST /api/certificates  ┌────────────────────┐  POST /sign   ┌─────────┐
│  Svelte UI   │  {"subject":"dev01"}      │  Go backend :8080  │  {csr, ott}   │ step-ca │
│  (Vite :5173)│ ─────────────────────────►│  1. gen key + CSR  │ ─────────────►│  :9000  │
│              │                           │  2. sign OTT (JWT) │               └─────────┘
│              │◄──── crt + key + chain ───│  3. POST /sign     │
└──────────────┘                           └────────────────────┘
```

Konsep penting: private key **provisioner** (bukan kunci CA!) di-decrypt sekali lalu disimpan di
`server/provisioner.jwk.json`. Backend memakainya untuk menandatangani OTT (JWT ES256) — persis
yang dilakukan `step ca token`, tapi diimplementasi murni dengan `crypto/ecdsa` stdlib Go.

## Struktur

```
dashboard/
├── server/
│   ├── main.go                 # seluruh backend (stdlib only, tanpa dependency)
│   ├── provisioner.jwk.json    # private JWK provisioner (SECRET, chmod 600)
│   └── go.mod
└── ui/                         # Svelte 5 + Vite
    ├── src/App.svelte
    └── vite.config.js          # proxy /api → localhost:8080
```

## Menjalankan

Terminal 1 — pastikan step-ca dan EMQX jalan:

```bash
docker compose up -d        # step-ca
```

Terminal 2 — backend Go:

```bash
cd dashboard/server
go run .                    # atau: go build -o dashboard-server . && ./dashboard-server
```

Terminal 3 — frontend:

```bash
cd dashboard/ui
npm install
npm run dev                 # buka http://localhost:5173
```

> Konfigurasi via env: `LISTEN` (default `:8080`), `CA_URL` (`https://localhost:9000`),
> `CA_ROOT` (`../../data/step/certs/root_ca.crt`), `PROVISIONER_NAME` (`admin`),
> `PROVISIONER_JWK` (`provisioner.jwk.json`).

## API Dashboard

| Method | Path | Fungsi |
|---|---|---|
| GET | `/api/health` | Status CA (proxy ke `/health` + `/version`) |
| GET | `/api/ca/roots` | Root CA PEM (proxy ke `/roots`) |
| POST | `/api/certificates` | Issue sertifikat baru. Body: `{"subject": "...", "sans": ["..."], "duration": "720h", "provisioner": "admin"}` |
| GET | `/api/certificates` | Daftar sertifikat yang pernah diterbitkan dari dashboard ini |
| GET | `/api/admin/provisioners` | Daftar provisioner (read-only) |

## Kelola provisioner — via Admin API murni

Fitur create/delete provisioner di dashboard berjalan lewat **Admin API step-ca** (pure HTTP).
Autentikasinya memakai **X5C JWT**: dashboard otomatis menerbitkan cert untuk subject super admin
(`step`) memakai provisioner JWK, lalu menandatangani token admin dengan cert tersebut.

> ⚠️ **Quirk krusial step-ca v0.30.2**: middleware auth TIDAK melakukan strip prefix `Bearer `.
> Header `Authorization` harus diisi **token murni tanpa "Bearer "** — jika tidak, go-jose
> mendekode `"BearereyJ..."` sebagai base64 dan menghasilkan error parse misterius
> (`invalid character \x05` / `illegal base64 data at input byte N`).
> Detail: deskripsi folder Admin API di `step-ca.postman_collection.json`
> + [smallstep/cli#860](https://github.com/smallstep/cli/issues/860).

Setelah provisioner dibuat dari UI:
- private JWK-nya otomatis disimpan sebagai `server/provisioner-<nama>.jwk.json` → langsung bisa dipakai issue
- hapus provisioner dari UI juga menghapus file kuncinya

Alternatif manual via CLI (menulis DB langsung, tanpa HTTP):

```bash
# buat provisioner JWK baru (1 tahun, untuk device misalnya)
docker exec step-ca step ca provisioner add <nama> --type JWK --create \
  --password-file /home/step/secrets/password \
  --x509-default-dur 8760h --x509-max-dur 8760h

# daftar
docker exec step-ca step ca provisioner list --ca-url https://localhost:9000 --root /home/step/certs/root_ca.crt

# hapus
docker exec step-ca step ca provisioner remove <nama>
```

## Panduan download di UI

Setelah issue, UI menampilkan 3 kartu panduan pemasangan. Satu cert yang sama bisa dipakai
untuk semua peran karena EKU-nya `ServerAuth + ClientAuth`:

| Peran | File yang dibutuhkan | Isi file |
|---|---|---|
| **Server — TLS** (satu arah) | `server.crt`, `server.key` | crt = leaf + intermediate; key = private key |
| **Server — mTLS** | + `ca-chain.crt` | chain = intermediate + root (trust store utk verifikasi client cert) |
| **Client — mTLS** | `client.crt`, `client.key`, `root_ca.crt` | crt = leaf saja; root = trust anchor |

Bundle dibentuk di sisi UI: `server.crt = crt + ca`, `ca-chain.crt = ca + root` — tanpa duplikat.

## Apa yang dilakukan backend saat "Terbitkan" diklik

1. **Generate EC P-256 key + CSR** (`crypto/x509`) — private key dibuat server-side
2. **Sign OTT**: JWT ES256 dengan header `{alg, kid, typ}` dan claims
   `{aud: <caurl>/1.0/sign, iss, sub, sans, nbf: now-1m, exp: now+5m, jti: random}`
   — tanda tangan pakai format raw `R||S` 64 byte (bukan DER), sesuai spec JWS
3. **`POST <caurl>/sign`** dengan `{"csr": ..., "ott": ...}` — TLS client memverifikasi CA
   memakai `root_ca.crt` (bukan skip-verification)
4. Parse sertifikat hasil (serial, notAfter) → simpan ke list in-memory → return ke UI

## Batasan prototipe (untuk produksi)

- List sertifikat masih in-memory (hilang saat restart) → pakai SQLite/Postgres
- Private key cert dikembalikan ke browser → idealnya generate di sisi device/client
- Belum ada auth di dashboard → tambahkan login/API key
- `provisioner.jwk.json` adalah **secret** — jangan di-commit; rotasi via Admin API
- Belum ada fitur renew/revoke → endpoint-nya tersedia di step-ca (`/renew`, `/revoke`),
  tinggal ditambah handler yang sama polanya dengan `handleIssue`
