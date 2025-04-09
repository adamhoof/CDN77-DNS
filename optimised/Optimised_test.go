package optimised

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Helpers
func mustParseCIDR(t *testing.T, cidr string) *net.IPNet {
	t.Helper()
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("mustParseCIDR failed for '%s': %v", cidr, err)
	}
	if ipNet.IP.To4() != nil || len(ipNet.IP) != net.IPv6len {
		if ipNet.IP.To4() == nil { // Is IPv6 but maybe not 16 bytes? Unlikely.
			t.Logf("Warning: Parsed IP for '%s' is IPv6 but not 16 bytes? IP: %v", cidr, ipNet.IP)
		}
	}
	return ipNet
}

// checkRoute performs a lookup and asserts the expected result.
func checkRoute(t *testing.T, data *Data, ecsCIDR string, wantPop uint16, wantScope int) {
	t.Helper()
	ecsNet := mustParseCIDR(t, ecsCIDR)
	gotPop, gotScope := data.Route(ecsNet)
	if gotPop != wantPop || gotScope != wantScope {
		t.Errorf("Route(%s): got pop %d, scope %d; want pop %d, scope %d",
			ecsCIDR, gotPop, gotScope, wantPop, wantScope)
	}
}

// checkInsert performs an insertion and asserts whether an error is expected.
func checkInsert(t *testing.T, data *Data, cidr string, popID uint16, wantErrSubstring string) {
	t.Helper()
	ipNet := mustParseCIDR(t, cidr)
	err := data.insert(ipNet, popID) // Use lowercase insert as defined

	if wantErrSubstring == "" {
		if err != nil {
			t.Errorf("insert(%s, %d): unexpected error: %v", cidr, popID, err)
		}
	} else {
		if err == nil {
			t.Errorf("insert(%s, %d): expected error containing '%s', but got nil", cidr, popID, wantErrSubstring)
		} else if !strings.Contains(err.Error(), wantErrSubstring) {
			t.Errorf("insert(%s, %d): expected error containing '%s', but got: %v", cidr, popID, wantErrSubstring, err)
		}
	}
}

// Tests
func TestNewData(t *testing.T) {
	data := NewData()
	if data == nil {
		t.Fatal("NewData() returned nil")
	}
	if data.root == nil {
		t.Fatal("NewData().root is nil")
	}
	if data.root.ruleInfo != nil || data.root.children[0] != nil || data.root.children[1] != nil {
		t.Error("NewData().root should be empty")
	}
}

func TestGetBit(t *testing.T) {
	ip := net.ParseIP("2001:db8::1")
	ip = ip.To16()

	tests := []struct {
		n    uint8
		want uint8
		err  bool
	}{
		// analyze first 4 bits (MSB order)
		{0, 0, false}, // First bit of '2' (0010)
		{1, 0, false}, // Second bit of '2'
		{2, 1, false}, // Third bit of '2'
		{3, 0, false}, // Fourth bit of '2'

		// literary edge cases
		{15, 1, false},  // last bit of 1st byte
		{7, 0, false},   // last bit of 0th byte
		{16, 0, false},  // first bit of second byte
		{127, 1, false}, // last bit of the IP (the final '1')
		{128, 0, true},  // out of bounds
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("Bit%d", tc.n), func(t *testing.T) {
			got, err := getBit(ip, tc.n)
			if tc.err {
				if err == nil {
					t.Errorf("getBit(ip, %d): expected error, got nil", tc.n)
				}
			} else {
				if err != nil {
					t.Errorf("getBit(ip, %d): unexpected error: %v", tc.n, err)
				}
				if got != tc.want {
					t.Errorf("getBit(ip, %d): got %d, want %d", tc.n, got, tc.want)
				}
			}
		})
	}
}

func TestValidateSubnet(t *testing.T) {
	t.Run("NilSubnet", func(t *testing.T) {
		err := validateSubnet(nil)
		if err == nil {
			t.Error("Expected error for nil subnet, got nil")
		}
	})

	t.Run("ValidIPv6", func(t *testing.T) {
		_, ipn, _ := net.ParseCIDR("2001:db8::/48")
		err := validateSubnet(ipn)
		if err != nil {
			t.Errorf("Unexpected error for valid IPv6 subnet: %v", err)
		}
	})

	t.Run("IPv4Mask", func(t *testing.T) {
		_, ipn, _ := net.ParseCIDR("192.168.1.0/24")
		err := validateSubnet(ipn)
		if err == nil {
			t.Error("Expected error for IPv4 mask, got nil")
		} else if !strings.Contains(err.Error(), "expected IPv6 subnet mask") {
			t.Errorf("Expected IPv6 mask error, got: %v", err)
		}
	})

	t.Run("InvalidIP", func(t *testing.T) {
		// Create an IPNet with a nil IP but valid mask
		mask := net.CIDRMask(48, 128)
		ipn := &net.IPNet{IP: nil, Mask: mask}
		err := validateSubnet(ipn)
		if err == nil {
			t.Error("Expected error for invalid (nil) IP, got nil")
		} else if !strings.Contains(err.Error(), "invalid IP address") {
			t.Errorf("Expected invalid IP error, got: %v", err)
		}
	})
}

func TestInsertAndRoute(t *testing.T) {
	data := NewData()
	// 1. Basic Insert and Exact Match
	t.Run("BasicInsertAndRoute", func(t *testing.T) {
		data := NewData()
		checkInsert(t, data, "2001:db8:aaaa::/48", 101, "")
		checkRoute(t, data, "2001:db8:aaaa:1::1/64", 101, 48)
		checkRoute(t, data, "2001:db8:bbbb::/48", 0, -1)
		checkRoute(t, data, "2001:db8::/32", 0, -1)
	})

	// 2. LPM - Overlap with SAME PoP ID
	t.Run("LPMOverlapSamePoP", func(t *testing.T) {
		data := NewData()
		const testPop uint16 = 150
		checkInsert(t, data, "2001:db8:aaaa::/48", testPop, "")
		checkInsert(t, data, "2001:db8:aaaa:bb00::/56", testPop, "")
		checkInsert(t, data, "2001:db8::/32", testPop, "")

		// Verify Route chooses most specific
		checkRoute(t, data, "2001:db8:aaaa:bb00:1::/64", testPop, 56)
		checkRoute(t, data, "2001:db8:aaaa:cc00::/56", testPop, 48)
		checkRoute(t, data, "2001:db8:bbbb::1/64", testPop, 32)
	})

	// 3. Default Route (::/0) - Ensure non-conflicting insert
	t.Run("DefaultRoute", func(t *testing.T) {
		data = NewData()
		const testPopID uint16 = 999

		checkInsert(t, data, "::/0", testPopID, "")
		checkInsert(t, data, "2001:db8::/32", testPopID, "")

		checkRoute(t, data, "2001:db8:1::1/64", testPopID, 32)
		checkRoute(t, data, "2002::/16", testPopID, 0)
		checkRoute(t, data, "::1/128", testPopID, 0)
	})

	// 4. No Match
	t.Run("NoMatch", func(t *testing.T) {
		data = NewData()
		checkInsert(t, data, "2001:db8::/32", 100, "")
		checkRoute(t, data, "2002::/16", 0, -1) // No default, no match
	})

	// 5. /128 Route - Ensure non-conflicting insert
	t.Run("SpecificHostRoute", func(t *testing.T) {
		data = NewData()
		const hostPop uint16 = 128
		const broadPop uint16 = 128

		checkInsert(t, data, "2001:db8::1/128", hostPop, "")
		checkInsert(t, data, "2001:db8::/32", broadPop, "")

		checkRoute(t, data, "2001:db8::1/128", hostPop, 128)
		checkRoute(t, data, "2001:db8::2/128", broadPop, 32)
	})
}
func TestInsertConflicts(t *testing.T) {
	t.Run("AncestorConflict", func(t *testing.T) {
		data := NewData()
		checkInsert(t, data, "2001:db8::/32", 100, "") // Broader first
		// Narrower with DIFF PoP -> FAIL
		checkInsert(t, data, "2001:db8:aaaa::/48", 200, "conflicts with broader rule at scope /32 (PoP 100)")
		// Check that only broader rule exists
		checkRoute(t, data, "2001:db8:aaaa::1/64", 100, 32)
		checkRoute(t, data, "2001:db8:bbbb::1/64", 100, 32)
	})

	t.Run("AncestorOKSamePoP", func(t *testing.T) {
		data := NewData()
		checkInsert(t, data, "2001:db8::/32", 100, "")
		// Narrower with SAME PoP -> OK
		checkInsert(t, data, "2001:db8:aaaa::/48", 100, "")
		// Check LPM works
		checkRoute(t, data, "2001:db8:aaaa::1/64", 100, 48)
		checkRoute(t, data, "2001:db8:bbbb::1/64", 100, 32)
	})

	// Exact Conflicts
	t.Run("ExactConflict", func(t *testing.T) {
		data := NewData()
		checkInsert(t, data, "2001:db8:aaaa::/48", 100, "")
		// Same prefix, different PoP -> FAIL
		checkInsert(t, data, "2001:db8:aaaa::/48", 200, "rule for exact prefix 2001:db8:aaaa::/48 exists with different PoP 100")
		// Check original rule exists
		checkRoute(t, data, "2001:db8:aaaa::1/64", 100, 48)
	})

	t.Run("ExactOKSamePoP", func(t *testing.T) {
		data := NewData()
		checkInsert(t, data, "2001:db8:aaaa::/48", 100, "")
		// Same prefix, SAME PoP -> OK (overwrites)
		checkInsert(t, data, "2001:db8:aaaa::/48", 100, "")
		// Check rule exists
		checkRoute(t, data, "2001:db8:aaaa::1/64", 100, 48)
	})

	// Descendant Conflicts
	t.Run("DescendantConflict", func(t *testing.T) {
		data := NewData()
		checkInsert(t, data, "2001:db8:aaaa::/48", 200, "") // Insert narrower first
		// Broader with different PoP -> FAIL
		checkInsert(t, data, "2001:db8::/32", 100, "conflicts with existing narrower rule")
		// Only narrower rule exists
		checkRoute(t, data, "2001:db8:aaaa::1/64", 200, 48)
		checkRoute(t, data, "2001:db8:bbbb::1/64", 0, -1) // No broader rule was added
	})

	t.Run("DescendantOKSamePoP", func(t *testing.T) {
		data := NewData()
		checkInsert(t, data, "2001:db8:aaaa::/48", 100, "") // Insert narrower first
		// Broader with SAME PoP -> OK
		checkInsert(t, data, "2001:db8::/32", 100, "")

		checkRoute(t, data, "2001:db8:aaaa::1/64", 100, 48)
		checkRoute(t, data, "2001:db8:bbbb::1/64", 100, 32)
	})
}

func TestLoadRoutingData(t *testing.T) {
	createTempFile := func(content string) string {
		t.Helper()
		dir := t.TempDir()
		filePath := filepath.Join(dir, "routing.txt")
		err := os.WriteFile(filePath, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create temp file: %v", err)
		}
		return filePath
	}

	t.Run("ValidFile", func(t *testing.T) {
		content := `
2001:db8:aaaa::/48 101
2001:db8:aaaa::/56 101
`
		filePath := createTempFile(content)
		data := NewData()
		err := data.LoadRoutingData(filePath)
		if err != nil {
			t.Fatalf("LoadRoutingData failed: %v", err)
		}
		// Verify loaded data with Route checks
		checkRoute(t, data, "2001:db8:aaaa::1/64", 101, 56)
		checkRoute(t, data, "2001:db8:aaaa:cc00::/56", 101, 48)
		checkRoute(t, data, "2001::/16", 0, -1)
	})

	t.Run("InvalidCIDR", func(t *testing.T) {
		content := "2001:db8:xyz::/48 100"
		filePath := createTempFile(content)
		data := NewData()
		err := data.LoadRoutingData(filePath)
		if err == nil {
			t.Error("Expected error for invalid CIDR, got nil")
		} else if !strings.Contains(err.Error(), "failed to parse CIDR") {
			t.Errorf("Expected CIDR parse error, got: %v", err)
		}
	})

	t.Run("InvalidPoPID", func(t *testing.T) {
		content := "2001:db8::/48 abc"
		filePath := createTempFile(content)
		data := NewData()
		err := data.LoadRoutingData(filePath)
		if err == nil {
			t.Error("Expected error for invalid PoP ID, got nil")
		} else if !strings.Contains(err.Error(), "failed to parse PoP ID") {
			t.Errorf("Expected PoP ID parse error, got: %v", err)
		}
	})

	t.Run("WrongFieldCount", func(t *testing.T) {
		content := "2001:db8::/48 100 extra"
		filePath := createTempFile(content)
		data := NewData()
		err := data.LoadRoutingData(filePath)
		if err == nil {
			t.Error("Expected error for wrong field count, got nil")
		} else if !strings.Contains(err.Error(), "expected 2 fields") {
			t.Errorf("Expected field count error, got: %v", err)
		}
	})

	t.Run("IPv4Rule", func(t *testing.T) {
		content := "192.168.1.0/24 100"
		filePath := createTempFile(content)
		data := NewData()
		err := data.LoadRoutingData(filePath)
		if err == nil {
			t.Error("Expected error for IPv4 rule, got nil")
		} else if !strings.Contains(err.Error(), "expected IPv6 subnet mask") {
			// Error comes from validateSubnet called by insert
			t.Errorf("Expected IPv6 mask error, got: %v", err)
		}
	})

	t.Run("FileWithConflict", func(t *testing.T) {
		content := `
2001:db8::/32 100
2001:db8:aaaa::/48 200
`
		filePath := createTempFile(content)
		data := NewData()
		err := data.LoadRoutingData(filePath)
		if err == nil {
			t.Error("Expected error due to conflicting rules, got nil")
		} else if !strings.Contains(err.Error(), "conflicts with broader rule") {
			t.Errorf("Expected ancestor conflict error, got: %v", err)
		}
	})
}
