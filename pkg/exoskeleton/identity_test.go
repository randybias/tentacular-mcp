package exoskeleton

import (
	"regexp"
	"strings"
	"testing"
)

func TestCompileIdentity_Standard(t *testing.T) {
	id := CompileIdentity("tent-myapp", "hello-world")

	if id.Namespace != "tent-myapp" {
		t.Errorf("Namespace = %q, want %q", id.Namespace, "tent-myapp")
	}
	if id.Workflow != "hello-world" {
		t.Errorf("Workflow = %q, want %q", id.Workflow, "hello-world")
	}
	if id.PostgresRole != "tn_tent_myapp_hello_world" {
		t.Errorf("PostgresRole = %q, want %q", id.PostgresRole, "tn_tent_myapp_hello_world")
	}
	if id.PostgresSchema != "tn_tent_myapp_hello_world" {
		t.Errorf("PostgresSchema = %q, want %q", id.PostgresSchema, "tn_tent_myapp_hello_world")
	}
	if id.NATSSubjectPrefix != "tentacular.tent_myapp.hello_world" {
		t.Errorf("NATSSubjectPrefix = %q, want %q", id.NATSSubjectPrefix, "tentacular.tent_myapp.hello_world")
	}
	if id.NATSPrincipal != "tent_myapp.hello_world" {
		t.Errorf("NATSPrincipal = %q, want %q", id.NATSPrincipal, "tent_myapp.hello_world")
	}
	if id.RustFSPrefix != "ns/tent-myapp/tentacles/hello-world/" {
		t.Errorf("RustFSPrefix = %q, want %q", id.RustFSPrefix, "ns/tent-myapp/tentacles/hello-world/")
	}
	if id.CanonicalPrincipal != "tent-myapp/hello-world" {
		t.Errorf("CanonicalPrincipal = %q, want %q", id.CanonicalPrincipal, "tent-myapp/hello-world")
	}
}

func TestCompileIdentity_Uppercase(t *testing.T) {
	id := CompileIdentity("Tent-MyApp", "Hello-World")

	// Should be lowercased
	if id.PostgresRole != "tn_tent_myapp_hello_world" {
		t.Errorf("PostgresRole = %q, want lowercased", id.PostgresRole)
	}
	if id.NATSSubjectPrefix != "tentacular.tent_myapp.hello_world" {
		t.Errorf("NATSSubjectPrefix = %q, want lowercased", id.NATSSubjectPrefix)
	}
}

func TestCompileIdentity_SpecialCharacters(t *testing.T) {
	id := CompileIdentity("tent-my.app@v2", "workflow#1!")

	// Special chars become underscores in Postgres
	if id.PostgresRole != "tn_tent_my_app_v2_workflow_1_" {
		t.Errorf("PostgresRole = %q, want %q", id.PostgresRole, "tn_tent_my_app_v2_workflow_1_")
	}
	// NATS also sanitizes special chars (dots, @, #, !) to underscores
	if id.NATSPrincipal != "tent_my_app_v2.workflow_1_" {
		t.Errorf("NATSPrincipal = %q, want %q", id.NATSPrincipal, "tent_my_app_v2.workflow_1_")
	}
}

func TestCompileIdentity_LongNames_Truncation(t *testing.T) {
	// Create names that will exceed 63 chars when combined with prefix
	longNS := strings.Repeat("a", 30)
	longWF := strings.Repeat("b", 30)

	id := CompileIdentity(longNS, longWF)

	// tn_ + 30 + _ + 30 = 64, which exceeds 63
	if len(id.PostgresRole) > 63 {
		t.Errorf("PostgresRole length = %d, should be <= 63", len(id.PostgresRole))
	}
	if len(id.PostgresSchema) > 63 {
		t.Errorf("PostgresSchema length = %d, should be <= 63", len(id.PostgresSchema))
	}
	// Should end with a hash suffix like _abcdef01
	hashSuffix := regexp.MustCompile(`_[0-9a-f]{8}$`)
	if !hashSuffix.MatchString(id.PostgresRole) {
		t.Errorf("truncated PostgresRole %q should end with a hex hash suffix", id.PostgresRole)
	}
}

func TestCompileIdentity_ExactlyAt63Chars(t *testing.T) {
	// tn_ (3) + ns (26) + _ (1) + wf (33) = 63 exactly
	ns := strings.Repeat("a", 26)
	wf := strings.Repeat("b", 33)

	id := CompileIdentity(ns, wf)

	if len(id.PostgresRole) != 63 {
		t.Errorf("PostgresRole length = %d, want exactly 63", len(id.PostgresRole))
	}
	// Should NOT be truncated (no hash suffix)
	expected := "tn_" + ns + "_" + wf
	if id.PostgresRole != expected {
		t.Errorf("PostgresRole = %q, want %q (no truncation needed)", id.PostgresRole, expected)
	}
}

func TestCompileIdentity_SingleCharNames(t *testing.T) {
	id := CompileIdentity("a", "b")

	if id.PostgresRole != "tn_a_b" {
		t.Errorf("PostgresRole = %q, want %q", id.PostgresRole, "tn_a_b")
	}
	if id.NATSSubjectPrefix != "tentacular.a.b" {
		t.Errorf("NATSSubjectPrefix = %q, want %q", id.NATSSubjectPrefix, "tentacular.a.b")
	}
	if id.RustFSPrefix != "ns/a/tentacles/b/" {
		t.Errorf("RustFSPrefix = %q, want %q", id.RustFSPrefix, "ns/a/tentacles/b/")
	}
}

func TestCompileIdentity_Deterministic(t *testing.T) {
	// Same input must always produce the same output
	id1 := CompileIdentity("tent-app", "workflow-1")
	id2 := CompileIdentity("tent-app", "workflow-1")

	if id1 != id2 {
		t.Error("CompileIdentity is not deterministic: two calls with same input produced different results")
	}
}

func TestCompileIdentity_DifferentInputsDifferentOutput(t *testing.T) {
	id1 := CompileIdentity("tent-app1", "workflow-1")
	id2 := CompileIdentity("tent-app2", "workflow-1")

	if id1.PostgresRole == id2.PostgresRole {
		t.Error("different namespaces should produce different Postgres roles")
	}
	if id1.NATSSubjectPrefix == id2.NATSSubjectPrefix {
		t.Error("different namespaces should produce different NATS subjects")
	}
}

func TestCompileIdentity_ConsistentAcrossServices(t *testing.T) {
	id := CompileIdentity("tent-myns", "mywf")

	// NATS and Postgres identifiers should be derived from the same namespace/workflow
	if !strings.Contains(id.PostgresRole, "tent_myns") {
		t.Error("PostgresRole should contain sanitized namespace")
	}
	if !strings.Contains(id.PostgresRole, "mywf") {
		t.Error("PostgresRole should contain sanitized workflow")
	}
	if !strings.Contains(id.NATSSubjectPrefix, "tent_myns") {
		t.Error("NATSSubjectPrefix should contain sanitized namespace")
	}
	if !strings.Contains(id.NATSSubjectPrefix, "mywf") {
		t.Error("NATSSubjectPrefix should contain sanitized workflow")
	}
}

func TestTruncatePostgresID_NoTruncation(t *testing.T) {
	short := "tn_short_name"
	result := truncatePostgresID(short)
	if result != short {
		t.Errorf("truncatePostgresID(%q) = %q, want unchanged", short, result)
	}
}

func TestTruncatePostgresID_Truncation(t *testing.T) {
	long := "tn_" + strings.Repeat("x", 70) // 73 chars, well over 63
	result := truncatePostgresID(long)

	if len(result) > 63 {
		t.Errorf("truncated result length = %d, should be <= 63", len(result))
	}
	if !strings.HasPrefix(result, "tn_") {
		t.Error("truncated result should preserve prefix")
	}
}

func TestTruncatePostgresID_Uniqueness(t *testing.T) {
	// Two different long identifiers should produce different truncated results
	id1 := "tn_" + strings.Repeat("a", 65)
	id2 := "tn_" + strings.Repeat("b", 65)

	r1 := truncatePostgresID(id1)
	r2 := truncatePostgresID(id2)

	if r1 == r2 {
		t.Error("different long identifiers should produce different truncated results")
	}
}

func TestSanitizePostgres(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello-world", "hello_world"},
		{"hello_world", "hello_world"},
		{"hello.world", "hello_world"},
		{"hello@world!", "hello_world_"},
		{"abc123", "abc123"},
		{"ABC", "abc"},
	}

	for _, tt := range tests {
		got := sanitizePostgres(strings.ToLower(tt.input))
		if got != tt.want {
			t.Errorf("sanitizePostgres(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
