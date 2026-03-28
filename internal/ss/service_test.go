package ss

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func TestServiceTCPAndUDP(t *testing.T) {
	tcpTarget, tcpClose := startTCPEchoServer(t)
	defer tcpClose()

	udpTarget, udpClose := startUDPEchoServer(t)
	defer udpClose()

	port := pickFreePort(t)
	service := NewService(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer func() { _ = service.Close() }()

	cfg := RuntimeConfig{
		Server: ServerConfig{
			ListenIP:   "127.0.0.1",
			ServerPort: port,
			Cipher:     "aes-256-gcm",
			EnableTCP:  true,
			EnableUDP:  true,
		},
		Users: []UserConfig{
			{ID: 1, UUID: "user-1", Method: "aes-256-gcm", Password: "f07a0130-b901-442f-82ca-4bb51113f193"},
		},
	}

	if err := service.Apply(cfg); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	def, _ := LookupCipher("aes-256-gcm")
	masterKey := DeriveMasterKey(def, cfg.Users[0].Password)

	if err := testTCPRoundTrip(port, def, masterKey, tcpTarget); err != nil {
		t.Fatalf("tcp round trip failed: %v", err)
	}
	if err := testUDPRoundTrip(port, def, masterKey, udpTarget); err != nil {
		t.Fatalf("udp round trip failed: %v", err)
	}

	snapshot, err := service.Snapshot(nil)
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	if len(snapshot.Traffic) == 0 {
		t.Fatalf("expected traffic records")
	}
}

func testTCPRoundTrip(port int, def CipherDef, masterKey []byte, target string) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", itoa(port)), 3*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	writer := NewStreamWriter(conn, def, masterKey)
	reader := NewClientStreamReader(conn, def, masterKey)

	targetAddr, err := EncodeAddr(target)
	if err != nil {
		return err
	}

	payload := append(targetAddr, []byte("hello-smallx-tcp")...)
	if _, err := writer.Write(payload); err != nil {
		return err
	}

	buf := make([]byte, len("hello-smallx-tcp"))
	if _, err := io.ReadFull(reader, buf); err != nil {
		return err
	}
	if string(buf) != "hello-smallx-tcp" {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func testUDPRoundTrip(port int, def CipherDef, masterKey []byte, target string) error {
	serverAddr := net.JoinHostPort("127.0.0.1", itoa(port))
	udpAddr, err := net.ResolveUDPAddr("udp", serverAddr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	targetAddr, err := EncodeAddr(target)
	if err != nil {
		return err
	}
	packet, err := encryptUDPPacket(def, masterKey, append(targetAddr, []byte("hello-smallx-udp")...))
	if err != nil {
		return err
	}
	if _, err := conn.WriteToUDP(packet, udpAddr); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 64*1024)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return err
	}
	plaintext, err := decryptUDPPacketForUser(def, masterKey, buf[:n])
	if err != nil {
		return err
	}
	_, headerLen, err := SplitAddr(plaintext)
	if err != nil {
		return err
	}
	if string(plaintext[headerLen:]) != "hello-smallx-udp" {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func startTCPEchoServer(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func startUDPEchoServer(t *testing.T) (string, func()) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = conn.WriteTo(buf[:n], addr)
		}
	}()
	return conn.LocalAddr().String(), func() { _ = conn.Close() }
}

func pickFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("unexpected addr type")
	}
	return addr.Port
}

func itoa(v int) string {
	return fmt.Sprintf("%d", v)
}
