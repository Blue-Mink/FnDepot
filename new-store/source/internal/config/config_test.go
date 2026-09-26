package config

import "testing"

// TestDockerFallbackPrefixes locks the mirror fallback chain used when a
// Docker image pull is denied by the selected mirror (conversun/fnos-apps
// #267, #266, #257, #248): the selected source goes first, every other real
// mirror follows in declaration order, and a direct pull ("") is always
// present exactly once — last for mirror selections, first for direct.
func TestDockerFallbackPrefixes(t *testing.T) {
	// realMirrors mirrors the dockerMirrors declaration order, excluding the
	// custom/direct pseudo-entries.
	realMirrors := []string{
		"m.daocloud.io/",
		"docker.1ms.run/",
		"docker.m.daocloud.io/",
		"hub.rat.dev/",
		"docker.1panel.live/",
		"dockerproxy.net/",
		"registry.cyou/",
	}

	// chainOf builds the expected chain for a mirror selection: selected
	// first, the other real mirrors in declaration order, direct last.
	chainOf := func(selected string) []string {
		chain := []string{selected}
		for _, m := range realMirrors {
			if m != selected {
				chain = append(chain, m)
			}
		}
		return append(chain, "")
	}

	cases := []struct {
		name string
		key  string
		cfg  Config
		want []string
	}{
		{
			name: "daocloud selection starts with m.daocloud.io and ends direct",
			key:  "daocloud",
			cfg:  Config{DockerMirror: "daocloud"},
			want: chainOf("m.daocloud.io/"),
		},
		{
			name: "docker-1ms selection starts with docker.1ms.run",
			key:  "docker-1ms",
			cfg:  Config{DockerMirror: "docker-1ms"},
			want: chainOf("docker.1ms.run/"),
		},
		{
			name: "custom prefix first, then real mirrors, then direct",
			key:  "custom",
			cfg:  Config{DockerMirror: "custom", CustomDockerMirror: "mirror.example.com/"},
			want: chainOf("mirror.example.com/"),
		},
		{
			name: "direct selection keeps the real mirrors as fallbacks",
			key:  "direct",
			cfg:  Config{DockerMirror: "direct"},
			want: append([]string{""}, realMirrors...),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DockerFallbackPrefixes(tc.key, tc.cfg)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d: %v", len(got), len(tc.want), got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("chain[%d] = %q, want %q (full chain: %v)", i, got[i], tc.want[i], got)
				}
			}

			// No prefix may appear twice in any chain.
			seen := map[string]int{}
			for _, p := range got {
				seen[p]++
			}
			for p, n := range seen {
				if n != 1 {
					t.Errorf("prefix %q appears %d times in %v", p, n, got)
				}
			}
			// The direct pull is always present exactly once...
			if seen[""] != 1 {
				t.Errorf("direct pull appears %d times in %v, want exactly 1", seen[""], got)
			}
			// ...and for non-direct selections it is the LAST resort.
			if tc.key != "direct" && got[len(got)-1] != "" {
				t.Errorf("direct pull is not last in %v", got)
			}
		})
	}
}
