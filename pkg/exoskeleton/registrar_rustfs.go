package exoskeleton

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/signer"
)

// rustfsAdmin is a thin HTTP client for RustFS's admin API.
// RustFS uses /rustfs/admin/v3/ instead of MinIO's /minio/admin/v3/.
// Auth is AWS SigV4 with service "s3".
type rustfsAdmin struct {
	httpClient *http.Client
	endpoint   string
	accessKey  string
	secretKey  string
	region     string
}

// newRustFSAdmin creates a new RustFS admin HTTP client.
func newRustFSAdmin(endpoint, accessKey, secretKey, region string, httpClient *http.Client) *rustfsAdmin {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &rustfsAdmin{
		endpoint:   strings.TrimRight(endpoint, "/"),
		accessKey:  accessKey,
		secretKey:  secretKey,
		region:     region,
		httpClient: httpClient,
	}
}

// adminURL builds the full URL for an admin API path with optional query params.
func (a *rustfsAdmin) adminURL(path string, query url.Values) string {
	u := a.endpoint + "/rustfs/admin/v3" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// do executes a signed admin API request.
func (a *rustfsAdmin) do(ctx context.Context, method, path string, query url.Values, body []byte) (*http.Response, error) {
	fullURL := a.adminURL(path, query)

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	// Set content-sha256 header (required for SigV4).
	h := sha256.Sum256(body)
	req.Header.Set("X-Amz-Content-Sha256", hex.EncodeToString(h[:]))

	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	// Sign with SigV4 (service "s3", matching RustFS expectations).
	signed := signer.SignV4(*req, a.accessKey, a.secretKey, "", a.region)

	resp, err := a.httpClient.Do(signed)
	if err != nil {
		return nil, fmt.Errorf("admin request %s %s: %w", method, path, err)
	}
	return resp, nil
}

// doNoBody executes a signed request and checks for a successful status code,
// discarding the response body.
func (a *rustfsAdmin) doNoBody(ctx context.Context, method, path string, query url.Values, body []byte) error {
	resp, err := a.do(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("admin %s %s: HTTP %d: %s", method, path, resp.StatusCode, string(respBody))
	}
	// Drain the body so the underlying TCP connection can be reused.
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// AddUser creates or updates an IAM user.
func (a *rustfsAdmin) AddUser(ctx context.Context, accessKey, secretKey string) error {
	q := url.Values{"accessKey": {accessKey}}
	body, err := json.Marshal(struct {
		SecretKey string `json:"secretKey"`
		Status    string `json:"status"`
	}{
		SecretKey: secretKey,
		Status:    "enabled",
	})
	if err != nil {
		return fmt.Errorf("marshal add-user body: %w", err)
	}
	return a.doNoBody(ctx, http.MethodPut, "/add-user", q, body)
}

// RemoveUser deletes an IAM user.
func (a *rustfsAdmin) RemoveUser(ctx context.Context, accessKey string) error {
	q := url.Values{"accessKey": {accessKey}}
	return a.doNoBody(ctx, http.MethodDelete, "/remove-user", q, nil)
}

// AddCannedPolicy creates or replaces a canned IAM policy.
func (a *rustfsAdmin) AddCannedPolicy(ctx context.Context, name string, policy []byte) error {
	q := url.Values{"name": {name}}
	return a.doNoBody(ctx, http.MethodPut, "/add-canned-policy", q, policy)
}

// SetPolicy attaches a named policy to a user.
func (a *rustfsAdmin) SetPolicy(ctx context.Context, policyName, userName string) error {
	q := url.Values{
		"policyName":  {policyName},
		"userOrGroup": {userName},
		"isGroup":     {"false"},
	}
	return a.doNoBody(ctx, http.MethodPut, "/set-user-or-group-policy", q, nil)
}

// RemoveCannedPolicy deletes a canned IAM policy.
func (a *rustfsAdmin) RemoveCannedPolicy(ctx context.Context, name string) error {
	q := url.Values{"name": {name}}
	return a.doNoBody(ctx, http.MethodDelete, "/remove-canned-policy", q, nil)
}

// RustFSCreds holds the connection details returned after registering
// a tentacle with RustFS (MinIO-compatible object storage).
type RustFSCreds struct {
	Endpoint  string `json:"endpoint"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	Region    string `json:"region"`
	Protocol  string `json:"protocol"`
}

// RustFSRegistrar manages per-tentacle RustFS IAM users, policies, and
// prefix-scoped access.
type RustFSRegistrar struct {
	admin *rustfsAdmin
	s3    *minio.Client
	cfg   RustFSConfig
}

// lazyCATransport wraps an http.Transport and defers loading a CA cert file
// until the first HTTP request. This handles the first-install race where
// cert-manager hasn't created the CA secret yet when the registrar initializes.
// On each request, if the cert hasn't been loaded yet, it attempts to read the
// file. Once the cert is successfully loaded, it is cached and no further file
// reads occur.
type lazyCATransport struct {
	inner    *http.Transport
	certPath string
	mu       sync.Mutex
	loaded   bool
}

func (t *lazyCATransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	if !t.loaded {
		if pemData, err := os.ReadFile(t.certPath); err == nil && len(pemData) > 0 {
			pool, sysErr := x509.SystemCertPool()
			if sysErr != nil {
				pool = x509.NewCertPool()
			}
			if pool.AppendCertsFromPEM(pemData) {
				t.inner.CloseIdleConnections()
				newTransport := t.inner.Clone()
				newTransport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
				t.inner = newTransport
				t.loaded = true
				slog.Info("exoskeleton: rustfs CA cert loaded from file (deferred)", "path", t.certPath)
			}
		}
	}
	inner := t.inner
	t.mu.Unlock()
	return inner.RoundTrip(req)
}

// NewRustFSRegistrar creates a new RustFS registrar with admin and S3 clients.
func NewRustFSRegistrar(_ context.Context, cfg RustFSConfig) (*RustFSRegistrar, error) {
	endpoint := strings.TrimPrefix(strings.TrimPrefix(cfg.Endpoint, "http://"), "https://")
	useSSL := strings.HasPrefix(cfg.Endpoint, "https://")

	// Build custom TLS transport when a CA cert is configured and SSL is enabled.
	// CACertPEM (env var with PEM content) takes precedence over CACertPath (file path).
	var httpClient *http.Client
	var transport http.RoundTripper
	if useSSL && (cfg.CACertPEM != "" || cfg.CACertPath != "") {
		var pemData []byte
		if cfg.CACertPEM != "" {
			pemData = []byte(cfg.CACertPEM)
		} else {
			var err error
			pemData, err = os.ReadFile(cfg.CACertPath)
			if err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("rustfs read CA cert %s: %w", cfg.CACertPath, err)
			}
			if os.IsNotExist(err) {
				// File doesn't exist yet (e.g., cert-manager hasn't created the
				// CA secret on first install). Use a lazy transport that retries
				// loading the file on each request until it appears.
				slog.Warn("exoskeleton: rustfs CA cert file not yet available, will load on first use", "path", cfg.CACertPath)
				baseTransport := http.DefaultTransport.(*http.Transport).Clone()
				transport = &lazyCATransport{inner: baseTransport, certPath: cfg.CACertPath}
				httpClient = &http.Client{Transport: transport}
			}
		}
		if len(pemData) > 0 {
			pool, sysErr := x509.SystemCertPool()
			if sysErr != nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(pemData) {
				return nil, errors.New("rustfs CA cert: no valid PEM certificates found")
			}
			tlsConfig := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
			baseTransport := http.DefaultTransport.(*http.Transport).Clone()
			baseTransport.TLSClientConfig = tlsConfig
			transport = baseTransport
			httpClient = &http.Client{Transport: transport}
			slog.Info("exoskeleton: rustfs using custom CA cert")
		}
	}

	s3Client, err := minio.New(endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:    useSSL,
		Region:    cfg.Region,
		Transport: transport,
	})
	if err != nil {
		return nil, fmt.Errorf("rustfs s3 client: %w", err)
	}

	// Build the full scheme+host endpoint for the admin client.
	scheme := "http://"
	if useSSL {
		scheme = "https://"
	}
	adminEndpoint := scheme + endpoint

	admin := newRustFSAdmin(adminEndpoint, cfg.AccessKey, cfg.SecretKey, cfg.Region, httpClient)

	return &RustFSRegistrar{admin: admin, s3: s3Client, cfg: cfg}, nil
}

// Register creates (or updates) a scoped RustFS IAM user and policy for
// the given identity. It is idempotent.
func (r *RustFSRegistrar) Register(ctx context.Context, id Identity) (*RustFSCreds, error) {
	bucket := r.cfg.Bucket
	prefix := id.S3Prefix
	userName := id.S3User
	policyName := id.S3Policy

	// Ensure bucket exists.
	exists, err := r.s3.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket %s: %w", bucket, err)
	}
	if !exists {
		if mkErr := r.s3.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: r.cfg.Region}); mkErr != nil {
			// Ignore "already exists" errors from race conditions.
			errResp := minio.ToErrorResponse(mkErr)
			if errResp.Code != "BucketAlreadyOwnedByYou" && errResp.Code != "BucketAlreadyExists" {
				return nil, fmt.Errorf("create bucket %s: %w", bucket, mkErr)
			}
		}
	}

	// Generate secret key for the scoped IAM user.
	// Use the deterministic userName (id.S3User) as the MinIO access key so that
	// Unregister can find and remove the user by the same deterministic name.
	secretKey, err := generateHexPassword(32)
	if err != nil {
		return nil, fmt.Errorf("generate secret key: %w", err)
	}

	// Create or update the IAM user using the deterministic userName as access key.
	if addErr := r.admin.AddUser(ctx, userName, secretKey); addErr != nil {
		return nil, fmt.Errorf("add user %s: %w", userName, addErr)
	}

	// Build a canned policy scoped to the prefix.
	policy := buildS3Policy(bucket, prefix)
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("marshal policy: %w", err)
	}

	if err := r.admin.AddCannedPolicy(ctx, policyName, policyJSON); err != nil {
		return nil, fmt.Errorf("add policy %s: %w", policyName, err)
	}

	// Attach the policy to the user.
	if err := r.admin.SetPolicy(ctx, policyName, userName); err != nil {
		return nil, fmt.Errorf("set policy %s for user %s: %w", policyName, userName, err)
	}

	slog.Info("rustfs: registered tentacle",
		"user", userName, "policy", policyName,
		"bucket", bucket, "prefix", prefix)

	return &RustFSCreds{
		Endpoint:  r.cfg.Endpoint,
		AccessKey: userName,
		SecretKey: secretKey,
		Bucket:    bucket,
		Prefix:    prefix,
		Region:    r.cfg.Region,
		Protocol:  "s3",
	}, nil
}

// Unregister removes the tentacle's objects, policy, and IAM user.
func (r *RustFSRegistrar) Unregister(ctx context.Context, id Identity) error {
	bucket := r.cfg.Bucket
	prefix := id.S3Prefix
	userName := id.S3User
	policyName := id.S3Policy

	var errs []string

	// Delete all objects under the prefix.
	objectCh := r.s3.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
	for obj := range objectCh {
		if obj.Err != nil {
			errs = append(errs, fmt.Sprintf("list objects: %v", obj.Err))
			break
		}
		if err := r.s3.RemoveObject(ctx, bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
			errs = append(errs, fmt.Sprintf("delete object %s: %v", obj.Key, err))
		}
	}

	// Remove the IAM user first (detaches policy automatically).
	// Must happen before policy removal — RustFS rejects removing a policy
	// that is still attached to a user ("policy in use").
	if err := r.admin.RemoveUser(ctx, userName); err != nil {
		errs = append(errs, fmt.Sprintf("remove user %s: %v", userName, err))
	}

	// Remove the canned policy (now that no user references it).
	if err := r.admin.RemoveCannedPolicy(ctx, policyName); err != nil {
		errs = append(errs, fmt.Sprintf("remove policy %s: %v", policyName, err))
	}

	slog.Info("rustfs: unregistered tentacle",
		"user", userName, "policy", policyName,
		"bucket", bucket, "prefix", prefix)

	if len(errs) > 0 {
		return fmt.Errorf("rustfs unregister: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Close is a no-op since the clients don't hold persistent connections.
func (*RustFSRegistrar) Close() {}

// s3PolicyDoc represents a MinIO/S3 IAM policy document.
type s3PolicyDoc struct {
	Version   string         `json:"Version"`
	Statement []s3PolicyStmt `json:"Statement"`
}

type s3PolicyStmt struct {
	Resource  any      `json:"Resource,omitempty"`
	Condition any      `json:"Condition,omitempty"`
	Effect    string   `json:"Effect"`
	Action    []string `json:"Action"`
}

// buildS3Policy creates an IAM policy granting GetObject, PutObject,
// DeleteObject on objects under the prefix, and ListBucket with a
// prefix condition.
func buildS3Policy(bucket, prefix string) s3PolicyDoc {
	arnBucket := "arn:aws:s3:::" + bucket
	arnPrefix := fmt.Sprintf("arn:aws:s3:::%s/%s*", bucket, prefix)

	return s3PolicyDoc{
		Version: "2012-10-17",
		Statement: []s3PolicyStmt{
			{
				Effect:   "Allow",
				Action:   []string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject"},
				Resource: arnPrefix,
			},
			{
				Effect:   "Allow",
				Action:   []string{"s3:ListBucket"},
				Resource: arnBucket,
				Condition: map[string]any{
					"StringLike": map[string]string{
						"s3:prefix": prefix + "*",
					},
				},
			},
		},
	}
}
