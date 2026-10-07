package config

import "os"

// Config — dashboard runtime configuration (from environment variables).

type Config struct {
	Listen          string
	CAURL           string
	CARoot          string
	ProvisionerName string
	ProvisionerJWK  string
	IssuedDir       string
	AdminSubject    string // subject super admin di DB CA (default "step")
}

// Load reads configuration from the environment with defaults.
func Load() Config {
	return Config{
		Listen:          env("LISTEN", ":8080"),
		CAURL:           env("CA_URL", "https://localhost:9000"),
		CARoot:          env("CA_ROOT", "../../data/step/certs/root_ca.crt"),
		ProvisionerName: env("PROVISIONER_NAME", "admin"),
		ProvisionerJWK:  env("PROVISIONER_JWK", "jwk/provisioner.jwk.json"),
		IssuedDir:       env("ISSUED_DIR", "issued"),
		AdminSubject:    env("ADMIN_SUBJECT", "step"),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
