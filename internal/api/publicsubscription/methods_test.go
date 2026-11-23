package publicsubscription

import "testing"

func TestProfileFilenameUsesServerName(t *testing.T) {
	for _, test := range []struct {
		name string
		ext  string
		want string
	}{
		{"Нидерланды", "conf", "Нидерланды.conf"},
		{"Frankfurt/01", "vpn", "Frankfurt-01.vpn"},
		{"../", "conf", "AmneziaWG.conf"},
	} {
		if got := profileFilename(test.name, test.ext); got != test.want {
			t.Fatalf("profileFilename(%q, %q) = %q, want %q", test.name, test.ext, got, test.want)
		}
	}
}
