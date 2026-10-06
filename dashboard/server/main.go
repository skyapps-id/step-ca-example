package main

// main.go — dashboard entry point: wire dependencies, mount routes, serve.
//
// Logic lives under internal/:
//   internal/config     runtime configuration
//   internal/types      shared structs
//   internal/keys       key/CSR/thumbprint utilities
//   internal/ca         step-ca client (sign OTT, /sign, /renew)
//   internal/store      issued certificate persistence
//   internal/admin      Admin API service (X5C)
//   internal/handlers   HTTP handlers

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"dashboard-server/internal/admin"
	"dashboard-server/internal/ca"
	"dashboard-server/internal/config"
	"dashboard-server/internal/handlers"
	"dashboard-server/internal/keys"
	"dashboard-server/internal/store"
	"dashboard-server/internal/types"
)

func main() {
	cfg := config.Load()

	caClient, err := ca.NewClient(cfg.CAURL, cfg.CARoot)
	if err != nil {
		log.Fatal(err)
	}
	st := store.New(cfg.IssuedDir)
	adminSvc := admin.New(caClient, cfg.CAURL, cfg.ProvisionerName)

	provs, err := loadProvisioners(cfg)
	if err != nil {
		log.Fatal(err)
	}

	h := handlers.New(cfg, provs, caClient, st, adminSvc)

	// bootstrap kredensial admin: issue cert untuk subject super admin
	if prov, ok := provs[cfg.ProvisionerName]; ok {
		if err := adminSvc.Bootstrap(cfg.AdminSubject, prov); err != nil {
			log.Printf("WARNING: admin credential gagal (fitur kelola provisioner nonaktif): %v", err)
		} else {
			log.Printf("admin credential siap: subject %q", cfg.AdminSubject)
		}
	}

	log.Printf("dashboard API listening on %s (CA: %s)", cfg.Listen, cfg.CAURL)
	if err := http.ListenAndServe(cfg.Listen, h.Routes()); err != nil {
		log.Fatal(err)
	}
}

// loadProvisioners loads the main provisioner plus extras (iot, etc).
func loadProvisioners(cfg config.Config) (map[string]types.ProvisionerKey, error) {
	provs := map[string]types.ProvisionerKey{}

	adm, err := keys.DecodeJWK(cfg.ProvisionerJWK)
	if err != nil {
		return nil, err
	}
	adm.Name = cfg.ProvisionerName
	provs[adm.Name] = adm
	log.Printf("provisioner %s loaded (kid %s...)", adm.Name, adm.Kid[:12])

	// muat semua provisioner tambahan dari file provisioner-*.jwk.json
	// (nama provisioner diambil dari nama file)
	extra, _ := filepath.Glob("provisioner-*.jwk.json")
	jwkDir, _ := filepath.Glob(filepath.Join("jwk", "provisioner-*.jwk.json"))
	extra = append(extra, jwkDir...)
	for _, path := range extra {
		base := filepath.Base(path)
		name := strings.TrimSuffix(strings.TrimPrefix(base, "provisioner-"), ".jwk.json")
		if name == "" {
			continue
		}
		if _, exists := provs[name]; exists {
			continue
		}
		if p, err := keys.DecodeJWK(path); err == nil {
			p.Name = name
			provs[p.Name] = p
			log.Printf("provisioner %s loaded (kid %s...)", name, p.Kid[:12])
		} else {
			log.Printf("skip %s: %v", base, err)
		}
	}

	// opsional: satu file tambahan via env (path bebas)
	if p := os.Getenv("PROVISIONER_IOT_JWK"); p != "" {
		if pk, err := keys.DecodeJWK(p); err == nil {
			pk.Name = "iot"
			provs[pk.Name] = pk
			log.Printf("provisioner iot loaded (kid %s...)", pk.Kid[:12])
		}
	}
	return provs, nil
}
