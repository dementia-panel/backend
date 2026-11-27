package vpn

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestAmneziaLinkIncludesIOSRequiredAWGSettings(t *testing.T) {
	profile := ClientProfile{Name: "Test", IP: "192.0.2.1", Port: 51820, Address: "10.70.0.2/32", HeaderKey: "test-header-key"}
	link, err := profile.AmneziaLink()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(link, "vpn://"))
	if err != nil || len(payload) < 4 {
		t.Fatalf("invalid Qt payload: %v", err)
	}
	r, err := zlib.NewReader(bytes.NewReader(payload[4:]))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if int(binary.BigEndian.Uint32(payload[:4])) != len(data) {
		t.Fatal("Qt payload length differs from uncompressed JSON length")
	}
	var guest struct {
		Containers []struct {
			AWG struct {
				LastConfig string `json:"last_config"`
			} `json:"awg"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(data, &guest); err != nil || len(guest.Containers) != 1 {
		t.Fatalf("invalid guest config: %v", err)
	}
	var client map[string]any
	if err := json.Unmarshal([]byte(guest.Containers[0].AWG.LastConfig), &client); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"Jc": "0", "Jmin": "0", "Jmax": "0", "S1": "32", "S2": "48", "S3": "16", "S4": "24", "H1": "14581001", "H2": "14581002", "H3": "14581003", "H4": "14581004", "HeaderProtectionKey": profile.HeaderKey} {
		if got, ok := client[key].(string); !ok || got != want {
			t.Errorf("iOS AWG setting %s = %v, want string %q", key, client[key], want)
		}
		if !strings.Contains(profile.Conf(), key+" = "+want+"\n") {
			t.Errorf("native configuration is missing %s", key)
		}
	}
	if client["config"] != profile.Conf() {
		t.Fatal("embedded native config differs from exported .conf")
	}
	if client["mtu"] != "1280" || !strings.Contains(profile.Conf(), "MTU = 1280\n") {
		t.Fatal("mobile MTU is missing from the exported profiles")
	}
}
