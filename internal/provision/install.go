package provision

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"dementia/backend/internal/vpn"
	"golang.org/x/crypto/ssh"
)

// Resolve Latest once so every file belongs to the same published release.
const latestBundleScript = `release_url=$(curl --proto '=https' --proto-redir '=https' --fail --silent --show-error --location \
    --head --output /dev/null --write-out '%{url_effective}' \
    --connect-timeout 10 --max-time 60 --retry 2 --retry-max-time 90 \
    'https://github.com/dementia-panel/node/releases/latest') || exit 71
case "$release_url" in
    https://github.com/dementia-panel/node/releases/tag/*) ;;
    *) exit 71 ;;
esac
release_tag=${release_url#https://github.com/dementia-panel/node/releases/tag/}
case "$release_tag" in
    ''|*[!a-zA-Z0-9._-]*) exit 71 ;;
esac
bundle_url="https://raw.githubusercontent.com/dementia-panel/node/$release_tag"
`

type Request struct {
	ID, IP, Login, Password, Token, PanelURL, Name string
	SSHPort, VPNPort                               int
	VerifyHostKey                                  func(context.Context, string) error
}

func Install(ctx context.Context, req Request) (publicKey, headerKey string, err error) {
	if req.VerifyHostKey == nil {
		return "", "", errors.New("SSH server key verifier is not configured")
	}
	address := net.JoinHostPort(req.IP, strconv.Itoa(req.SSHPort))
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return "", "", errors.New("SSH connection failed")
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		return "", "", errors.New("could not initialize SSH timeout")
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	clientConfig := &ssh.ClientConfig{
		User:              req.Login,
		HostKeyAlgorithms: []string{ssh.KeyAlgoED25519},
		Auth:              []ssh.AuthMethod{ssh.Password(req.Password)},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			return req.VerifyHostKey(ctx, ssh.FingerprintSHA256(key))
		},
	}
	sshConn, channels, requests, err := ssh.NewClientConn(conn, address, clientConfig)
	if err != nil {
		return "", "", errors.New("SSH authentication or host key verification failed")
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return "", "", errors.New("could not clear SSH handshake timeout")
	}
	client := ssh.NewClient(sshConn, channels, requests)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", "", errors.New("could not open SSH session")
	}
	defer session.Close()

	var input strings.Builder
	command := `set -eu
umask 077
payload=$(mktemp)
trap 'rm -f "$payload"' EXIT
cat > "$payload"
/bin/bash "$payload"`
	if req.Login != "root" {
		command = `set -eu
umask 077
IFS= read -r password
payload=$(mktemp)
trap 'rm -f "$payload"' EXIT
cat > "$payload"
printf '%s\n' "$password" | sudo -S -p '' -- /bin/bash "$payload"`
		input.WriteString(req.Password + "\n")
	}
	input.WriteString(`set -eu
umask 077
if ! command -v curl >/dev/null; then
    . /etc/os-release
    case "$ID" in
        ubuntu|debian)
            export DEBIAN_FRONTEND=noninteractive
            (apt-get update && apt-get install -y ca-certificates curl) >&2 || exit 70
            ;;
        *) exit 70 ;;
    esac
fi
`)
	input.WriteString(latestBundleScript)
	input.WriteString(`bundle=$(mktemp -d)
trap 'rm -rf "$bundle"' EXIT
for file in install.sh compose.yaml Dockerfile entrypoint.sh sync.py; do
    curl --proto '=https' --proto-redir '=https' --fail --silent --show-error --location \
        --connect-timeout 10 --max-time 60 --retry 2 --retry-max-time 90 \
        "$bundle_url/$file" -o "$bundle/$file" || exit 71
done
printf '%s\n' `)
	for _, value := range []string{req.PanelURL, req.ID, req.Token, strconv.Itoa(req.VPNPort), req.Name} {
		input.WriteString(shellQuote(value) + " ")
	}
	input.WriteString("| /bin/bash \"$bundle/install.sh\"\n")
	session.Stdin = strings.NewReader(input.String())
	var output keyOutput
	session.Stdout = &output
	session.Stderr = io.Discard
	if err := session.Run(command); err != nil {
		var exit *ssh.ExitError
		if errors.As(err, &exit) {
			switch exit.ExitStatus() {
			case 70:
				return "", "", errors.New("could not prepare curl on the VPS; Ubuntu or Debian is required")
			case 71:
				return "", "", errors.New("could not resolve or download the latest node release from GitHub")
			}
		}
		return "", "", errors.New("AmneziaWG 3.1 installation failed; inspect the VPS provisioning log")
	}
	fields := strings.Fields(output.buffer.String())
	if output.overflow || len(fields) != 2 || !vpn.ValidKey(fields[0]) || !vpn.ValidKey(fields[1]) {
		return "", "", fmt.Errorf("invalid node key response")
	}
	return fields[0], fields[1], nil
}

type keyOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (out *keyOutput) Write(p []byte) (int, error) {
	remaining := 256 - out.buffer.Len()
	if len(p) > remaining {
		out.overflow = true
	}
	_, _ = out.buffer.Write(p[:min(len(p), remaining)])
	return len(p), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
