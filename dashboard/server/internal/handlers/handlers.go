package handlers

// handlers.go — dashboard HTTP handlers + their dependencies (dependency injection).

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dashboard-server/internal/admin"
	"dashboard-server/internal/ca"
	"dashboard-server/internal/config"
	"dashboard-server/internal/keys"
	"dashboard-server/internal/store"
	"dashboard-server/internal/types"
)

// Handlers — dependencies + state shared by all handlers.
type Handlers struct {
	Cfg   config.Config
	Provs map[string]types.ProvisionerKey
	Store *store.Store
	CA    *ca.Client
	Admin *admin.Service

	mu     sync.Mutex
	issued []types.IssuedCert
}

// New builds Handlers + loads the issuance history from the store.
func New(cfg config.Config, provs map[string]types.ProvisionerKey, caClient *ca.Client, st *store.Store, adminSvc *admin.Service) *Handlers {
	h := &Handlers{Cfg: cfg, Provs: provs, CA: caClient, Store: st, Admin: adminSvc}
	h.issued = st.LoadAll()
	log.Printf("riwayat issue dimuat: %d sertifikat", len(h.issued))
	return h
}

// Routes mounts all dashboard endpoints.
func (h *Handlers) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", h.Health)
	mux.HandleFunc("GET /api/ca/roots", h.Roots)
	mux.HandleFunc("POST /api/certificates", h.Issue)
	mux.HandleFunc("GET /api/certificates", h.List)
	mux.HandleFunc("POST /api/certificates/{serial}/renew", h.Renew)
	mux.HandleFunc("GET /api/certificates/{serial}/download/{kind}", h.Download)
	mux.HandleFunc("GET /api/admin/provisioners", h.ProvisionerList)
	mux.HandleFunc("POST /api/admin/provisioners", h.ProvisionerCreate)
	mux.HandleFunc("DELETE /api/admin/provisioners/{name}", h.ProvisionerDelete)
	return mux
}

// ---- response helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Health — CA status (proxies /health + /version).
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	var hs struct {
		Status string `json:"status"`
	}
	if err := h.CA.Get("/health", &hs); err != nil {
		writeJSON(w, 200, map[string]string{"ca": "down", "detail": err.Error()})
		return
	}
	var v struct {
		Version string `json:"version"`
	}
	h.CA.Get("/version", &v)
	writeJSON(w, 200, map[string]string{"ca": hs.Status, "version": v.Version, "url": h.Cfg.CAURL})
}

// Roots — root CA PEM (proxies /roots).
func (h *Handlers) Roots(w http.ResponseWriter, r *http.Request) {
	var roots struct {
		Crts []string `json:"crts"`
	}
	if err := h.CA.Get("/roots", &roots); err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, roots)
}

// Issue — menerbitkan sertifikat baru (CSR + OTT + POST /sign).
func (h *Handlers) Issue(w http.ResponseWriter, r *http.Request) {
	var req types.IssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON body")
		return
	}
	req.Subject = strings.TrimSpace(req.Subject)
	if req.Subject == "" {
		writeErr(w, 400, "subject field is required")
		return
	}

	sans := []string{req.Subject}
	for _, s := range req.SANs {
		if s = strings.TrimSpace(s); s != "" && s != req.Subject {
			sans = append(sans, s)
		}
	}

	notAfter := ""
	if d := strings.TrimSpace(req.Duration); d != "" {
		na, ok := admin.NormalizeDuration(d)
		if !ok {
			writeErr(w, 400, "invalid duration (e.g. 24h, 7d)")
			return
		}
		notAfter = na
	}

	provName := strings.TrimSpace(req.Provisioner)
	if provName == "" {
		provName = h.Cfg.ProvisionerName
	}
	prov, ok := h.Provs[provName]
	if !ok {
		writeErr(w, 400, "provisioner '"+provName+"' not available")
		return
	}

	csrPEM, keyPEM, err := keys.GenerateCSR(req.Subject, sans)
	if err != nil {
		writeErr(w, 500, "failed to generate key/CSR: "+err.Error())
		return
	}
	ott, err := h.CA.SignOTT(prov, req.Subject, "/1.0/sign", sans)
	if err != nil {
		writeErr(w, 500, "failed to sign OTT: "+err.Error())
		return
	}
	sr, err := h.CA.Sign(csrPEM, ott, notAfter)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}

	rec := types.IssuedCert{
		Subject: req.Subject, SANs: sans,
		IssuedAt: time.Now().UTC(), Provisioner: provName,
	}
	if blk, _ := pem.Decode([]byte(sr.CRT)); blk != nil {
		if crt, err := x509.ParseCertificate(blk.Bytes); err == nil {
			rec.Serial = crt.SerialNumber.Text(16)
			rec.NotAfter = crt.NotAfter
			rec.Issuer = crt.Issuer.CommonName
		}
	}

	h.mu.Lock()
	h.issued = append(h.issued, rec)
	h.mu.Unlock()
	h.Store.Save(rec, sr.CRT+"\n"+sr.CA, keyPEM)

	log.Printf("cert terbit: %s (serial %s)", rec.Subject, rec.Serial)
	writeJSON(w, 201, map[string]any{
		"subject": rec.Subject, "sans": rec.SANs,
		"serial": rec.Serial, "notAfter": rec.NotAfter,
		"provisioner": provName,
		"crt":         sr.CRT, "ca": sr.CA, "certChain": sr.CertChain, "key": keyPEM,
	})
}

// List — certificates issued by this dashboard.
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	list := make([]types.IssuedCert, len(h.issued))
	copy(list, h.issued)
	h.mu.Unlock()
	writeJSON(w, 200, list)
}

// Renew — perbarui sertifikat memakai cert+key lamanya (mTLS /renew).
func (h *Handlers) Renew(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")

	h.mu.Lock()
	var rec *types.IssuedCert
	for i := range h.issued {
		if h.issued[i].Serial == serial {
			rec = &h.issued[i]
			break
		}
	}
	h.mu.Unlock()
	if rec == nil {
		writeErr(w, 404, "serial not found in this dashboard's storage")
		return
	}

	crtPEM, keyPEM, err := h.Store.ReadFiles(serial)
	if err != nil {
		writeErr(w, 404, "old cert/key files not found on disk")
		return
	}
	tlsCert, err := tls.X509KeyPair(crtPEM, keyPEM)
	if err != nil {
		writeErr(w, 500, "old cert/key invalid: "+err.Error())
		return
	}

	sr, err := h.CA.RenewWithCert(tlsCert)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}

	newSerial, newNotAfter := rec.Serial, rec.NotAfter
	if blk, _ := pem.Decode([]byte(sr.CRT)); blk != nil {
		if c, err := x509.ParseCertificate(blk.Bytes); err == nil {
			newSerial = c.SerialNumber.Text(16)
			newNotAfter = c.NotAfter
		}
	}

	h.mu.Lock()
	rec.Serial, rec.NotAfter, rec.IssuedAt = newSerial, newNotAfter, time.Now().UTC()
	h.mu.Unlock()
	fullchain := sr.CRT + "\n" + sr.CA
	if err := h.Store.UpdateAfterRenew(serial, *rec, fullchain); err != nil {
		writeErr(w, 500, "renew succeeded but failed to persist: "+err.Error())
		return
	}

	log.Printf("cert renewed: %s → serial baru %s", rec.Subject, newSerial)
	writeJSON(w, 200, map[string]any{
		"subject": rec.Subject, "serial": newSerial, "notAfter": newNotAfter,
		"crt": sr.CRT, "ca": sr.CA, "certChain": sr.CertChain,
	})
}

// Download — mengirim file cert/key tersimpan by serial sebagai attachment.
func (h *Handlers) Download(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")
	kind := r.PathValue("kind")
	if kind != "crt" && kind != "key" {
		writeErr(w, 400, "kind must be crt or key")
		return
	}
	data, err := os.ReadFile(filepath.Join(h.Store.Dir(), serial+"."+kind))
	if err != nil {
		writeErr(w, 404, "file not found for serial "+serial)
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", `attachment; filename="`+serial+"."+kind+`"`)
	w.Write(data)
}

// ProvisionerList — provisioner list (public CA endpoint) + local key flag.
func (h *Handlers) ProvisionerList(w http.ResponseWriter, r *http.Request) {
	var pub struct {
		Provisioners []types.ProvisionerView `json:"provisioners"`
	}
	if err := h.CA.Get("/provisioners", &pub); err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	for i := range pub.Provisioners {
		_, pub.Provisioners[i].IsLocal = h.Provs[pub.Provisioners[i].Name]
	}
	writeJSON(w, 200, pub.Provisioners)
}

// ProvisionerCreate — creates a new JWK provisioner via the Admin API.
// The private key is stored locally so it can be used for issuing right away.
func (h *Handlers) ProvisionerCreate(w http.ResponseWriter, r *http.Request) {
	var req types.AddProvisionerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !admin.ValidName(req.Name) {
		writeErr(w, 400, "provisioner name may only contain alphanumerics, '-', '_'")
		return
	}
	if _, exists := h.Provs[req.Name]; exists {
		writeErr(w, 400, "provisioner '"+req.Name+"' already exists")
		return
	}
	defDur, ok1 := admin.NormalizeDuration(req.DefaultDuration)
	maxDur, ok2 := admin.NormalizeDuration(req.MaxDuration)
	if !ok1 || !ok2 {
		writeErr(w, 400, "invalid duration (e.g. 24h, 7d)")
		return
	}

	kid, privJSON, err := h.Admin.CreateProvisioner(req.Name, defDur, maxDur)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}

	fileName := fmt.Sprintf("provisioner-%s.jwk.json", req.Name)
	if err := admin.SavePrivateJWK(fileName, privJSON); err != nil {
		writeErr(w, 500, "provisioner tercatat di CA, tapi gagal simpan kunci lokal: "+err.Error())
		return
	}
	if pk, err := keys.DecodeJWK(fileName); err == nil {
		pk.Name = req.Name
		h.Provs[req.Name] = pk
	}

	log.Printf("provisioner baru dibuat via Admin API: %s (kid %s...)", req.Name, kid[:12])
	writeJSON(w, 201, map[string]any{
		"name": req.Name, "kid": kid,
		"defaultDuration": defDur, "maxDuration": maxDur,
		"note": "private key provisioner disimpan di " + fileName,
	})
}

// ProvisionerDelete — removes a provisioner via the Admin API + local key file.
func (h *Handlers) ProvisionerDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == h.Cfg.ProvisionerName {
		writeErr(w, 400, "tidak boleh hapus provisioner utama '"+name+"'")
		return
	}
	if err := h.Admin.DeleteProvisioner(name); err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	delete(h.Provs, name)
	os.Remove(fmt.Sprintf("provisioner-%s.jwk.json", name))
	log.Printf("provisioner dihapus via Admin API: %s", name)
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
