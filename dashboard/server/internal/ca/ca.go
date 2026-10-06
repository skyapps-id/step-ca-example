package ca

// ca.go — HTTP client for step-ca: root-pinned TLS, OTT signing, POST /sign, /renew.

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"dashboard-server/internal/types"
)

// Client — HTTPS client trusting the root CA + sign/renew operations.
type Client struct {
	caURL string
	http  *http.Client
}

func readFile(path string) ([]byte, error) { return os.ReadFile(path) }

// NewClient creates a client trusting the root CA at rootPath.
// ForceAttemptHTTP2 is required on a custom Transport, otherwise Go falls back to HTTP/1.1.
func NewClient(caURL, rootPath string) (*Client, error) {
	rootPEM, err := readFile(rootPath)
	if err != nil {
		return nil, fmt.Errorf("baca root CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(rootPEM) {
		return nil, errors.New("invalid root CA")
	}
	return &Client{
		caURL: caURL,
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig:   &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
				ForceAttemptHTTP2: true,
			},
		},
	}, nil
}

// HTTP exposes the underlying *http.Client (for requests with custom headers,
// e.g. Admin API calls carrying an X5C token).
func (c *Client) HTTP() *http.Client { return c.http }

// Get — simple GET + JSON decode.
func (c *Client) Get(path string, out any) error {
	resp, err := c.http.Get(c.caURL + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// PostJSON — POST a JSON body; caller checks status and decodes errors.
func (c *Client) PostJSON(path string, body any) (*http.Response, error) {
	return c.do("POST", path, body)
}

func (c *Client) do(method, path string, body any) (*http.Response, error) {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, c.caURL+path, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// decodeErr extracts the "message" field from a step-ca error body.
func decodeErr(resp *http.Response) string {
	var e struct {
		Message string `json:"message"`
	}
	json.NewDecoder(resp.Body).Decode(&e)
	return e.Message
}

// SignOTT mints a one-time token (JWT ES256) identical to `step ca token`.
func (c *Client) SignOTT(prov types.ProvisionerKey, subject, audPath string, sans []string) (string, error) {
	now := time.Now()
	jti := make([]byte, 32)
	if _, err := rand.Read(jti); err != nil {
		return "", err
	}

	claims := map[string]any{
		"aud":  c.caURL + audPath,
		"iss":  prov.Name,
		"sub":  subject,
		"sans": sans,
		"nbf":  now.Add(-time.Minute).Unix(),
		"exp":  now.Add(5 * time.Minute).Unix(),
		"jti":  hex.EncodeToString(jti),
	}
	header := fmt.Sprintf(`{"alg":"ES256","kid":%q,"typ":"JWT"}`, prov.Kid)

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := b64url([]byte(header)) + "." + b64url(payload)

	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, prov.Key, digest[:])
	if err != nil {
		return "", err
	}
	// JWS ES256 uses raw R||S (64 bytes), NOT DER encoding
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signingInput + "." + b64url(sig), nil
}

// Sign — POST /sign with CSR + OTT.
func (c *Client) Sign(csrPEM, ott, notAfter string) (types.SignResponse, error) {
	body := map[string]string{"csr": csrPEM, "ott": ott}
	if notAfter != "" {
		body["notAfter"] = notAfter
	}
	resp, err := c.PostJSON("/sign", body)
	if err != nil {
		return types.SignResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return types.SignResponse{}, fmt.Errorf("step-ca /sign %d: %s", resp.StatusCode, decodeErr(resp))
	}
	var sr types.SignResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return types.SignResponse{}, err
	}
	return sr, nil
}

// RenewWithCert — POST /renew using the old cert+key as mTLS credentials.
func (c *Client) RenewWithCert(cert tls.Certificate) (types.SignResponse, error) {
	tr := c.http.Transport.(*http.Transport).Clone()
	tr.TLSClientConfig.Certificates = []tls.Certificate{cert}
	mtls := &http.Client{Timeout: 15 * time.Second, Transport: tr}

	resp, err := mtls.Post(c.caURL+"/renew", "application/json", nil)
	if err != nil {
		return types.SignResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return types.SignResponse{}, fmt.Errorf("step-ca /renew %d: %s", resp.StatusCode, decodeErr(resp))
	}
	var sr types.SignResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return types.SignResponse{}, err
	}
	return sr, nil
}

// ---- base64 helpers (JOSE convention) ----

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
