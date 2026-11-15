package vpn

import (
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// These values must match the server configuration in node/entrypoint.sh.
const Parameters = "S1 = 32\nS2 = 48\nS3 = 16\nS4 = 24\nH1 = 14581001\nH2 = 14581002\nH3 = 14581003\nH4 = 14581004\n"

func KeyPair() (private, public string, err error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", "", err
	}
	key[0] &= 248
	key[31] = (key[31] & 127) | 64
	priv, err := ecdh.X25519().NewPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(priv.Bytes()), base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes()), nil
}

func ValidKey(key string) bool {
	decoded, err := base64.StdEncoding.Strict().DecodeString(key)
	return err == nil && len(decoded) == 32 && base64.StdEncoding.EncodeToString(decoded) == key
}

func Address(offset int) string {
	return fmt.Sprintf("10.70.%d.%d/32", offset/256, offset%256)
}

type ClientProfile struct {
	Name, PrivateKey, PublicKey, HeaderKey, IP, Address string
	Port                                                int
}

const dns = "1.1.1.1"
const mtu = "1360"
const keepalive = "25"
const allowed = "0.0.0.0/0"

func (p ClientProfile) Conf() string {
	endpoint := net.JoinHostPort(p.IP, strconv.Itoa(p.Port))
	return fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = %s\nDNS = %s\nMTU = %s\n%sHeaderProtectionKey = %s\n\n[Peer]\nPublicKey = %s\nEndpoint = %s\nAllowedIPs = %s\nPersistentKeepalive = %s\n",
		p.PrivateKey, p.Address, dns, mtu, Parameters, p.HeaderKey, p.PublicKey, endpoint, allowed, keepalive)
}

// AmneziaLink uses the AmneziaVPN guest config format with no SSH credentials.
func (p ClientProfile) AmneziaLink() (string, error) {
	const container = "amnezia-awg"
	lastConfig := map[string]any{
		"config": p.Conf(), "client_priv_key": p.PrivateKey, "server_pub_key": p.PublicKey,
		"client_ip": p.Address, "hostName": p.IP, "port": p.Port, "mtu": mtu,
		"allowed_ips": []string{allowed}, "persistent_keep_alive": keepalive, "HeaderProtectionKey": p.HeaderKey,
	}
	for line := range strings.SplitSeq(strings.TrimSpace(Parameters), "\n") {
		key, value, _ := strings.Cut(line, " = ")
		lastConfig[key] = value
	}
	inner, err := json.Marshal(lastConfig)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(map[string]any{
		"description": p.Name, "hostName": p.IP, "defaultContainer": container, "dns1": dns, "dns2": dns,
		"containers": []any{map[string]any{
			"container": container,
			"awg": map[string]any{
				"last_config": string(inner), "isThirdPartyConfig": true, "port": strconv.Itoa(p.Port), "transport_proto": "udp",
			},
		}},
	})
	if err != nil {
		return "", err
	}

	var payload bytes.Buffer
	// Qt's qCompress prepends the big-endian length of the uncompressed JSON.
	payload.Write(binary.BigEndian.AppendUint32(nil, uint32(len(data))))
	zip := zlib.NewWriter(&payload)
	if _, err := zip.Write(data); err != nil {
		return "", err
	}
	if err := zip.Close(); err != nil {
		return "", err
	}
	return "vpn://" + base64.RawURLEncoding.EncodeToString(payload.Bytes()), nil
}

type Vault struct {
	cipher cipher.AEAD
}

func NewVault(key []byte) (Vault, error) {
	if len(key) != 32 {
		return Vault{}, fmt.Errorf("encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return Vault{}, err
	}
	aead, err := cipher.NewGCM(block)
	return Vault{cipher: aead}, err
}

func (v Vault) Seal(value string) []byte {
	nonce := make([]byte, v.cipher.NonceSize())
	return v.cipher.Seal(nonce, fillNonce(nonce), []byte(value), nil)
}

func fillNonce(nonce []byte) []byte {
	// Go's crypto/rand.Read always fills the buffer and terminates on entropy failure.
	rand.Read(nonce)
	return nonce
}

func (v Vault) Open(value []byte) (string, error) {
	size := v.cipher.NonceSize()
	if len(value) < size {
		return "", fmt.Errorf("invalid encrypted key")
	}
	data, err := v.cipher.Open(nil, value[:size], value[size:], nil)
	return string(data), err
}
