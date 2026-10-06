package store

// store.go — issued certificate persistence: cert (fullchain) + key + metadata
// per serial. The old cert serves as mTLS credentials for Renew.

import (
	"encoding/json"
	"os"
	"path/filepath"

	"dashboard-server/internal/types"
)

// Store — storage folder for issued certificates.
type Store struct {
	dir string
}

// New creates a Store in the given directory.
func New(dir string) *Store { return &Store{dir: dir} }

// Dir returns the storage folder location.
func (s *Store) Dir() string { return s.dir }

// Save stores the fullchain, key, and certificate metadata.
func (s *Store) Save(rec types.IssuedCert, fullchainPEM, keyPEM string) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	base := filepath.Join(s.dir, rec.Serial)
	if err := os.WriteFile(base+".crt", []byte(fullchainPEM), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(base+".key", []byte(keyPEM), 0600); err != nil {
		return err
	}
	meta, _ := json.Marshal(rec)
	return os.WriteFile(base+".json", meta, 0600)
}

// LoadAll reads all stored certificate metadata.
func (s *Store) LoadAll() []types.IssuedCert {
	var out []types.IssuedCert
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var rec types.IssuedCert
		if json.Unmarshal(b, &rec) == nil && rec.Serial != "" {
			out = append(out, rec)
		}
	}
	return out
}

// ReadFiles reads the stored cert & key by serial.
func (s *Store) ReadFiles(serial string) (crtPEM, keyPEM []byte, err error) {
	crtPEM, err = os.ReadFile(filepath.Join(s.dir, serial+".crt"))
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err = os.ReadFile(filepath.Join(s.dir, serial+".key"))
	if err != nil {
		return nil, nil, err
	}
	return crtPEM, keyPEM, nil
}

// UpdateAfterRenew stores the renewed cert and cleans up old serial files.
// The private key is unchanged — just renamed to follow the new serial.
func (s *Store) UpdateAfterRenew(oldSerial string, rec types.IssuedCert, fullchainPEM string) error {
	base := filepath.Join(s.dir, rec.Serial)
	if err := os.WriteFile(base+".crt", []byte(fullchainPEM), 0600); err != nil {
		return err
	}
	meta, _ := json.Marshal(rec)
	if err := os.WriteFile(base+".json", meta, 0600); err != nil {
		return err
	}
	if rec.Serial != oldSerial {
		os.Remove(filepath.Join(s.dir, oldSerial+".crt"))
		os.Remove(filepath.Join(s.dir, oldSerial+".json"))
		os.Rename(filepath.Join(s.dir, oldSerial+".key"), base+".key")
	}
	return nil
}
