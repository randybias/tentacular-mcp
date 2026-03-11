package exoskeleton

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// mockNATSAdmin records calls and returns configurable results.
type mockNATSAdmin struct {
	createCalls []mockNATSCall
	getCalls    []string
	updateCalls []mockNATSCall
	deleteCalls []string

	createErr error
	getResult bool
	getErr    error
	updateErr error
	deleteErr error

	lastToken string // last token returned
}

type mockNATSCall struct {
	Principal string
	Perm      NATSPermission
}

func newMockNATSAdmin() *mockNATSAdmin {
	return &mockNATSAdmin{}
}

func (m *mockNATSAdmin) CreateUser(_ context.Context, principal string, perm NATSPermission) (string, error) {
	m.createCalls = append(m.createCalls, mockNATSCall{Principal: principal, Perm: perm})
	if m.createErr != nil {
		return "", m.createErr
	}
	token, _ := generateNATSToken()
	m.lastToken = token
	return token, nil
}

func (m *mockNATSAdmin) GetUser(_ context.Context, principal string) (bool, error) {
	m.getCalls = append(m.getCalls, principal)
	if m.getErr != nil {
		return false, m.getErr
	}
	return m.getResult, nil
}

func (m *mockNATSAdmin) UpdateUser(_ context.Context, principal string, perm NATSPermission) (string, error) {
	m.updateCalls = append(m.updateCalls, mockNATSCall{Principal: principal, Perm: perm})
	if m.updateErr != nil {
		return "", m.updateErr
	}
	token, _ := generateNATSToken()
	m.lastToken = token
	return token, nil
}

func (m *mockNATSAdmin) DeleteUser(_ context.Context, principal string) error {
	m.deleteCalls = append(m.deleteCalls, principal)
	return m.deleteErr
}

func TestNATSRegister(t *testing.T) {
	mock := newMockNATSAdmin()
	reg := NewNATSRegistrar(NATSConfig{
		URL:   "nats://nats.example.com:4222",
		Token: "admin-token",
	})
	reg.SetAdmin(mock)

	id := CompileIdentity("tent-myapp", "hello-world")
	result, err := reg.Register(context.Background(), id)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Verify CreateUser was called once
	if len(mock.createCalls) != 1 {
		t.Fatalf("expected 1 CreateUser call, got %d", len(mock.createCalls))
	}

	// Verify correct principal
	if mock.createCalls[0].Principal != id.NATSPrincipal {
		t.Errorf("principal mismatch: got %s, want %s", mock.createCalls[0].Principal, id.NATSPrincipal)
	}

	// Verify subject scope matches IdentityCompiler output
	expectedPub := id.NATSSubjectPrefix + ".>"
	expectedSub := id.NATSSubjectPrefix + ".>"
	if mock.createCalls[0].Perm.Publish != expectedPub {
		t.Errorf("publish scope mismatch: got %s, want %s", mock.createCalls[0].Perm.Publish, expectedPub)
	}
	if mock.createCalls[0].Perm.Subscribe != expectedSub {
		t.Errorf("subscribe scope mismatch: got %s, want %s", mock.createCalls[0].Perm.Subscribe, expectedSub)
	}

	// Verify registration result — v1 returns the shared config token
	if result.URL != "nats://nats.example.com:4222" {
		t.Errorf("URL mismatch: got %s", result.URL)
	}
	if result.Token != "admin-token" {
		t.Errorf("token should be config token 'admin-token', got %q", result.Token)
	}
	if result.SubjectPrefix != id.NATSSubjectPrefix {
		t.Errorf("subject prefix mismatch: got %s, want %s", result.SubjectPrefix, id.NATSSubjectPrefix)
	}
	if result.Principal != id.NATSPrincipal {
		t.Errorf("principal mismatch: got %s, want %s", result.Principal, id.NATSPrincipal)
	}
}

func TestNATSRegisterSubjectScope(t *testing.T) {
	mock := newMockNATSAdmin()
	reg := NewNATSRegistrar(NATSConfig{URL: "nats://localhost:4222"})
	reg.SetAdmin(mock)

	// Test that the subject scope uses the IdentityCompiler's NATSSubjectPrefix
	id := CompileIdentity("tent-production", "api-gateway")
	_, err := reg.Register(context.Background(), id)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	perm := mock.createCalls[0].Perm
	// Subject prefix should contain the namespace and workflow
	if !strings.Contains(perm.Publish, "tentacular.") {
		t.Errorf("publish scope should contain 'tentacular.' prefix: %s", perm.Publish)
	}
	if !strings.HasSuffix(perm.Publish, ".>") {
		t.Errorf("publish scope should end with '.>': %s", perm.Publish)
	}
	if !strings.HasSuffix(perm.Subscribe, ".>") {
		t.Errorf("subscribe scope should end with '.>': %s", perm.Subscribe)
	}
}

func TestNATSRegisterNotConnected(t *testing.T) {
	reg := NewNATSRegistrar(NATSConfig{})
	_, err := reg.Register(context.Background(), CompileIdentity("ns", "wf"))
	if err == nil {
		t.Fatal("expected error for unconnected registrar")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("expected 'not connected' error, got: %v", err)
	}
}

func TestNATSRegisterCreateError(t *testing.T) {
	mock := newMockNATSAdmin()
	mock.createErr = fmt.Errorf("connection refused")
	reg := NewNATSRegistrar(NATSConfig{URL: "nats://localhost:4222"})
	reg.SetAdmin(mock)

	_, err := reg.Register(context.Background(), CompileIdentity("tent-myapp", "wf"))
	if err == nil {
		t.Fatal("expected error on CreateUser failure")
	}
	if !strings.Contains(err.Error(), "create user") {
		t.Errorf("expected 'create user' error, got: %v", err)
	}
}

func TestNATSReRegisterUserExists(t *testing.T) {
	mock := newMockNATSAdmin()
	mock.getResult = true // user exists
	reg := NewNATSRegistrar(NATSConfig{URL: "nats://localhost:4222", Token: "shared-token"})
	reg.SetAdmin(mock)

	id := CompileIdentity("tent-myapp", "hello-world")
	result, err := reg.ReRegister(context.Background(), id)
	if err != nil {
		t.Fatalf("ReRegister failed: %v", err)
	}

	// Should call GetUser then UpdateUser (not CreateUser)
	if len(mock.getCalls) != 1 {
		t.Fatalf("expected 1 GetUser call, got %d", len(mock.getCalls))
	}
	if len(mock.updateCalls) != 1 {
		t.Fatalf("expected 1 UpdateUser call, got %d", len(mock.updateCalls))
	}
	if len(mock.createCalls) != 0 {
		t.Errorf("should NOT call CreateUser when user exists, got %d calls", len(mock.createCalls))
	}

	// Verify permissions are correct
	if mock.updateCalls[0].Perm.Publish != id.NATSSubjectPrefix+".>" {
		t.Errorf("publish scope mismatch: got %s", mock.updateCalls[0].Perm.Publish)
	}

	if result.Token != "shared-token" {
		t.Errorf("token should be config token 'shared-token', got %q", result.Token)
	}
	if result.Principal != id.NATSPrincipal {
		t.Errorf("principal mismatch: got %s, want %s", result.Principal, id.NATSPrincipal)
	}
}

func TestNATSReRegisterUserMissing(t *testing.T) {
	mock := newMockNATSAdmin()
	mock.getResult = false // user does not exist
	reg := NewNATSRegistrar(NATSConfig{URL: "nats://localhost:4222", Token: "shared-token"})
	reg.SetAdmin(mock)

	id := CompileIdentity("tent-myapp", "hello-world")
	result, err := reg.ReRegister(context.Background(), id)
	if err != nil {
		t.Fatalf("ReRegister failed: %v", err)
	}

	// Should call GetUser then CreateUser (drift repair)
	if len(mock.getCalls) != 1 {
		t.Fatalf("expected 1 GetUser call, got %d", len(mock.getCalls))
	}
	if len(mock.createCalls) != 1 {
		t.Fatalf("expected 1 CreateUser call (drift repair), got %d", len(mock.createCalls))
	}
	if len(mock.updateCalls) != 0 {
		t.Errorf("should NOT call UpdateUser when user is missing, got %d calls", len(mock.updateCalls))
	}

	if result.Token != "shared-token" {
		t.Errorf("token should be config token 'shared-token', got %q", result.Token)
	}
}

func TestNATSReRegisterNotConnected(t *testing.T) {
	reg := NewNATSRegistrar(NATSConfig{})
	_, err := reg.ReRegister(context.Background(), CompileIdentity("ns", "wf"))
	if err == nil {
		t.Fatal("expected error for unconnected registrar")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("expected 'not connected' error, got: %v", err)
	}
}

func TestNATSReRegisterGetError(t *testing.T) {
	mock := newMockNATSAdmin()
	mock.getErr = fmt.Errorf("nats unreachable")
	reg := NewNATSRegistrar(NATSConfig{URL: "nats://localhost:4222"})
	reg.SetAdmin(mock)

	_, err := reg.ReRegister(context.Background(), CompileIdentity("tent-myapp", "wf"))
	if err == nil {
		t.Fatal("expected error on GetUser failure")
	}
	if !strings.Contains(err.Error(), "check user") {
		t.Errorf("expected 'check user' error, got: %v", err)
	}
}

func TestNATSReRegisterUpdateError(t *testing.T) {
	mock := newMockNATSAdmin()
	mock.getResult = true
	mock.updateErr = fmt.Errorf("permission denied")
	reg := NewNATSRegistrar(NATSConfig{URL: "nats://localhost:4222"})
	reg.SetAdmin(mock)

	_, err := reg.ReRegister(context.Background(), CompileIdentity("tent-myapp", "wf"))
	if err == nil {
		t.Fatal("expected error on UpdateUser failure")
	}
	if !strings.Contains(err.Error(), "update user") {
		t.Errorf("expected 'update user' error, got: %v", err)
	}
}

func TestNATSUnregister(t *testing.T) {
	mock := newMockNATSAdmin()
	reg := NewNATSRegistrar(NATSConfig{URL: "nats://localhost:4222"})
	reg.SetAdmin(mock)

	id := CompileIdentity("tent-myapp", "hello-world")
	err := reg.Unregister(context.Background(), id)
	if err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	// Verify DeleteUser was called with the correct principal
	if len(mock.deleteCalls) != 1 {
		t.Fatalf("expected 1 DeleteUser call, got %d", len(mock.deleteCalls))
	}
	if mock.deleteCalls[0] != id.NATSPrincipal {
		t.Errorf("principal mismatch: got %s, want %s", mock.deleteCalls[0], id.NATSPrincipal)
	}
}

func TestNATSUnregisterNotConnected(t *testing.T) {
	reg := NewNATSRegistrar(NATSConfig{})
	err := reg.Unregister(context.Background(), CompileIdentity("ns", "wf"))
	if err == nil {
		t.Fatal("expected error for unconnected registrar")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("expected 'not connected' error, got: %v", err)
	}
}

func TestNATSUnregisterDeleteError(t *testing.T) {
	mock := newMockNATSAdmin()
	mock.deleteErr = fmt.Errorf("nats unreachable")
	reg := NewNATSRegistrar(NATSConfig{URL: "nats://localhost:4222"})
	reg.SetAdmin(mock)

	err := reg.Unregister(context.Background(), CompileIdentity("tent-myapp", "wf"))
	if err == nil {
		t.Fatal("expected error on DeleteUser failure")
	}
	if !strings.Contains(err.Error(), "delete user") {
		t.Errorf("expected 'delete user' error, got: %v", err)
	}
}

func TestNATSTokenGeneration(t *testing.T) {
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := generateNATSToken()
		if err != nil {
			t.Fatalf("generateNATSToken failed: %v", err)
		}
		if len(token) != 64 { // 32 bytes hex-encoded
			t.Errorf("token should be 64 hex chars, got %d", len(token))
		}
		if tokens[token] {
			t.Errorf("duplicate token generated: %s", token)
		}
		tokens[token] = true
	}
}

func TestInMemoryNATSAdmin(t *testing.T) {
	admin := NewInMemoryNATSAdmin()
	ctx := context.Background()
	perm := NATSPermission{
		Publish:   "tentacular.tent_myapp.hello_world.>",
		Subscribe: "tentacular.tent_myapp.hello_world.>",
	}

	// Create user
	token, err := admin.CreateUser(ctx, "tent_myapp.hello_world", perm)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if token == "" {
		t.Error("token should not be empty")
	}

	// Check user exists
	exists, err := admin.GetUser(ctx, "tent_myapp.hello_world")
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if !exists {
		t.Error("user should exist after creation")
	}

	// Check non-existent user
	exists, err = admin.GetUser(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if exists {
		t.Error("non-existent user should not exist")
	}

	// Update user
	newToken, err := admin.UpdateUser(ctx, "tent_myapp.hello_world", perm)
	if err != nil {
		t.Fatalf("UpdateUser failed: %v", err)
	}
	if newToken == "" {
		t.Error("new token should not be empty")
	}
	if newToken == token {
		t.Error("UpdateUser should rotate the token")
	}

	// Delete user
	err = admin.DeleteUser(ctx, "tent_myapp.hello_world")
	if err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}

	// Verify deleted
	exists, err = admin.GetUser(ctx, "tent_myapp.hello_world")
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if exists {
		t.Error("user should not exist after deletion")
	}

	// Delete non-existent user should not error
	err = admin.DeleteUser(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("DeleteUser on non-existent should not error: %v", err)
	}
}
