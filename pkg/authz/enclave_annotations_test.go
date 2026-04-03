package authz

import (
	"strings"
	"testing"
)

// --- Annotation constants ---

func TestAnnotationConstants_Prefix(t *testing.T) {
	constants := []string{
		AnnotationEnclave,
		AnnotationEnclaveOwner,
		AnnotationEnclaveOwnerSub,
		AnnotationEnclaveMembers,
		AnnotationEnclavePlatform,
		AnnotationEnclaveChannelID,
		AnnotationEnclaveChannelName,
		AnnotationEnclaveStatus,
		AnnotationEnclaveDefaultMode,
		AnnotationEnclaveCreatedAt,
		AnnotationEnclaveUpdatedAt,
	}
	for _, c := range constants {
		if !strings.HasPrefix(c, "tentacular.io/enclave") {
			t.Errorf("annotation %q does not start with tentacular.io/enclave", c)
		}
	}
}

func TestAnnotationEnclave_NoSuffix(t *testing.T) {
	if AnnotationEnclave != "tentacular.io/enclave" {
		t.Errorf("AnnotationEnclave = %q, want %q", AnnotationEnclave, "tentacular.io/enclave")
	}
}

// --- ReadEnclaveInfo ---

func TestReadEnclaveInfo_FullAnnotations(t *testing.T) {
	ann := map[string]string{
		AnnotationEnclave:            "my-enclave",
		AnnotationEnclaveOwner:       "alice@example.com",
		AnnotationEnclaveOwnerSub:    "sub-abc123",
		AnnotationEnclaveMembers:     "bob@example.com,carol@example.com",
		AnnotationEnclavePlatform:    "slack",
		AnnotationEnclaveChannelID:   "C12345678",
		AnnotationEnclaveChannelName: "competitor-pricing",
		AnnotationEnclaveStatus:      "active",
		AnnotationEnclaveDefaultMode: "rwxrwx---",
		AnnotationEnclaveCreatedAt:   "2026-04-03T00:00:00Z",
		AnnotationEnclaveUpdatedAt:   "2026-04-03T12:00:00Z",
	}
	info := ReadEnclaveInfo(ann)

	if info.Enclave != "my-enclave" {
		t.Errorf("Enclave = %q, want %q", info.Enclave, "my-enclave")
	}
	if info.Owner != "alice@example.com" {
		t.Errorf("Owner = %q, want %q", info.Owner, "alice@example.com")
	}
	if info.OwnerSub != "sub-abc123" {
		t.Errorf("OwnerSub = %q, want %q", info.OwnerSub, "sub-abc123")
	}
	if len(info.Members) != 2 || info.Members[0] != "bob@example.com" || info.Members[1] != "carol@example.com" {
		t.Errorf("Members = %v, want [bob@example.com carol@example.com]", info.Members)
	}
	if info.Platform != "slack" {
		t.Errorf("Platform = %q, want %q", info.Platform, "slack")
	}
	if info.ChannelID != "C12345678" {
		t.Errorf("ChannelID = %q, want %q", info.ChannelID, "C12345678")
	}
	if info.ChannelName != "competitor-pricing" {
		t.Errorf("ChannelName = %q, want %q", info.ChannelName, "competitor-pricing")
	}
	if info.Status != "active" {
		t.Errorf("Status = %q, want %q", info.Status, "active")
	}
	if info.DefaultMode != "rwxrwx---" {
		t.Errorf("DefaultMode = %q, want %q", info.DefaultMode, "rwxrwx---")
	}
	if info.CreatedAt != "2026-04-03T00:00:00Z" {
		t.Errorf("CreatedAt = %q, want %q", info.CreatedAt, "2026-04-03T00:00:00Z")
	}
	if info.UpdatedAt != "2026-04-03T12:00:00Z" {
		t.Errorf("UpdatedAt = %q, want %q", info.UpdatedAt, "2026-04-03T12:00:00Z")
	}
}

func TestReadEnclaveInfo_OnlyRequired(t *testing.T) {
	ann := map[string]string{
		AnnotationEnclave: "my-enclave",
	}
	info := ReadEnclaveInfo(ann)

	if info.Enclave != "my-enclave" {
		t.Errorf("Enclave = %q, want %q", info.Enclave, "my-enclave")
	}
	if info.Owner != "" {
		t.Errorf("Owner = %q, want empty", info.Owner)
	}
	if info.OwnerSub != "" {
		t.Errorf("OwnerSub = %q, want empty", info.OwnerSub)
	}
	if len(info.Members) != 0 {
		t.Errorf("Members = %v, want empty slice", info.Members)
	}
	if info.Platform != "" {
		t.Errorf("Platform = %q, want empty", info.Platform)
	}
	if info.Status != "" {
		t.Errorf("Status = %q, want empty", info.Status)
	}
}

func TestReadEnclaveInfo_NonEnclave(t *testing.T) {
	ann := map[string]string{
		"tentacular.io/owner": "alice@example.com",
	}
	info := ReadEnclaveInfo(ann)

	if info.Enclave != "" {
		t.Errorf("Enclave = %q, want empty for non-enclave namespace", info.Enclave)
	}
}

// --- WriteEnclaveAnnotations ---

func TestWriteEnclaveAnnotations_RoundTrip(t *testing.T) {
	original := EnclaveInfo{
		Enclave:     "my-enclave",
		Owner:       "alice@example.com",
		OwnerSub:    "sub-abc123",
		Members:     []string{"bob@example.com", "carol@example.com"},
		Platform:    "slack",
		ChannelID:   "C12345678",
		ChannelName: "competitor-pricing",
		Status:      "active",
		DefaultMode: "rwxrwx---",
		CreatedAt:   "2026-04-03T00:00:00Z",
		UpdatedAt:   "2026-04-03T12:00:00Z",
	}

	annotations := WriteEnclaveAnnotations(original)
	roundTripped := ReadEnclaveInfo(annotations)

	if roundTripped.Enclave != original.Enclave {
		t.Errorf("Enclave round-trip: got %q, want %q", roundTripped.Enclave, original.Enclave)
	}
	if roundTripped.Owner != original.Owner {
		t.Errorf("Owner round-trip: got %q, want %q", roundTripped.Owner, original.Owner)
	}
	if roundTripped.OwnerSub != original.OwnerSub {
		t.Errorf("OwnerSub round-trip: got %q, want %q", roundTripped.OwnerSub, original.OwnerSub)
	}
	if len(roundTripped.Members) != len(original.Members) {
		t.Fatalf("Members round-trip: got %v, want %v", roundTripped.Members, original.Members)
	}
	for i, m := range original.Members {
		if roundTripped.Members[i] != m {
			t.Errorf("Members[%d] round-trip: got %q, want %q", i, roundTripped.Members[i], m)
		}
	}
	if roundTripped.Platform != original.Platform {
		t.Errorf("Platform round-trip: got %q, want %q", roundTripped.Platform, original.Platform)
	}
	if roundTripped.ChannelID != original.ChannelID {
		t.Errorf("ChannelID round-trip: got %q, want %q", roundTripped.ChannelID, original.ChannelID)
	}
	if roundTripped.Status != original.Status {
		t.Errorf("Status round-trip: got %q, want %q", roundTripped.Status, original.Status)
	}
	if roundTripped.DefaultMode != original.DefaultMode {
		t.Errorf("DefaultMode round-trip: got %q, want %q", roundTripped.DefaultMode, original.DefaultMode)
	}
	if roundTripped.CreatedAt != original.CreatedAt {
		t.Errorf("CreatedAt round-trip: got %q, want %q", roundTripped.CreatedAt, original.CreatedAt)
	}
	if roundTripped.UpdatedAt != original.UpdatedAt {
		t.Errorf("UpdatedAt round-trip: got %q, want %q", roundTripped.UpdatedAt, original.UpdatedAt)
	}
}

func TestWriteEnclaveAnnotations_EmptyMembers(t *testing.T) {
	info := EnclaveInfo{
		Enclave: "my-enclave",
		Members: []string{},
	}
	annotations := WriteEnclaveAnnotations(info)
	if annotations[AnnotationEnclaveMembers] != "" {
		t.Errorf("enclave-members annotation = %q, want empty string for no members", annotations[AnnotationEnclaveMembers])
	}
}

func TestWriteEnclaveAnnotations_NilMembers(t *testing.T) {
	info := EnclaveInfo{
		Enclave: "my-enclave",
		Members: nil,
	}
	annotations := WriteEnclaveAnnotations(info)
	if annotations[AnnotationEnclaveMembers] != "" {
		t.Errorf("enclave-members annotation = %q, want empty string for nil members", annotations[AnnotationEnclaveMembers])
	}
}

// --- ParseMembers ---

func TestParseMembers_CommaSeparated(t *testing.T) {
	result := ParseMembers("alice@co.com,bob@co.com")
	if len(result) != 2 || result[0] != "alice@co.com" || result[1] != "bob@co.com" {
		t.Errorf("ParseMembers = %v, want [alice@co.com bob@co.com]", result)
	}
}

func TestParseMembers_TrimsWhitespace(t *testing.T) {
	result := ParseMembers(" alice@co.com , bob@co.com ")
	if len(result) != 2 || result[0] != "alice@co.com" || result[1] != "bob@co.com" {
		t.Errorf("ParseMembers = %v, want [alice@co.com bob@co.com]", result)
	}
}

func TestParseMembers_EmptyString(t *testing.T) {
	result := ParseMembers("")
	if result == nil {
		t.Error("ParseMembers(\"\") returned nil, want empty non-nil slice")
	}
	if len(result) != 0 {
		t.Errorf("ParseMembers(\"\") = %v, want empty slice", result)
	}
}

func TestParseMembers_RemovesEmptyEntries(t *testing.T) {
	result := ParseMembers("alice@co.com,,bob@co.com,")
	if len(result) != 2 || result[0] != "alice@co.com" || result[1] != "bob@co.com" {
		t.Errorf("ParseMembers = %v, want [alice@co.com bob@co.com]", result)
	}
}

func TestParseMembers_SingleEntry(t *testing.T) {
	result := ParseMembers("alice@co.com")
	if len(result) != 1 || result[0] != "alice@co.com" {
		t.Errorf("ParseMembers = %v, want [alice@co.com]", result)
	}
}

func TestParseMembers_NormalizesToLowercase(t *testing.T) {
	result := ParseMembers("Alice@Example.COM, BOB@EXAMPLE.COM")
	if len(result) != 2 {
		t.Fatalf("ParseMembers = %v, want 2 entries", result)
	}
	if result[0] != "alice@example.com" {
		t.Errorf("ParseMembers[0] = %q, want alice@example.com", result[0])
	}
	if result[1] != "bob@example.com" {
		t.Errorf("ParseMembers[1] = %q, want bob@example.com", result[1])
	}
}

// --- FormatMembers ---

func TestFormatMembers_TwoEntries(t *testing.T) {
	result := FormatMembers([]string{"alice@co.com", "bob@co.com"})
	if result != "alice@co.com,bob@co.com" {
		t.Errorf("FormatMembers = %q, want %q", result, "alice@co.com,bob@co.com")
	}
}

func TestFormatMembers_Empty(t *testing.T) {
	result := FormatMembers([]string{})
	if result != "" {
		t.Errorf("FormatMembers([]) = %q, want empty string", result)
	}
}

func TestFormatMembers_Nil(t *testing.T) {
	result := FormatMembers(nil)
	if result != "" {
		t.Errorf("FormatMembers(nil) = %q, want empty string", result)
	}
}

func TestFormatMembers_SingleEntry(t *testing.T) {
	result := FormatMembers([]string{"alice@co.com"})
	if result != "alice@co.com" {
		t.Errorf("FormatMembers = %q, want %q", result, "alice@co.com")
	}
}

// --- ValidateMembers ---

func TestValidateMembers_UnderLimit(t *testing.T) {
	members := make([]string, MaxEnclaveMembers-1)
	for i := range members {
		members[i] = "user@example.com"
	}
	if err := ValidateMembers(members); err != nil {
		t.Errorf("ValidateMembers under limit: unexpected error: %v", err)
	}
}

func TestValidateMembers_AtLimit(t *testing.T) {
	members := make([]string, MaxEnclaveMembers)
	for i := range members {
		members[i] = "user@example.com"
	}
	if err := ValidateMembers(members); err != nil {
		t.Errorf("ValidateMembers at limit: unexpected error: %v", err)
	}
}

func TestValidateMembers_OverLimit(t *testing.T) {
	members := make([]string, MaxEnclaveMembers+1)
	for i := range members {
		members[i] = "user@example.com"
	}
	if err := ValidateMembers(members); err == nil {
		t.Error("ValidateMembers over limit: expected error, got nil")
	}
}

func TestValidateMembers_Empty(t *testing.T) {
	if err := ValidateMembers([]string{}); err != nil {
		t.Errorf("ValidateMembers empty: unexpected error: %v", err)
	}
}

// --- IsEnclave ---

func TestIsEnclave_WithAnnotation(t *testing.T) {
	ann := map[string]string{
		AnnotationEnclave: "my-enclave",
	}
	if !IsEnclave(ann) {
		t.Error("IsEnclave with enclave annotation: expected true, got false")
	}
}

func TestIsEnclave_WithoutAnnotation(t *testing.T) {
	ann := map[string]string{
		"tentacular.io/owner": "alice@example.com",
	}
	if IsEnclave(ann) {
		t.Error("IsEnclave without enclave annotation: expected false, got true")
	}
}

func TestIsEnclave_EmptyAnnotation(t *testing.T) {
	ann := map[string]string{
		AnnotationEnclave: "",
	}
	if IsEnclave(ann) {
		t.Error("IsEnclave with empty enclave annotation: expected false, got true")
	}
}

func TestIsEnclave_EmptyMap(t *testing.T) {
	if IsEnclave(map[string]string{}) {
		t.Error("IsEnclave with empty map: expected false, got true")
	}
}

func TestIsEnclave_NilMap(t *testing.T) {
	if IsEnclave(nil) {
		t.Error("IsEnclave with nil map: expected false, got true")
	}
}

// --- MaxEnclaveMembers ---

func TestMaxEnclaveMembers_DefaultValue(t *testing.T) {
	if MaxEnclaveMembers != 100 {
		t.Errorf("MaxEnclaveMembers = %d, want 100", MaxEnclaveMembers)
	}
}

// --- ValidateEnclaveName ---

func TestValidateEnclaveName_Valid(t *testing.T) {
	validNames := []string{
		"ab",
		"my-enclave",
		"marketing-workflows",
		"a",
		"a1",
		"abc123",
		strings.Repeat("a", 63), // exactly 63 chars (single char repeated matches [a-z0-9]([a-z0-9-]{0,61}[a-z0-9])? only if len>=2 or single char)
	}
	// Single char "a" matches ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ because the group is optional
	for _, name := range validNames {
		if err := ValidateEnclaveName(name); err != nil {
			t.Errorf("ValidateEnclaveName(%q) unexpected error: %v", name, err)
		}
	}
}

func TestValidateEnclaveName_Empty(t *testing.T) {
	if err := ValidateEnclaveName(""); err == nil {
		t.Error("ValidateEnclaveName(\"\") expected error, got nil")
	}
}

func TestValidateEnclaveName_UpperCase(t *testing.T) {
	if err := ValidateEnclaveName("MyEnclave"); err == nil {
		t.Error("ValidateEnclaveName(\"MyEnclave\") expected error for uppercase, got nil")
	}
}

func TestValidateEnclaveName_Underscore(t *testing.T) {
	if err := ValidateEnclaveName("my_enclave"); err == nil {
		t.Error("ValidateEnclaveName(\"my_enclave\") expected error for underscore, got nil")
	}
}

func TestValidateEnclaveName_StartsWithHyphen(t *testing.T) {
	if err := ValidateEnclaveName("-myenclave"); err == nil {
		t.Error("ValidateEnclaveName(\"-myenclave\") expected error for leading hyphen, got nil")
	}
}

func TestValidateEnclaveName_EndsWithHyphen(t *testing.T) {
	if err := ValidateEnclaveName("myenclave-"); err == nil {
		t.Error("ValidateEnclaveName(\"myenclave-\") expected error for trailing hyphen, got nil")
	}
}

func TestValidateEnclaveName_TooLong(t *testing.T) {
	name := strings.Repeat("a", 64)
	if err := ValidateEnclaveName(name); err == nil {
		t.Errorf("ValidateEnclaveName(%q) expected error for >63 chars, got nil", name)
	}
}

func TestValidateEnclaveName_DotRejected(t *testing.T) {
	if err := ValidateEnclaveName("my.enclave"); err == nil {
		t.Error("ValidateEnclaveName(\"my.enclave\") expected error for '.', got nil")
	}
}

func TestValidateEnclaveName_DoubleDotRejected(t *testing.T) {
	if err := ValidateEnclaveName(".."); err == nil {
		t.Error("ValidateEnclaveName(\"..\") expected error for '..', got nil")
	}
}

// --- ValidateEnclaveInfo ---

func TestValidateEnclaveInfo_Valid(t *testing.T) {
	info := EnclaveInfo{
		Enclave:     "my-enclave",
		Owner:       "alice@example.com",
		Members:     []string{"bob@example.com"},
		Platform:    "slack",
		Status:      "active",
		DefaultMode: "rwxr-x---",
	}
	if err := ValidateEnclaveInfo(info); err != nil {
		t.Errorf("ValidateEnclaveInfo valid info: unexpected error: %v", err)
	}
}

func TestValidateEnclaveInfo_EmptyOptionalFields(t *testing.T) {
	// Platform and Status empty = backwards compat allowed
	info := EnclaveInfo{
		Enclave: "my-enclave",
		Owner:   "alice@example.com",
	}
	if err := ValidateEnclaveInfo(info); err != nil {
		t.Errorf("ValidateEnclaveInfo with empty optional fields: unexpected error: %v", err)
	}
}

func TestValidateEnclaveInfo_InvalidEnclaveName(t *testing.T) {
	info := EnclaveInfo{
		Enclave: "My_Enclave",
		Owner:   "alice@example.com",
	}
	if err := ValidateEnclaveInfo(info); err == nil {
		t.Error("ValidateEnclaveInfo with invalid enclave name: expected error, got nil")
	}
}

func TestValidateEnclaveInfo_EmptyOwner(t *testing.T) {
	info := EnclaveInfo{
		Enclave: "my-enclave",
		Owner:   "",
	}
	if err := ValidateEnclaveInfo(info); err == nil {
		t.Error("ValidateEnclaveInfo with empty owner: expected error, got nil")
	}
}

func TestValidateEnclaveInfo_OwnerMissingAt(t *testing.T) {
	info := EnclaveInfo{
		Enclave: "my-enclave",
		Owner:   "alice-no-at-sign",
	}
	if err := ValidateEnclaveInfo(info); err == nil {
		t.Error("ValidateEnclaveInfo with owner missing '@': expected error, got nil")
	}
}

func TestValidateEnclaveInfo_MemberMissingAt(t *testing.T) {
	info := EnclaveInfo{
		Enclave: "my-enclave",
		Owner:   "alice@example.com",
		Members: []string{"bob-no-at"},
	}
	if err := ValidateEnclaveInfo(info); err == nil {
		t.Error("ValidateEnclaveInfo with member missing '@': expected error, got nil")
	}
}

func TestValidateEnclaveInfo_EmptyMember(t *testing.T) {
	info := EnclaveInfo{
		Enclave: "my-enclave",
		Owner:   "alice@example.com",
		Members: []string{""},
	}
	if err := ValidateEnclaveInfo(info); err == nil {
		t.Error("ValidateEnclaveInfo with empty member: expected error, got nil")
	}
}

func TestValidateEnclaveInfo_InvalidPlatform(t *testing.T) {
	info := EnclaveInfo{
		Enclave:  "my-enclave",
		Owner:    "alice@example.com",
		Platform: "teams",
	}
	if err := ValidateEnclaveInfo(info); err == nil {
		t.Error("ValidateEnclaveInfo with invalid platform: expected error, got nil")
	}
}

func TestValidateEnclaveInfo_InvalidStatus(t *testing.T) {
	info := EnclaveInfo{
		Enclave: "my-enclave",
		Owner:   "alice@example.com",
		Status:  "deleted",
	}
	if err := ValidateEnclaveInfo(info); err == nil {
		t.Error("ValidateEnclaveInfo with invalid status: expected error, got nil")
	}
}

func TestValidateEnclaveInfo_ValidStatuses(t *testing.T) {
	for _, status := range []string{"active", "provisioning", "frozen", ""} {
		info := EnclaveInfo{
			Enclave: "my-enclave",
			Owner:   "alice@example.com",
			Status:  status,
		}
		if err := ValidateEnclaveInfo(info); err != nil {
			t.Errorf("ValidateEnclaveInfo with status %q: unexpected error: %v", status, err)
		}
	}
}

func TestValidateEnclaveInfo_InvalidDefaultMode(t *testing.T) {
	info := EnclaveInfo{
		Enclave:     "my-enclave",
		Owner:       "alice@example.com",
		DefaultMode: "rwxrwxrw", // only 8 chars
	}
	if err := ValidateEnclaveInfo(info); err == nil {
		t.Error("ValidateEnclaveInfo with invalid default-mode: expected error, got nil")
	}
}

func TestValidateEnclaveInfo_InvalidDefaultModeChars(t *testing.T) {
	info := EnclaveInfo{
		Enclave:     "my-enclave",
		Owner:       "alice@example.com",
		DefaultMode: "rwxrwxrwZ", // invalid char Z
	}
	if err := ValidateEnclaveInfo(info); err == nil {
		t.Error("ValidateEnclaveInfo with invalid default-mode char: expected error, got nil")
	}
}

func TestValidateEnclaveInfo_EmptyDefaultMode(t *testing.T) {
	info := EnclaveInfo{
		Enclave:     "my-enclave",
		Owner:       "alice@example.com",
		DefaultMode: "",
	}
	if err := ValidateEnclaveInfo(info); err != nil {
		t.Errorf("ValidateEnclaveInfo with empty default-mode: unexpected error: %v", err)
	}
}

func TestValidateEnclaveInfo_MemberCountExceedsMax(t *testing.T) {
	members := make([]string, MaxEnclaveMembers+1)
	for i := range members {
		members[i] = "user@example.com"
	}
	info := EnclaveInfo{
		Enclave: "my-enclave",
		Owner:   "alice@example.com",
		Members: members,
	}
	if err := ValidateEnclaveInfo(info); err == nil {
		t.Error("ValidateEnclaveInfo over member limit: expected error, got nil")
	}
}
