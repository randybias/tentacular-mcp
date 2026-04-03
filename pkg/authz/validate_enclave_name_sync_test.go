package authz_test

import (
	"testing"

	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
)

// TestValidateEnclaveName_AuthzExoSync verifies that authz.ValidateEnclaveName
// and exoskeleton.ValidateEnclaveName agree on all test inputs. These two
// implementations are intentionally duplicated to avoid a circular import
// (authz -> exoskeleton for DeployerInfo). This test ensures they stay in sync.
func TestValidateEnclaveName_AuthzExoSync(t *testing.T) {
	names := []string{
		// Valid names
		"my-enclave",
		"a",
		"abc123",
		"a-b-c",
		"a1",
		// Invalid names
		"",
		"-leading-hyphen",
		"trailing-hyphen-",
		"UPPERCASE",
		"has.dot",
		"has space",
		"a_underscore",
		"..",
		"way-too-long-name-that-exceeds-the-sixty-three-character-limit-for-dns-labels-abcdefgh",
	}

	for _, name := range names {
		authzErr := authz.ValidateEnclaveName(name)
		exoErr := exoskeleton.ValidateEnclaveName(name)

		authzOK := authzErr == nil
		exoOK := exoErr == nil

		if authzOK != exoOK {
			t.Errorf("ValidateEnclaveName(%q): authz=%v (err=%v), exoskeleton=%v (err=%v)",
				name, authzOK, authzErr, exoOK, exoErr)
		}
	}
}
