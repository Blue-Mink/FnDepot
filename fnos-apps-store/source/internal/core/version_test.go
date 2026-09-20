package core

import "testing"

// The pairs below are real values measured on a live fnOS box (1.2.0505) with
// 61 installed apps. CompareFpkVersions decides whether EVERY app shows an
// update badge, so its behavior is pinned here rather than left to inspection.
func TestCompareFpkVersions(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		// Revision-only bumps: the common case. Same upstream release,
		// re-packaged. Must read as "update available".
		{"revision bump headscale", "0.29.3-r3", "0.29.3-r4", -1},
		{"revision bump ezbookkeeping", "1.6.1-r4", "1.6.1-r5", -1},
		{"revision bump gopeed", "1.9.3-r3", "1.9.3-r6", -1},
		{"revision bump 1panel", "1.10.34-lts-r7", "1.10.34-lts-r8", -1},

		// Identical: no badge.
		{"identical with revision", "4.2.5-r3", "4.2.5-r3", 0},
		{"identical without revision", "0.18.8", "0.18.8", 0},
		{"identical lts tag", "1.10.34-lts-r8", "1.10.34-lts-r8", 0},

		// Upstream version moved.
		{"upstream bump prometheus", "3.13.3", "3.14.0", -1},
		{"upstream bump beszel", "0.18.7-r1", "0.18.8", -1},

		// Installed is ahead of the catalog: never offer a downgrade.
		{"catalog behind installed", "0.18.8", "0.18.7-r1", 1},
		{"catalog behind installed alist", "3.63.1", "3.63.0-r3", 1},

		// A revision suffix appearing or disappearing is still a change, and
		// with equal bases the newer package is the catalog's.
		{"revision added", "1.7.71", "1.7.71-r6", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CompareFpkVersions(tt.a, tt.b); got != tt.want {
				t.Errorf("CompareFpkVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"equal", "1.9.3", "1.9.3", 0},
		{"less", "3.13.3", "3.14.0", -1},
		{"greater", "0.29.7", "0.29.3", 1},
		{"greater ezbookkeeping", "1.6.3", "1.6.1", 1},

		// Missing trailing components count as zero, so an extra ".0" is
		// not a version change but an extra ".1" is.
		{"missing parts are zero", "1.2", "1.2.0", 0},
		{"extra nonzero part wins", "10.11.11.1", "10.11.11", 1},

		// Non-numeric suffixes are truncated to their leading digits.
		{"lts suffix equal", "1.10.34-lts", "1.10.34-lts", 0},
		{"date-style version", "2026.07.2", "2026.07.2", 0},

		{"empty both", "", "", 0},
		{"empty is zero", "", "0.0.0", 0},
		{"whitespace tolerated", " 1.2.3 ", "1.2.3", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CompareVersions(tt.a, tt.b); got != tt.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// Known limitation, pinned so a future change to it is a deliberate decision:
// only the leading digits of a component are compared, so differing
// non-numeric suffixes compare equal.
func TestCompareVersionsIgnoresNonNumericSuffix(t *testing.T) {
	if got := CompareVersions("1.10.34-lts", "1.10.34-beta"); got != 0 {
		t.Errorf("CompareVersions with differing suffixes = %d, want 0 (documented limitation)", got)
	}
}
