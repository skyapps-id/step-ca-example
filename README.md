# step-ca Example

Lab lengkap belajar **Private PKI** dengan [step-ca](https://github.com/smallstep/certificates):
CA server di Docker, dashboard (Go + Svelte) untuk menerbitkan, memperbarui, dan
mengelola sertifikat & provisioner, plus contoh implementasi **TLS** (EMQX),
**mTLS** (gRPC), dan **ACME** (Caddy).

```mermaid
flowchart LR
  subgraph CA["step-ca :9000"]
    R["Root CA"] --> I["Intermediate CA"]
  end
  D["Dashboard Go + Svelte<br/>API :8080 · UI :5173"] -- "issue · renew (JWK OTT)<br/>provisioner (Admin API X5C)" --> CA
  CA -. "emqx-server.crt" .-> E["EMQX broker :8883"]
  CA -. "server.crt · client.crt" .-> G["gRPC server :50051"]
  C["Caddy :8443"] <-->|"ACME"| CA
  M["MQTT client"] -- "TLS" --> E
  GC["gRPC client"] <-->|"mTLS"| G
  B["browser / curl"] -- "HTTPS" --> C
```

## Tampilan

![Dashboard step-ca](docs/screenshot.png)

## Struktur project

```
├── docker-compose.yml              # step-ca (+ init bootstrap otomatis)
├── .env / .env.example             # STEP_CA_VERSION=0.30.2 (pin versi!), CA_NAME, dst
├── data/step/                      # STEPPATH: kunci CA, certs, config, DB (JANGAN di-commit)
├── example/                        # contoh implementasi (lihat example/README.md)
│   ├── gen-cert.sh                 # helper issue leaf cert via API dashboard
│   ├── emqx/                       # MQTT broker + TLS
│   ├── grpc/                       # service Go + mTLS
│   └── caddy/                      # web server + ACME (issue/renew otomatis)
├── docs/                           # dokumen tambahan (issue via HTTP API, dst)
├── dashboard/
│   ├── README.md                   # dokumentasi dashboard
│   ├── server/                     # backend Go
│   │   ├── main.go                 # entry point: wiring + serve
│   │   └── internal/
│   │       ├── config/             # konfigurasi dari env
│   │       ├── types/              # struct bersama
│   │       ├── keys/               # JWK, CSR, thumbprint
│   │       ├── ca/                 # klien step-ca (OTT, /sign, /renew)
│   │       ├── store/              # persistensi sertifikat terbit
│   │       ├── admin/              # service Admin API (X5C)
│   │       ├── handlers/           # HTTP handlers
│   │       └── issued/             # cert+key tersimpan (runtime, untuk Renew)
│   └── ui/                         # Svelte 5 + Vite (dark mode)
└── step-ca.postman_collection.json
```

---

## 1. Setup server step-ca

### Jalankan

```bash
docker compose up -d
```

Saat pertama kali dijalankan, container `step-ca-init` otomatis:

1. Generate password CA → `data/step/secrets/password`
2. `step ca init --remote-management` — membuat Root CA + Intermediate CA +
   provisioner JWK `admin` + mengaktifkan **Admin API** + membuat **super admin**
   (subject default: `step`)

> ⚠️ Bootstrap super admin hanya bisa terjadi saat init fresh. Container
> `step-ca-init` idempotent: kalau `data/step/` sudah ada isinya, dia skip.

### Verifikasi

```bash
curl -sk https://localhost:9000/health          # → {"status":"ok"}
curl -sk https://localhost:9000/provisioners    # daftar provisioner
```

### File penting di `data/step/`

| File | Fungsi |
|---|---|
| `certs/root_ca.crt` | Root CA — **bagikan ke semua client** (trust anchor) |
| `certs/intermediate_ca.crt` | Intermediate — penandatangan harian |
| `secrets/password` | Password kunci CA & provisioner — **RAHASIA** |
| `config/ca.json` | Konfigurasi CA |
| `db/` | Database (provisioners, admins, ACME state) |

### Pin versi

`.env` memakai `STEP_CA_VERSION=0.30.2` — jangan `latest`, agar binary tidak
berubah diam-diam.

---

## 2. Membuat Intermediate CA tambahan

Satu instance step-ca hanya melayani **satu** intermediate (yang tercantum di
`config/ca.json`). Kalau mau memisahkan domain kepercayaan — misalnya
**Infrastructure**, **IoT**, dan **Kubernetes** — buat intermediate tambahan
yang ditandatangani Root yang sama.

> 📌 **Aturan penting: multi intermediate = multi instance.** Skema `ca.json`
> hanya punya satu pasang field `crt` + `key`, dan API `/sign` tidak punya
> parameter "pilih intermediate" — jadi tidak bisa memilih issuer per-request.
> Provisioner juga terdaftar per instance. Cara idiomatis step-ca: **satu
> intermediate = satu instance CA di port berbeda**:
>
> ```
> :9000 → intermediate default        (umum / dashboard)
> :9001 → IoT intermediate            (leaf pendek, policy ketat)
> :9002 → Kubernetes intermediate     (untuk cluster)
> ```
>
> "Memilih intermediate" = memilih CA URL tujuan. Detail pemakaian: bagian
> [Cara pakai intermediate baru](#cara-pakai-intermediate-baru) di bawah.

Membuat intermediate-nya sendiri adalah operasi **offline** (pakai kunci Root
langsung), karena Admin API step-ca tidak punya endpoint untuk membuat intermediate:

```bash
docker exec step-ca step certificate create "Example Infrastructure Intermediate CA" \
  /home/step/certs/infrastructure_ca.crt /home/step/secrets/infrastructure_ca_key \
  --profile intermediate-ca \
  --ca /home/step/certs/root_ca.crt --ca-key /home/step/secrets/root_ca_key \
  --password-file /home/step/secrets/password \
  --not-after 43800h   # 5 tahun

docker exec step-ca step certificate create "Example IoT Intermediate CA" \
  /home/step/certs/iot_ca.crt /home/step/secrets/iot_ca_key \
  --profile intermediate-ca \
  --ca /home/step/certs/root_ca.crt --ca-key /home/step/secrets/root_ca_key \
  --password-file /home/step/secrets/password \
  --not-after 43800h

docker exec step-ca step certificate create "Example Kubernetes Intermediate CA" \
  /home/step/certs/kubernetes_ca.crt /home/step/secrets/kubernetes_ca_key \
  --profile intermediate-ca \
  --ca /home/step/certs/root_ca.crt --ca-key /home/step/secrets/root_ca_key \
  --password-file /home/step/secrets/password \
  --not-after 43800h
```

Verifikasi:

```bash
docker exec step-ca step certificate inspect /home/step/certs/iot_ca.crt --short
# Issuer: root CA, Basic Constraints CA=true, pathlen=0
```

Hasilnya di `data/step/`:

| File | Fungsi |
|---|---|
| `certs/<nama>_ca.crt` | Sertifikat intermediate baru (publik — boleh dibagikan) |
| `secrets/<nama>_ca_key` | Private key intermediate (terenkripsi password CA) |

> ⚠️ Command di atas membaca **kunci Root langsung dari volume**. Di lab ini boleh,
> tapi prinsip produksi: kunci Root harus offline/HSM, proses sign intermediate
> dilakukan sekali di tempat yang aman.

### Cara pakai intermediate baru

Intermediate tambahan **tidak otomatis dipakai** instance step-ca yang berjalan —
dia tetap menandatangani leaf dengan `intermediate_ca.crt` lama. Pemakaian umum:

1. **Instance CA kedua (cara utama)** — tiap intermediate jadi "bintang" di
   instance-nya sendiri. Salin `iot_ca.crt` + `iot_ca_key` ke STEPPATH baru
   (sebagai `certs/intermediate_ca.crt` + `secrets/intermediate_ca_key`), buat
   `ca.json` sendiri, lalu jalankan container `step-ca` kedua di port berbeda.
   Contoh service compose untuk instance IoT:
   ```yaml
   step-ca-iot:
     image: smallstep/step-ca:${STEP_CA_VERSION:-latest}
     container_name: step-ca-iot
     restart: unless-stopped
     ports:
       - "9001:9000"
     environment:
       STEPPATH: /home/step
     volumes:
       # struktur folder sama seperti data/step/, tapi isinya iot_ca.crt/key
       - ./data/step-iot:/home/step
   ```
   Eksekusi awal `data/step-iot/`:
   ```bash
   mkdir -p data/step-iot/{certs,secrets,config,db}
   cp data/step/secrets/password data/step-iot/secrets/password
   cp data/step/certs/root_ca.crt data/step-iot/certs/
   cp data/step/certs/iot_ca.crt data/step-iot/certs/intermediate_ca.crt
   cp data/step/secrets/iot_ca_key data/step-iot/secrets/intermediate_ca_key
   # ca.json: copy dari data/step/config/ca.json lalu sesuaikan dns
   # (address tetap ":9000" — itu port di dalam container, host yang di-map 9001)
   ```
   Provisioner dibuat per instance (`step ca provisioner add ...` di container
   tersebut). Trik: **JWK provisioner yang sama bisa didaftarkan ke beberapa
   instance** — satu private key provisioner, cukup ganti `--ca-url` untuk
   memilih intermediate mana yang menandatangani.
2. **Terbit offline** — pakai intermediate itu langsung tanpa server CA:
   ```bash
   docker exec step-ca step certificate create "sensor-01" sensor.crt sensor.key \
     --profile leaf --san 10.0.0.5 --san sensor-01.local \
     --ca /home/step/certs/iot_ca.crt --ca-key /home/step/secrets/iot_ca_key \
     --password-file /home/step/secrets/password --not-after 2160h
   ```
3. **Distribusikan** — kirim `kubernetes_ca.crt` + key-nya ke cluster/sistem lain
   (mis. Kubernetes CFSSL/cert-manager, EMQX kedua) supaya punya CA sendiri di
   bawah Root yang sama.

Semua leaf dari intermediate baru tetap dipercaya client yang memegang
`root_ca.crt` — chain: Root → Intermediate baru → leaf.

---

## 3. Menerbitkan sertifikat

### Cara Dashboard

```bash
cd dashboard/server && make run    # API :8080 (atau: go run .)
cd dashboard/ui && npm run dev     # UI  :5173
```

Isi form subject / SANs / provisioner / durasi → **⚡ Issue Certificate** →
download file per peran di kartu **Deployment Guide** (Server TLS, Server mTLS,
Client mTLS). Riwayat + tombol **⟳ Renew** ada di tab **Certificates**.

> Alternatif tanpa dashboard: CLI langsung di container
> (`docker exec step-ca step ca certificate ... --provisioner admin ...`) —
> berguna saat dashboard sedang tidak berjalan.
> Atau murni lewat HTTP API: [docs/issue-via-api.md](docs/issue-via-api.md).

---

## 4. Mengelola provisioner

Provisioner = "pintu masuk" CA beserta policy-nya (siapa boleh minta cert,
durasi maksimum, dll). Pemegang private key provisioner-lah yang boleh minta cert.

### Cara Dashboard (Admin API murni)

Tab **Provisioners** → isi nama + durasi → **+ Add**. Dashboard otomatis:

1. Generate keypair provisioner baru
2. Kirim public JWK ke CA via Admin API (X5C)
3. Simpan private key-nya ke `dashboard/server/provisioner-<nama>.jwk.json`
   → langsung bisa dipakai issue, dan persisten antar restart

Tombol **Hapus** menghapus provisioner dari CA sekaligus file kunci lokalnya.

> Policy durasi enforcement-nya terjadi di CA: request melebihi claim → ditolak
> 403, apa pun client-nya. Default provisioner baru dari `step ca init` = 24 jam.

### Import provisioner buatan CLI ke dashboard

Provisioner yang dibuat lewat CLI tidak otomatis dikenal dashboard (dashboard
butuh private key-nya untuk sign OTT). Export & dekripsi:

```bash
docker exec step-ca step ca provisioner list \
  --ca-url https://localhost:9000 --root /home/step/certs/root_ca.crt > /tmp/prov.json
python3 -c "
import json
for p in json.load(open('/tmp/prov.json')):
    if p['name'] == '<nama>':
        open('/tmp/enc.jwe','w').write(p['encryptedKey'])"

docker cp /tmp/enc.jwe step-ca:/tmp/enc.jwe
docker exec step-ca sh -c 'step crypto jwe decrypt --password-file /home/step/secrets/password < /tmp/enc.jwe' \
  > dashboard/server/provisioner-<nama>.jwk.json
chmod 600 dashboard/server/provisioner-<nama>.jwk.json
# restart dashboard → provisioner otomatis termuat (glob provisioner-*.jwk.json)
```

> Provisioner yang dibuat **dari dashboard UI** tidak perlu langkah ini —
> private key-nya sudah otomatis disimpan & dimuat.

---

## 5. Renew

Renew step-ca memakai **cert+key lama sebagai kredensial mTLS** (`POST /renew`) —
private key tidak berubah. step-ca mengizinkan renew kapan pun selama cert masih valid.

- **Dashboard**: tab **Certificates** → tombol **⟳ Renew** per baris.
  Cert+key otomatis tersimpan di `dashboard/server/issued/` saat issue.
- **CLI**: `docker exec step-ca step ca renew --cert /tmp/x.crt --key /tmp/x.key ...`

---

## 6. Quirk yang perlu diketahui (v0.30.2)

1. **Admin API: header Authorization TANPA "Bearer "**. Middleware auth tidak
   melakukan strip pada prefix `Bearer ` — go-jose malah mendekode
   `"BearereyJ..."` sebagai base64 → error parse misterius
   (`invalid character \x05` / `illegal base64 data`). Dashboard sudah mengakali
   ini. Detail: deskripsi folder Admin API di `step-ca.postman_collection.json`
   + [smallstep/cli#860](https://github.com/smallstep/cli/issues/860).
2. **Perintah `step ca admin/provisioner` dari CLI container berjalan offline**
   (menulis DB langsung, bukan lewat HTTP) — makanya selalu berhasil.
3. **Super admin default bernama `step`** (bukan `admin`) — sumber:
   `pki.go` di repo smallstep/certificates.

---

## 7. Topik lanjutan

| Topik | Dokumen |
|---|---|
| EMQX + TLS | [example/emqx/README.md](example/emqx/README.md) |
| gRPC + mTLS (Go) | [example/grpc/README.md](example/grpc/README.md) |
| Caddy + ACME (issue/renew otomatis) | [example/caddy/README.md](example/caddy/README.md) |
| Issue sertifikat via HTTP API | [docs/issue-via-api.md](docs/issue-via-api.md) |
| Dashboard (Go + Svelte), arsitektur `internal/` | [dashboard/README.md](dashboard/README.md) |
| Koleksi API (Postman) | `step-ca.postman_collection.json` |
| Konsep PKI, X5C, chain | deskripsi folder Admin API di koleksi Postman |

## 8. Catatan produksi

- Sertifikat pendek + auto-renew > sertifikat umur panjang + revoke manual
- Root CA key harus offline; yang online hanya intermediate
- `data/step/` = seluruh isi kepercayaan PKI — backup & jaga aksesnya
- Belum ada HTTPS di depan dashboard Go; tambahkan reverse proxy + auth sebelum expose
- Key sertifikat tersimpan di `dashboard/server/issued/` untuk fitur Renew —
  untuk produksi, lebih baik device menyimpan key-nya sendiri dan renew mandiri
