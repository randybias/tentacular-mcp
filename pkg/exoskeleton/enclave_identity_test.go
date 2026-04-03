package exoskeleton

import (
	"strings"
	"testing"
)

// --- CompileEnclaveIdentity ---

func TestCompileEnclaveIdentity_Basic(t *testing.T) {
	id, err := CompileEnclaveIdentity("marketing-workflows")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Enclave != "marketing-workflows" {
		t.Errorf("Enclave = %q, want %q", id.Enclave, "marketing-workflows")
	}
	if id.PgDB != "tn_marketing_workflows" {
		t.Errorf("PgDB = %q, want %q", id.PgDB, "tn_marketing_workflows")
	}
	if id.S3Bucket != "tentacular-marketing-workflows" {
		t.Errorf("S3Bucket = %q, want %q", id.S3Bucket, "tentacular-marketing-workflows")
	}
	if id.NATSAcct != "tentacular.marketing-workflows" {
		t.Errorf("NATSAcct = %q, want %q", id.NATSAcct, "tentacular.marketing-workflows")
	}
}

func TestCompileEnclaveIdentity_EmptyName(t *testing.T) {
	_, err := CompileEnclaveIdentity("")
	if err == nil {
		t.Error("expected error for empty enclave name, got nil")
	}
}

func TestCompileEnclaveIdentity_LongName_Truncated(t *testing.T) {
	longName := strings.Repeat("a", 60)
	id, err := CompileEnclaveIdentity(longName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(id.PgDB) > maxPgIdentLen {
		t.Errorf("PgDB length %d exceeds max %d", len(id.PgDB), maxPgIdentLen)
	}
	// Should have a hash suffix (ends with _xxxxxxxx pattern)
	if !strings.Contains(id.PgDB, "_") {
		t.Errorf("PgDB %q missing hash suffix for long name", id.PgDB)
	}
}

func TestCompileEnclaveIdentity_HyphenVsUnderscore_PgDB(t *testing.T) {
	// H1 mitigation: underscores are not valid DNS-1123 labels, so "my_team" is now
	// rejected by ValidateEnclaveName before any PgDB collision can occur.
	_, err1 := CompileEnclaveIdentity("my-team")
	if err1 != nil {
		t.Errorf("CompileEnclaveIdentity(\"my-team\") unexpected error: %v", err1)
	}
	_, err2 := CompileEnclaveIdentity("my_team")
	if err2 == nil {
		t.Error("CompileEnclaveIdentity(\"my_team\") expected error (underscore not DNS-1123), got nil")
	}
}

func TestCompileEnclaveIdentity_HyphenVsUnderscore_S3Bucket(t *testing.T) {
	// H1 mitigation: "my_team" is rejected before reaching S3Bucket construction.
	_, err1 := CompileEnclaveIdentity("my-team")
	if err1 != nil {
		t.Errorf("CompileEnclaveIdentity(\"my-team\") unexpected error: %v", err1)
	}
	_, err2 := CompileEnclaveIdentity("my_team")
	if err2 == nil {
		t.Error("CompileEnclaveIdentity(\"my_team\") expected error (underscore not DNS-1123), got nil")
	}
}

func TestCompileEnclaveIdentity_LongSimilarNames_DistinctPgDB(t *testing.T) {
	// Two 60-char names sharing a 55-char prefix, differing in last 5 chars
	prefix := strings.Repeat("a", 55)
	name1 := prefix + "bbbbb"
	name2 := prefix + "ccccc"
	id1, err1 := CompileEnclaveIdentity(name1)
	id2, err2 := CompileEnclaveIdentity(name2)
	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v, %v", err1, err2)
	}
	if id1.PgDB == id2.PgDB {
		t.Errorf("PgDB for similar long names should be distinct: both = %q", id1.PgDB)
	}
}

// --- CompileTentacleIdentity ---

func TestCompileTentacleIdentity_Basic(t *testing.T) {
	id, err := CompileTentacleIdentity("marketing-workflows", "competitor-scraper")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Enclave.Enclave != "marketing-workflows" {
		t.Errorf("Enclave.Enclave = %q, want %q", id.Enclave.Enclave, "marketing-workflows")
	}
	if id.Tentacle != "competitor-scraper" {
		t.Errorf("Tentacle = %q, want %q", id.Tentacle, "competitor-scraper")
	}
	if id.PgSchema != "tn_marketing_workflows_competitor_scraper" {
		t.Errorf("PgSchema = %q, want %q", id.PgSchema, "tn_marketing_workflows_competitor_scraper")
	}
	if id.S3Prefix != "tentacles/competitor-scraper/" {
		t.Errorf("S3Prefix = %q, want %q", id.S3Prefix, "tentacles/competitor-scraper/")
	}
	if id.NATSUser != "tentacular.marketing-workflows.competitor-scraper" {
		t.Errorf("NATSUser = %q, want %q", id.NATSUser, "tentacular.marketing-workflows.competitor-scraper")
	}
	if id.SpireSVID != "spiffe://tentacular/enclaves/marketing-workflows/tentacles/competitor-scraper" {
		t.Errorf("SpireSVID = %q, want %q", id.SpireSVID, "spiffe://tentacular/enclaves/marketing-workflows/tentacles/competitor-scraper")
	}
}

func TestCompileTentacleIdentity_EmptyEnclave(t *testing.T) {
	_, err := CompileTentacleIdentity("", "competitor-scraper")
	if err == nil {
		t.Error("expected error for empty enclave, got nil")
	}
}

func TestCompileTentacleIdentity_EmptyTentacle(t *testing.T) {
	_, err := CompileTentacleIdentity("marketing-workflows", "")
	if err == nil {
		t.Error("expected error for empty tentacle, got nil")
	}
}

func TestCompileTentacleIdentity_EnclaveIdentityEmbedded(t *testing.T) {
	id, err := CompileTentacleIdentity("marketing-workflows", "competitor-scraper")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Enclave-level fields should match what CompileEnclaveIdentity returns
	enc, err := CompileEnclaveIdentity("marketing-workflows")
	if err != nil {
		t.Fatalf("unexpected error compiling enclave identity: %v", err)
	}
	if id.Enclave.PgDB != enc.PgDB {
		t.Errorf("embedded PgDB = %q, want %q", id.Enclave.PgDB, enc.PgDB)
	}
	if id.Enclave.S3Bucket != enc.S3Bucket {
		t.Errorf("embedded S3Bucket = %q, want %q", id.Enclave.S3Bucket, enc.S3Bucket)
	}
	if id.Enclave.NATSAcct != enc.NATSAcct {
		t.Errorf("embedded NATSAcct = %q, want %q", id.Enclave.NATSAcct, enc.NATSAcct)
	}
}

func TestCompileTentacleIdentity_LongNames_PgSchemaTruncated(t *testing.T) {
	longEnclave := strings.Repeat("e", 30)
	longTentacle := strings.Repeat("t", 30)
	id, err := CompileTentacleIdentity(longEnclave, longTentacle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(id.PgSchema) > maxPgIdentLen {
		t.Errorf("PgSchema length %d exceeds max %d", len(id.PgSchema), maxPgIdentLen)
	}
}

// --- ValidateEnclaveName (exoskeleton) ---

func TestValidateEnclaveName_Exo_Valid(t *testing.T) {
	validNames := []string{
		"a",
		"ab",
		"my-enclave",
		"marketing-workflows",
		"abc123",
		strings.Repeat("a", 63),
	}
	for _, name := range validNames {
		if err := ValidateEnclaveName(name); err != nil {
			t.Errorf("ValidateEnclaveName(%q) unexpected error: %v", name, err)
		}
	}
}

func TestValidateEnclaveName_Exo_Empty(t *testing.T) {
	if err := ValidateEnclaveName(""); err == nil {
		t.Error("ValidateEnclaveName(\"\") expected error, got nil")
	}
}

func TestValidateEnclaveName_Exo_Underscore(t *testing.T) {
	if err := ValidateEnclaveName("my_enclave"); err == nil {
		t.Error("ValidateEnclaveName(\"my_enclave\") expected error, got nil")
	}
}

func TestValidateEnclaveName_Exo_DotRejected(t *testing.T) {
	if err := ValidateEnclaveName("my.enclave"); err == nil {
		t.Error("ValidateEnclaveName(\"my.enclave\") expected error for '.', got nil")
	}
}

func TestValidateEnclaveName_Exo_TooLong(t *testing.T) {
	name := strings.Repeat("a", 64)
	if err := ValidateEnclaveName(name); err == nil {
		t.Errorf("ValidateEnclaveName(%q) expected error for >63 chars, got nil", name)
	}
}

func TestValidateEnclaveName_Exo_LeadingHyphen(t *testing.T) {
	if err := ValidateEnclaveName("-bad"); err == nil {
		t.Error("ValidateEnclaveName(\"-bad\") expected error for leading hyphen, got nil")
	}
}

func TestValidateEnclaveName_Exo_TrailingHyphen(t *testing.T) {
	if err := ValidateEnclaveName("bad-"); err == nil {
		t.Error("ValidateEnclaveName(\"bad-\") expected error for trailing hyphen, got nil")
	}
}

// --- truncateS3Bucket ---

func TestTruncateS3Bucket_ShortName(t *testing.T) {
	// "tentacular-" (11) + "marketing-workflows" (19) = 30 chars — no truncation
	raw := "tentacular-marketing-workflows"
	result := truncateS3Bucket(raw)
	if result != raw {
		t.Errorf("truncateS3Bucket(%q) = %q, want unchanged", raw, result)
	}
}

func TestTruncateS3Bucket_ExactlyMaxLen(t *testing.T) {
	// Exactly 63 chars: no truncation
	raw := strings.Repeat("a", maxS3BucketLen)
	result := truncateS3Bucket(raw)
	if result != raw {
		t.Errorf("truncateS3Bucket at maxLen: got %q, want unchanged", result)
	}
	if len(result) != maxS3BucketLen {
		t.Errorf("truncateS3Bucket at maxLen: len = %d, want %d", len(result), maxS3BucketLen)
	}
}

func TestTruncateS3Bucket_TooLong(t *testing.T) {
	// "tentacular-" (11) + 52-char name = 63, "tentacular-" + 53-char name = 64 -> truncation
	enclaveName := strings.Repeat("a", 53)
	raw := "tentacular-" + enclaveName
	result := truncateS3Bucket(raw)
	if len(result) > maxS3BucketLen {
		t.Errorf("truncateS3Bucket: result length %d exceeds max %d: %q", len(result), maxS3BucketLen, result)
	}
	if len(result) == len(raw) {
		t.Errorf("truncateS3Bucket: long input was not truncated")
	}
}

func TestTruncateS3Bucket_TooLong_Deterministic(t *testing.T) {
	enclaveName := strings.Repeat("b", 53)
	raw := "tentacular-" + enclaveName
	r1 := truncateS3Bucket(raw)
	r2 := truncateS3Bucket(raw)
	if r1 != r2 {
		t.Errorf("truncateS3Bucket not deterministic: %q != %q", r1, r2)
	}
}

func TestTruncateS3Bucket_TooLong_DistinctInputs(t *testing.T) {
	// Two different long names must produce distinct bucket names
	raw1 := "tentacular-" + strings.Repeat("a", 53)
	raw2 := "tentacular-" + strings.Repeat("b", 53)
	r1 := truncateS3Bucket(raw1)
	r2 := truncateS3Bucket(raw2)
	if r1 == r2 {
		t.Errorf("truncateS3Bucket: distinct long inputs produced same result: %q", r1)
	}
}

func TestCompileEnclaveIdentity_LongName_S3BucketTruncated(t *testing.T) {
	// Enclave name of 53 chars -> "tentacular-" + 53 = 64 -> must be truncated to 63
	enclaveName := strings.Repeat("a", 53)
	id, err := CompileEnclaveIdentity(enclaveName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(id.S3Bucket) > maxS3BucketLen {
		t.Errorf("S3Bucket length %d exceeds max %d: %q", len(id.S3Bucket), maxS3BucketLen, id.S3Bucket)
	}
}

func TestCompileEnclaveIdentity_MaxSafeName_S3BucketNotTruncated(t *testing.T) {
	// "tentacular-" (11) + 52 chars = 63 exactly -> no truncation needed
	enclaveName := strings.Repeat("a", 52)
	id, err := CompileEnclaveIdentity(enclaveName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "tentacular-" + enclaveName
	if id.S3Bucket != want {
		t.Errorf("S3Bucket = %q, want %q (no truncation for 52-char enclave)", id.S3Bucket, want)
	}
}

// --- Existing CompileIdentity unchanged ---

func TestCompileIdentity_Unchanged(t *testing.T) {
	id, err := CompileIdentity("my-ns", "my-wf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Principal != "spiffe://tentacular/ns/my-ns/tentacles/my-wf" {
		t.Errorf("Principal = %q", id.Principal)
	}
	if id.PgRole != "tn_my_ns_my_wf" {
		t.Errorf("PgRole = %q", id.PgRole)
	}
	if id.NATSUser != "tentacle.my-ns.my-wf" {
		t.Errorf("NATSUser = %q", id.NATSUser)
	}
	if id.S3Prefix != "ns/my-ns/tentacles/my-wf/" {
		t.Errorf("S3Prefix = %q", id.S3Prefix)
	}
}
