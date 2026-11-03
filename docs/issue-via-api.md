# Issue Sertifikat via HTTP API

Alternatif tanpa `step ca certificate`: penerbitan sertifikat murni lewat HTTP API step-ca (`POST /sign`).

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
