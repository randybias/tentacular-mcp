package exoskeleton

import (
	"context"
	"fmt"
	"sync"
)

// NATSRegistration holds the result of a successful NATS registration.
type NATSRegistration struct {
	URL           string
	Token         string
	SubjectPrefix string
	Principal     string
}

// NATSPermission describes a subject-scoped permission for a tentacle.
type NATSPermission struct {
	Publish   string
	Subscribe string
}

// NATSAdmin abstracts NATS management operations for testability.
// The v1 implementation uses token-based auth; future versions may use JWT/NKey.
type NATSAdmin interface {
	// CreateUser provisions credentials with the given permissions.
	// Returns the generated token for the user.
	CreateUser(ctx context.Context, principal string, perm NATSPermission) (token string, err error)

	// GetUser checks if a user/credential exists and returns true if so.
	GetUser(ctx context.Context, principal string) (exists bool, err error)

	// UpdateUser updates the permissions for an existing user.
	// Returns the (possibly rotated) token.
	UpdateUser(ctx context.Context, principal string, perm NATSPermission) (token string, err error)

	// DeleteUser revokes/removes the user's credentials.
	DeleteUser(ctx context.Context, principal string) error
}

// NATSRegistrar provisions per-tentacle NATS credentials with scoped permissions.
type NATSRegistrar struct {
	config NATSConfig
	admin  NATSAdmin
}

// NewNATSRegistrar creates a registrar with the given config.
func NewNATSRegistrar(config NATSConfig) *NATSRegistrar {
	return &NATSRegistrar{
		config: config,
	}
}

// SetAdmin sets the NATS admin interface. Used to inject a real implementation
// after connection or a mock for testing.
func (r *NATSRegistrar) SetAdmin(admin NATSAdmin) {
	r.admin = admin
}

// Register provisions new NATS credentials for the given identity with scoped
// publish/subscribe permissions on the tentacle's subject prefix.
func (r *NATSRegistrar) Register(ctx context.Context, id Identity) (*NATSRegistration, error) {
	if r.admin == nil {
		return nil, fmt.Errorf("nats registrar: not connected")
	}

	perm := NATSPermission{
		Publish:   id.NATSSubjectPrefix + ".>",
		Subscribe: id.NATSSubjectPrefix + ".>",
	}

	// Track the user and permissions in the admin backend.
	// The admin may generate its own internal token, but for v1 token auth
	// we return the shared config token since NATS only accepts that token.
	// JWT-scoped per-workflow tokens are planned for production.
	_, err := r.admin.CreateUser(ctx, id.NATSPrincipal, perm)
	if err != nil {
		return nil, fmt.Errorf("nats registrar: create user: %w", err)
	}

	return &NATSRegistration{
		URL:           r.config.URL,
		Token:         r.config.Token,
		SubjectPrefix: id.NATSSubjectPrefix,
		Principal:     id.NATSPrincipal,
	}, nil
}

// ReRegister verifies existing credentials and optionally reissues them.
// Preserves any JetStream durable state.
func (r *NATSRegistrar) ReRegister(ctx context.Context, id Identity) (*NATSRegistration, error) {
	if r.admin == nil {
		return nil, fmt.Errorf("nats registrar: not connected")
	}

	perm := NATSPermission{
		Publish:   id.NATSSubjectPrefix + ".>",
		Subscribe: id.NATSSubjectPrefix + ".>",
	}

	exists, err := r.admin.GetUser(ctx, id.NATSPrincipal)
	if err != nil {
		return nil, fmt.Errorf("nats registrar: check user: %w", err)
	}

	if exists {
		_, err = r.admin.UpdateUser(ctx, id.NATSPrincipal, perm)
		if err != nil {
			return nil, fmt.Errorf("nats registrar: update user: %w", err)
		}
	} else {
		// User doesn't exist — create it (handles drift)
		_, err = r.admin.CreateUser(ctx, id.NATSPrincipal, perm)
		if err != nil {
			return nil, fmt.Errorf("nats registrar: create user (drift repair): %w", err)
		}
	}

	// Return the shared config token for v1 token auth.
	// The admin tracks permissions internally; JWT-scoped tokens are planned.
	return &NATSRegistration{
		URL:           r.config.URL,
		Token:         r.config.Token,
		SubjectPrefix: id.NATSSubjectPrefix,
		Principal:     id.NATSPrincipal,
	}, nil
}

// Unregister revokes the tentacle's NATS credentials.
func (r *NATSRegistrar) Unregister(ctx context.Context, id Identity) error {
	if r.admin == nil {
		return fmt.Errorf("nats registrar: not connected")
	}

	if err := r.admin.DeleteUser(ctx, id.NATSPrincipal); err != nil {
		return fmt.Errorf("nats registrar: delete user: %w", err)
	}

	return nil
}

// InMemoryNATSAdmin is a simple in-process NATSAdmin implementation backed by
// a map. Suitable for development, testing, and v1 token-based auth where
// there is no external NATS auth management API.
type InMemoryNATSAdmin struct {
	mu    sync.RWMutex
	users map[string]inMemoryUser
}

type inMemoryUser struct {
	Token string
	Perm  NATSPermission
}

// NewInMemoryNATSAdmin creates an in-memory NATS admin backend.
func NewInMemoryNATSAdmin() *InMemoryNATSAdmin {
	return &InMemoryNATSAdmin{
		users: make(map[string]inMemoryUser),
	}
}

func (a *InMemoryNATSAdmin) CreateUser(_ context.Context, principal string, perm NATSPermission) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	token, err := generateNATSToken()
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}

	a.users[principal] = inMemoryUser{
		Token: token,
		Perm:  perm,
	}

	return token, nil
}

func (a *InMemoryNATSAdmin) GetUser(_ context.Context, principal string) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	_, exists := a.users[principal]
	return exists, nil
}

func (a *InMemoryNATSAdmin) UpdateUser(_ context.Context, principal string, perm NATSPermission) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	token, err := generateNATSToken()
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}

	a.users[principal] = inMemoryUser{
		Token: token,
		Perm:  perm,
	}

	return token, nil
}

func (a *InMemoryNATSAdmin) DeleteUser(_ context.Context, principal string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.users, principal)
	return nil
}

// generateNATSToken creates a cryptographically random 32-byte hex-encoded token.
func generateNATSToken() (string, error) {
	return generateRandomHex()
}
