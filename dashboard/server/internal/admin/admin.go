package admin

// admin.go — service Admin API step-ca: kredensial X5C, sign token, dan
// operasi kelola provisioner (create/delete) via HTTP murni.
//
// ⚠️ QUIRK step-ca v0.30.2 (lihat smallstep/cli#860): middleware auth TIDAK
// strip prefix "Bearer " — header Authorization harus diisi token MURNI.
// Jika pakai "Bearer <token>", go-jose menghapus spasi lalu mendekode
// "BearereyJ..." sebagai base64 → "invalid character \x05".

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"dashboard-server/internal/ca"
	"dashboard-server/internal/keys"
	"dashboard-server/internal/types"
)

// Service — admin credentials + CA client for the Admin API.
type Service struct {
	ca              *ca.Client
	caURL           string
	provisionerName string
	admin           *types.AdminCreds
}

// New creates the Admin API service (credentials bootstrapped separately).
func New(caClient *ca.Client, caURL, provisionerName string) *Service {
	return &Service{ca: caClient, caURL: caURL, provisionerName: provisionerName}
}

// Bootstrap issues a certificate for the super admin subject using the given
// JWK provisioner. That certificate becomes the X5C Admin API credential.
func (s *Service) Bootstrap(subject string, prov types.ProvisionerKey) error {
	csrPEM, keyPEM, err := keys.GenerateCSR(subject, []string{subject})
	if err != nil {
		return err
	}
	ott, err := s.ca.SignOTT(prov, subject, "/1.0/sign", []string{subject})
	if err != nil {
		return err
	}
	sr, err := s.ca.Sign(csrPEM, ott, "")
	if err != nil {
		return fmt.Errorf("issue admin cert: %w", err)
	}

	key, err := parsePKCS8ECDSA(keyPEM)
	if err != nil {
		return err
	}
	leafBlk, _ := pem.Decode([]byte(sr.CRT))
	if leafBlk == nil {
		return errors.New("empty admin certificate")
	}
	leaf, err := x509.ParseCertificate(leafBlk.Bytes)
	if err != nil {
		return err
	}

	// x5c: leaf + intermediate (base64 std DER)
	x5c := []string{base64.StdEncoding.EncodeToString(leafBlk.Bytes)}
	for _, pemStr := range append([]string{sr.CA}, sr.CertChain...) {
		if blk, _ := pem.Decode([]byte(pemStr)); blk != nil {
			if c, err := x509.ParseCertificate(blk.Bytes); err == nil && c.IsCA {
				x5c = append(x5c, base64.StdEncoding.EncodeToString(blk.Bytes))
				break
			}
		}
	}

	s.admin = &types.AdminCreds{Subject: subject, Leaf: leaf, X5C: x5c, Key: key}
	return nil
}

// Ready melaporkan apakah kredensial admin sudah ter-bootstrap.
func (s *Service) Ready() bool { return s.admin != nil }

// Token mints an X5C JWT for an admin path:
// header {alg, kid(thumbprint), typ, x5c}, aud = caURL+path,
// iss = step-admin-client/1.0, sub = super admin subject.
func (s *Service) Token(path string) (string, error) {
	if s.admin == nil {
		return "", errors.New("admin credential not available")
	}
	now := time.Now()

	pub, ok := s.admin.Leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return "", errors.New("admin key is not ECDSA")
	}
	xb, yb := make([]byte, 32), make([]byte, 32)
	pub.X.FillBytes(xb)
	pub.Y.FillBytes(yb)

	header := map[string]any{
		"alg": "ES256",
		"kid": keys.Thumbprint(keys.B64URL(xb), keys.B64URL(yb)),
		"typ": "JWT",
		"x5c": s.admin.X5C,
	}
	jti := make([]byte, 32)
	if _, err := rand.Read(jti); err != nil {
		return "", err
	}
	claims := map[string]any{
		"aud": s.caURL + path,
		"iss": "step-admin-client/1.0",
		"sub": s.admin.Subject,
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
		"jti": hex.EncodeToString(jti),
	}
	hb, _ := json.Marshal(header)
	pb, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := keys.B64URL(hb) + "." + keys.B64URL(pb)
	digest := sha256.Sum256([]byte(signingInput))
	r, sv, err := ecdsa.Sign(rand.Reader, s.admin.Key, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	sv.FillBytes(sig[32:])
	return signingInput + "." + keys.B64URL(sig), nil
}

// JSON — request Admin API dengan token X5C (TANPA prefix "Bearer "!).
func (s *Service) JSON(method, path string, body any) (*http.Response, error) {
	tok, err := s.Token(path)
	if err != nil {
		return nil, err
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, s.caURL+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", tok) // ⚠️ TANPA "Bearer "
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return s.ca.HTTP().Do(req)
}

// CreateProvisioner creates a new JWK provisioner via the Admin API.
// Returns (kid, private JWK JSON) — the caller persists the private key.
func (s *Service) CreateProvisioner(name, defaultDur, maxDur string) (kid string, privateJWK []byte, err error) {
	key, kid, _, _, err := keys.GenerateKeypair()
	if err != nil {
		return "", nil, err
	}
	pubJSON, privJSON, err := keys.MarshalPrivateJWK(key, kid)
	if err != nil {
		return "", nil, err
	}

	body := map[string]any{
		"name": name,
		"type": "JWK",
		"details": map[string]any{
			"JWK": map[string]any{
				"publicKey": base64.StdEncoding.EncodeToString(pubJSON),
			},
		},
		"claims": map[string]any{
			"x509": map[string]any{
				"enabled":   true,
				"durations": map[string]string{"default": defaultDur, "max": maxDur},
			},
		},
	}
	resp, err := s.JSON("POST", "/admin/provisioners", body)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", nil, fmt.Errorf("CA %d: %s", resp.StatusCode, decodeErr(resp))
	}
	return kid, privJSON, nil
}

// DeleteProvisioner removes a provisioner via the Admin API.
func (s *Service) DeleteProvisioner(name string) error {
	resp, err := s.JSON("DELETE", "/admin/provisioners/"+name, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("CA %d: %s", resp.StatusCode, decodeErr(resp))
	}
	return nil
}

// ---- helper ----

func parsePKCS8ECDSA(keyPEM string) (*ecdsa.PrivateKey, error) {
	blk, _ := pem.Decode([]byte(keyPEM))
	if blk == nil {
		return nil, errors.New("empty key PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("key is not ECDSA")
	}
	return ec, nil
}

func decodeErr(resp *http.Response) string {
	var e struct {
		Message string `json:"message"`
	}
	json.NewDecoder(resp.Body).Decode(&e)
	return e.Message
}

// NormalizeDuration converts "Nd" (days) → hours; validates Go duration format.
func NormalizeDuration(s string) (string, bool) {
	if s == "" {
		return "", true
	}
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || days <= 0 {
			return "", false
		}
		s = fmt.Sprintf("%dh", days*24)
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return "", false
	}
	return s, true
}

var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// ValidName checks a provisioner name.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// SavePrivateJWK writes a private JWK file with 0600 permissions.
func SavePrivateJWK(path string, privJSON []byte) error {
	return os.WriteFile(path, privJSON, 0600)
}
