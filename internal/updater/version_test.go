package updater

import "testing"

func TestParseTag(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		version string
		build   string
		ok      bool
	}{
		{name: "leading v", tag: "v0.1.0-R130726", version: "0.1.0", build: "R130726", ok: true},
		{name: "without v", tag: "0.1.0-R130726", version: "0.1.0", build: "R130726", ok: true},
		{name: "build only", tag: "R130726"},
		{name: "version only", tag: "v0.1.0"},
		{name: "prerelease", tag: "v0.1.0-rc1"},
		{name: "invalid date", tag: "v0.1.0-R321326"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			version, build, ok := ParseTag(test.tag)
			if ok != test.ok || version != test.version || build != test.build {
				t.Fatalf("ParseTag(%q) = %q, %q, %v", test.tag, version, build, ok)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name             string
		aversion, abuild string
		bversion, bbuild string
		want             int
	}{
		{name: "patch", aversion: "1.2.4", abuild: "R010126", bversion: "1.2.3", bbuild: "R311226", want: 1},
		{name: "historical adjacent", aversion: "0.1.0", abuild: "R130726", bversion: "0.1.0", bbuild: "R140726", want: -1},
		{name: "month ordering", aversion: "0.1.0", abuild: "R100926", bversion: "0.1.0", bbuild: "R140726", want: 1},
		{name: "year boundary", aversion: "0.1.0", abuild: "R311225", bversion: "0.1.0", bbuild: "R010126", want: -1},
		{name: "equal", aversion: "2.0.0", abuild: "R281026", bversion: "2.0.0", bbuild: "R281026", want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Compare(test.aversion, test.abuild, test.bversion, test.bbuild); got != test.want {
				t.Fatalf("Compare() = %d, want %d", got, test.want)
			}
		})
	}
}
