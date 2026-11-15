package provision

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dementia/backend/internal/vpn"
	"golang.org/x/crypto/ssh"
)

func TestInstallSSH(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	header, public, err := vpn.KeyPair()
	if err != nil {
		t.Fatal(err)
	}

	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("integration tests are disabled")
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	const password = "ssh test password"
	config := &ssh.ServerConfig{
		PasswordCallback: func(meta ssh.ConnMetadata, raw []byte) (*ssh.Permissions, error) {
			if meta.User() != "ubuntu" || string(raw) != password {
				t.Error("unexpected SSH credentials")
			}
			return nil, nil
		},
	}
	config.AddHostKey(signer)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		server, channels, requests, err := ssh.NewServerConn(conn, config)
		if err != nil {
			t.Error(err)
			return
		}
		defer server.Close()
		go ssh.DiscardRequests(requests)
		session, requests, err := (<-channels).Accept()
		if err != nil {
			t.Error(err)
			return
		}
		defer session.Close()
		req := <-requests
		if req.Type != "exec" || !strings.Contains(string(req.Payload), "sudo -S") || strings.Contains(string(req.Payload), password) {
			t.Error("unexpected SSH exec request")
		}
		req.Reply(true, nil)
		payload, err := io.ReadAll(session)
		if err != nil {
			t.Error(err)
			return
		}
		script := string(payload)
		if !strings.HasPrefix(script, password+"\n") || !strings.Contains(script, "https://github.com/dementia-panel/node/releases/latest") ||
			!strings.Contains(script, "curl --proto") || !strings.Contains(script, "install.sh compose.yaml Dockerfile entrypoint.sh sync.py") ||
			!strings.Contains(script, `/bin/bash "$bundle/install.sh"`) || !strings.Contains(script, "'node-token'") || !strings.Contains(script, "'Финляндия'") {
			t.Error("installer input is incomplete")
		}
		io.WriteString(session, public+"\n"+header+"\n")
		session.SendRequest("exit-status", false, binary.BigEndian.AppendUint32(nil, 0))
	}()

	gotPublic, gotHeader, err := Install(t.Context(), Request{
		ID: "node", Name: "Финляндия", IP: "127.0.0.1", Login: "ubuntu", Password: password,
		Token: "node-token",
		VerifyHostKey: func(_ context.Context, fingerprint string) error {
			if fingerprint != ssh.FingerprintSHA256(signer.PublicKey()) {
				t.Fatal("unexpected server key")
			}
			return nil
		},
		PanelURL: "https://example.test", SSHPort: listener.Addr().(*net.TCPAddr).Port, VPNPort: 51820,
	})
	if err != nil {
		t.Fatal(err)
	}
	<-done
	if gotPublic != public || gotHeader != header {
		t.Fatal("wrong keys returned by the installer")
	}
}

func TestKeyOutputBoundsSSHOutput(t *testing.T) {
	var output keyOutput
	reader := strings.NewReader(strings.Repeat("x", 4096))
	count, err := io.Copy(&output, reader)
	if err != nil || count != 4096 || !output.overflow || output.buffer.Len() != 256 {
		t.Fatalf("unexpected bounded output: count=%d length=%d overflow=%t error=%v", count, output.buffer.Len(), output.overflow, err)
	}
}

func TestInstallRequiresHostKeyVerifier(t *testing.T) {
	_, _, err := Install(t.Context(), Request{})
	if err == nil || !strings.Contains(err.Error(), "verifier") {
		t.Fatal("missing host key verifier must fail before connecting")
	}
}

func TestRejectedHostKeyNeverSendsPassword(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("integration tests are disabled")
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	var authenticated atomic.Bool
	config := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { authenticated.Store(true); return nil, nil }}
	config.AddHostKey(signer)
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		server, _, _, err := ssh.NewServerConn(conn, config)
		if err == nil {
			server.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, _, err = Install(ctx, Request{IP: "127.0.0.1", Login: "root", Password: "must-not-be-transmitted", SSHPort: listener.Addr().(*net.TCPAddr).Port, VerifyHostKey: func(context.Context, string) error { return errors.New("changed key") }})
	if err == nil {
		t.Fatal("changed server key must be rejected")
	}
	<-done
	if authenticated.Load() {
		t.Fatal("password was sent before server identity verification")
	}
}
