package exoskeleton

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// generateTestCACertPEM creates a self-signed CA certificate in PEM format
// suitable for testing TLS CA cert loading.
func generateTestCACertPEM(t *testing.T) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

// writeTempFile writes data to a temp file in the given dir and returns its path.
func writeTempFile(t *testing.T, dir, pattern string, data []byte) string {
	t.Helper()
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		t.Fatalf("write temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	return f.Name()
}

// TestNewRustFSRegistrar_WithCACertPath verifies that NewRustFSRegistrar loads
// a valid CA cert PEM file and initializes with HTTPS successfully.
func TestNewRustFSRegistrar_WithCACertPath(t *testing.T) {
	caPEM := generateTestCACertPEM(t)
	caPath := writeTempFile(t, t.TempDir(), "ca-*.pem", caPEM)

	cfg := RustFSConfig{
		Endpoint:   "https://rustfs.example.svc.cluster.local:9000",
		AccessKey:  "admin",
		SecretKey:  "admin123",
		Bucket:     "tentacular",
		Region:     "us-east-1",
		CACertPath: caPath,
	}

	reg, err := NewRustFSRegistrar(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewRustFSRegistrar with valid CA cert should succeed, got: %v", err)
	}
	defer reg.Close()

	if reg.admin == nil {
		t.Error("admin client should be initialized")
	}
	if reg.s3 == nil {
		t.Error("s3 client should be initialized")
	}
	if !strings.HasPrefix(reg.admin.endpoint, "https://") {
		t.Errorf("admin endpoint = %q, expected https:// prefix", reg.admin.endpoint)
	}
	// Verify custom TLS transport was applied with RootCAs pool.
	tr, ok := reg.admin.httpClient.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil || tr.TLSClientConfig.RootCAs == nil {
		t.Error("expected custom TLS transport with RootCAs pool from CA cert")
	}
}

// TestNewRustFSRegistrar_WithCACertPEM verifies that NewRustFSRegistrar accepts
// PEM content directly via CACertPEM (env-var-based approach).
func TestNewRustFSRegistrar_WithCACertPEM(t *testing.T) {
	caPEM := generateTestCACertPEM(t)

	cfg := RustFSConfig{
		Endpoint:  "https://rustfs.example.svc.cluster.local:9000",
		AccessKey: "admin",
		SecretKey: "admin123",
		Bucket:    "tentacular",
		Region:    "us-east-1",
		CACertPEM: string(caPEM),
	}

	reg, err := NewRustFSRegistrar(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewRustFSRegistrar with CACertPEM should succeed, got: %v", err)
	}
	defer reg.Close()

	if reg.admin == nil {
		t.Error("admin client should be initialized")
	}
	if reg.s3 == nil {
		t.Error("s3 client should be initialized")
	}
	// Verify custom TLS transport was applied.
	tr, ok := reg.admin.httpClient.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil || tr.TLSClientConfig.RootCAs == nil {
		t.Error("expected custom TLS transport with RootCAs pool from CACertPEM")
	}
}

// TestNewRustFSRegistrar_CACertPEM_PrecedenceOverPath verifies that CACertPEM
// takes precedence over CACertPath when both are set.
func TestNewRustFSRegistrar_CACertPEM_PrecedenceOverPath(t *testing.T) {
	caPEM := generateTestCACertPEM(t)

	cfg := RustFSConfig{
		Endpoint:   "https://rustfs.example.svc.cluster.local:9000",
		AccessKey:  "admin",
		SecretKey:  "admin123",
		Bucket:     "tentacular",
		Region:     "us-east-1",
		CACertPEM:  string(caPEM),
		CACertPath: "/nonexistent/path/ca.pem", // Should be ignored.
	}

	reg, err := NewRustFSRegistrar(context.Background(), cfg)
	if err != nil {
		t.Fatalf("CACertPEM should take precedence, got: %v", err)
	}
	defer reg.Close()

	tr, ok := reg.admin.httpClient.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil || tr.TLSClientConfig.RootCAs == nil {
		t.Error("expected custom TLS transport from CACertPEM")
	}
}

// TestNewRustFSRegistrar_WithInvalidCACertPEM verifies that invalid PEM in
// CACertPEM produces an error.
func TestNewRustFSRegistrar_WithInvalidCACertPEM(t *testing.T) {
	cfg := RustFSConfig{
		Endpoint:  "https://rustfs.example.svc.cluster.local:9000",
		AccessKey: "admin",
		SecretKey: "admin123",
		Bucket:    "tentacular",
		Region:    "us-east-1",
		CACertPEM: "this is not a PEM certificate",
	}

	reg, err := NewRustFSRegistrar(context.Background(), cfg)
	if err == nil {
		reg.Close()
		t.Fatal("NewRustFSRegistrar should fail with invalid CACertPEM")
	}
	if !strings.Contains(err.Error(), "no valid PEM") {
		t.Errorf("error should mention invalid PEM, got: %v", err)
	}
}

// TestNewRustFSRegistrar_WithMissingCACert verifies that NewRustFSRegistrar
// returns an error when the CA cert file does not exist.
func TestNewRustFSRegistrar_WithMissingCACert(t *testing.T) {
	cfg := RustFSConfig{
		Endpoint:   "https://rustfs.example.svc.cluster.local:9000",
		AccessKey:  "admin",
		SecretKey:  "admin123",
		Bucket:     "tentacular",
		Region:     "us-east-1",
		CACertPath: filepath.Join(t.TempDir(), "nonexistent-ca.pem"),
	}

	reg, err := NewRustFSRegistrar(context.Background(), cfg)
	if err == nil {
		reg.Close()
		t.Fatal("NewRustFSRegistrar should fail with missing CA cert file")
	}
	if !strings.Contains(err.Error(), "rustfs read CA cert") {
		t.Errorf("error should mention reading CA cert, got: %v", err)
	}
}

// TestNewRustFSRegistrar_WithInvalidCACert verifies that NewRustFSRegistrar
// returns an error when the CA cert file contains invalid (non-PEM) data.
func TestNewRustFSRegistrar_WithInvalidCACert(t *testing.T) {
	badPath := writeTempFile(t, t.TempDir(), "bad-ca-*.pem", []byte("this is not a PEM certificate"))

	cfg := RustFSConfig{
		Endpoint:   "https://rustfs.example.svc.cluster.local:9000",
		AccessKey:  "admin",
		SecretKey:  "admin123",
		Bucket:     "tentacular",
		Region:     "us-east-1",
		CACertPath: badPath,
	}

	reg, err := NewRustFSRegistrar(context.Background(), cfg)
	if err == nil {
		reg.Close()
		t.Fatal("NewRustFSRegistrar should fail with invalid PEM data")
	}
	if !strings.Contains(err.Error(), "no valid PEM") {
		t.Errorf("error should mention invalid PEM, got: %v", err)
	}
}

// TestNewRustFSRegistrar_NoCACert_HTTP verifies existing behavior: empty
// CACertPath with an HTTP endpoint works without TLS configuration.
func TestNewRustFSRegistrar_NoCACert_HTTP(t *testing.T) {
	cfg := RustFSConfig{
		Endpoint:  "http://rustfs.example.svc.cluster.local:9000",
		AccessKey: "admin",
		SecretKey: "admin123",
		Bucket:    "tentacular",
		Region:    "us-east-1",
		// CACertPath and CACertPEM intentionally empty.
	}

	reg, err := NewRustFSRegistrar(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewRustFSRegistrar with HTTP and no CA cert should succeed, got: %v", err)
	}
	defer reg.Close()

	if reg.admin == nil {
		t.Error("admin client should be initialized")
	}
	if reg.s3 == nil {
		t.Error("s3 client should be initialized")
	}
	if !strings.HasPrefix(reg.admin.endpoint, "http://") {
		t.Errorf("admin endpoint = %q, expected http:// prefix", reg.admin.endpoint)
	}
	// Default HTTP client should be used (no custom transport).
	if reg.admin.httpClient != http.DefaultClient {
		t.Error("admin httpClient should be http.DefaultClient when no CA cert configured")
	}
}
