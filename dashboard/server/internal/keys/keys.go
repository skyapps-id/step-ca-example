package keys

// keys.go — key utilities: JWK, CSR, keypairs, thumbprint, base64url.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"

	"dashboard-server/internal/types"
)

// B64URL — unpadded URL-safe base64 (JOSE convention).
func B64URL(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// DecodeJWK reads an EC P-256 private JWK file.
func DecodeJWK(path string) (types.ProvisionerKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return types.ProvisionerKey{}, err
	}
	var k types.JWK
	if err := json.Unmarshal(raw, &k); err != nil {
		return types.ProvisionerKey{}, fmt.Errorf("parse JWK %s: %w", path, err)
	}
	if k.Kty != "EC" || k.Crv != "P-256" || k.D == "" {
		return types.ProvisionerKey{}, fmt.Errorf("%s: JWK bukan EC P-256 private key", path)
	}
	dec := base64.RawURLEncoding.DecodeString
	xb, err := dec(k.X)
	yb, err2 := dec(k.Y)
	db, err3 := dec(k.D)
	if err != nil || err2 != nil || err3 != nil {
		return types.ProvisionerKey{}, fmt.Errorf("%s: koordinat JWK tidak valid", path)
	}
	key := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: elliptic.P256()},
		D:         new(big.Int).SetBytes(db),
	}
	key.X = new(big.Int).SetBytes(xb)
	key.Y = new(big.Int).SetBytes(yb)
	return types.ProvisionerKey{Name: k.Kid, Key: key, Kid: k.Kid}, nil
}

// GenerateCSR creates a fresh EC P-256 keypair + CSR for the given subject/SANs.
func GenerateCSR(subject string, sans []string) (csrPEM, keyPEM string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	tmpl := &x509.CertificateRequest{
		Subject:            pkix.Name{CommonName: subject},
		DNSNames:           sans,
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	csrPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	return csrPEM, keyPEM, nil
}

// Thumbprint computes the kid (RFC 7638) of an EC P-256 key.
func Thumbprint(x, y string) string {
	canonical := fmt.Sprintf(`{"crv":"P-256","kty":"EC","x":%q,"y":%q}`, x, y)
	sum := sha256.Sum256([]byte(canonical))
	return B64URL(sum[:])
}

// MarshalPrivateJWK builds public and private JWK JSON from a keypair + kid.
func MarshalPrivateJWK(key *ecdsa.PrivateKey, kid string) (publicJSON, privateJSON []byte, err error) {
	xb, yb := make([]byte, 32), make([]byte, 32)
	key.X.FillBytes(xb)
	key.Y.FillBytes(yb)
	xs, ys := B64URL(xb), B64URL(yb)

	public := map[string]string{
		"use": "sig", "kty": "EC", "kid": kid,
		"crv": "P-256", "alg": "ES256", "x": xs, "y": ys,
	}
	private := map[string]string{
		"use": "sig", "kty": "EC", "kid": kid,
		"crv": "P-256", "alg": "ES256", "x": xs, "y": ys, "d": B64URL(key.D.Bytes()),
	}
	pubJSON, err := json.Marshal(public)
	if err != nil {
		return nil, nil, err
	}
	privJSON, err := json.Marshal(private)
	if err != nil {
		return nil, nil, err
	}
	return pubJSON, privJSON, nil
}

// GenerateKeypair creates a fresh EC P-256 keypair and its kid.
func GenerateKeypair() (key *ecdsa.PrivateKey, kid, xs, ys string, err error) {
	key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", "", "", err
	}
	xb, yb := make([]byte, 32), make([]byte, 32)
	key.X.FillBytes(xb)
	key.Y.FillBytes(yb)
	xs, ys = B64URL(xb), B64URL(yb)
	return key, Thumbprint(xs, ys), xs, ys, nil
}
