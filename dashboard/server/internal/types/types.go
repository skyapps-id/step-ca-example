package types

// types.go — shared structs used across packages (no logic).

import (
	"crypto/ecdsa"
	"crypto/x509"
	"time"
)

// JWK — private JWK provisioner (EC P-256) yang dipegang dashboard.
type JWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Kid string `json:"kid"`
	X   string `json:"x"`
	Y   string `json:"y"`
	D   string `json:"d"`
}

// ProvisionerKey — provisioner JWK yang bisa dipakai issue (kunci privat + kid).
type ProvisionerKey struct {
	Name string
	Key  *ecdsa.PrivateKey
	Kid  string
}

// IssuedCert — catatan sertifikat yang pernah diterbitkan dashboard.
type IssuedCert struct {
	Subject     string    `json:"subject"`
	SANs        []string  `json:"sans"`
	Serial      string    `json:"serial"`
	NotAfter    time.Time `json:"notAfter"`
	IssuedAt    time.Time `json:"issuedAt"`
	Issuer      string    `json:"issuer"`
	Provisioner string    `json:"provisioner"`
}

// AdminCreds — kredensial X5C Admin API: cert super admin + private key-nya.
type AdminCreds struct {
	Subject string
	Leaf    *x509.Certificate
	X5C     []string // base64 std DER: leaf + intermediate
	Key     *ecdsa.PrivateKey
}

// SignResponse — balasan POST /sign dan /renew.
type SignResponse struct {
	CRT       string   `json:"crt"`
	CA        string   `json:"ca"`
	CertChain []string `json:"certChain"`
}

// ---- DTO request/response ----

type IssueRequest struct {
	Subject     string   `json:"subject"`
	SANs        []string `json:"sans"`
	Duration    string   `json:"duration"`
	Provisioner string   `json:"provisioner"`
}

type AddProvisionerRequest struct {
	Name            string `json:"name"`
	DefaultDuration string `json:"defaultDuration"`
	MaxDuration     string `json:"maxDuration"`
}

type ProvisionerView struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Kid     string `json:"kid,omitempty"`
	Claims  any    `json:"claims,omitempty"`
	IsLocal bool   `json:"isLocal"`
}
