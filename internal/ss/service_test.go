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

func TestUDPFullConeMapping(t *testing.T) {
	target1, close1 := startUDPSourceReporter(t)
	defer close1()
	target2, close2 := startUDPSourceReporter(t)
	defer close2()

	port := pickFreePort(t)
	service := NewService(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer func() { _ = service.Close() }()

	cfg := RuntimeConfig{
		Server: ServerConfig{
			ListenIP:   "127.0.0.1",
			ServerPort: port,
			Cipher:     "aes-256-gcm",
			EnableTCP:  false,
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

	clientConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatalf("ListenUDP returned error: %v", err)
	}
	defer clientConn.Close()

	source1, err := udpReporterRoundTrip(clientConn, port, def, masterKey, target1, "probe-1")
	if err != nil {
		t.Fatalf("first udp reporter round trip failed: %v", err)
	}
	source2, err := udpReporterRoundTrip(clientConn, port, def, masterKey, target2, "probe-2")
	if err != nil {
		t.Fatalf("second udp reporter round trip failed: %v", err)
	}

	if source1 != source2 {
		t.Fatalf("expected fullcone-style source reuse, got %s and %s", source1, source2)
	}
}

func TestSpeedLimitAffectsTCPDownstream(t *testing.T) {
	target, closeTarget := startTCPBurstServer(t, 256*1024)
	defer closeTarget()

	port := pickFreePort(t)
	service := NewService(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer func() { _ = service.Close() }()

	cfg := RuntimeConfig{
		Server: ServerConfig{
			ListenIP:   "127.0.0.1",
			ServerPort: port,
			Cipher:     "aes-256-gcm",
			EnableTCP:  true,
			EnableUDP:  false,
		},
		Users: []UserConfig{
			{ID: 1, UUID: "user-1", Method: "aes-256-gcm", Password: "f07a0130-b901-442f-82ca-4bb51113f193", SpeedLimit: 1},
		},
	}

	if err := service.Apply(cfg); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	def, _ := LookupCipher("aes-256-gcm")
	masterKey := DeriveMasterKey(def, cfg.Users[0].Password)

	conn, reader, closeConn := openSSClient(t, port, def, masterKey, "127.0.0.2")
	defer closeConn()
	sendSSRequest(t, conn, target, []byte("burst"))

	start := time.Now()
	_, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 800*time.Millisecond {
		t.Fatalf("expected speed limit to slow transfer, got %s", elapsed)
	}
}

func TestDeviceLimitRejectsExtraTCPDevice(t *testing.T) {
	target, closeTarget := startTCPEchoServer(t)
	defer closeTarget()

	port := pickFreePort(t)
	service := NewService(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer func() { _ = service.Close() }()

	cfg := RuntimeConfig{
		Server: ServerConfig{
			ListenIP:            "127.0.0.1",
			ServerPort:          port,
			Cipher:              "aes-256-gcm",
			EnableTCP:           true,
			EnableUDP:           false,
			EnforceDeviceLimit:  true,
			DefaultTCPConnLimit: 0,
		},
		Users: []UserConfig{
			{ID: 1, UUID: "user-1", Method: "aes-256-gcm", Password: "f07a0130-b901-442f-82ca-4bb51113f193", DeviceLimit: 1},
		},
	}

	if err := service.Apply(cfg); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	def, _ := LookupCipher("aes-256-gcm")
	masterKey := DeriveMasterKey(def, cfg.Users[0].Password)

	firstConn, firstReader, firstClose := openSSClient(t, port, def, masterKey, "127.0.0.2")
	defer firstClose()
	sendSSRequest(t, firstConn, target, []byte("hold-open"))
	buf := make([]byte, len("hold-open"))
	if _, err := io.ReadFull(firstReader, buf); err != nil {
		t.Fatalf("first client read failed: %v", err)
	}

	secondConn, secondReader, secondClose := openSSClient(t, port, def, masterKey, "127.0.0.3")
	defer secondClose()
	sendSSRequest(t, secondConn, target, []byte("should-fail"))
	_ = secondConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := secondReader.Read(make([]byte, 16))
	if err == nil {
		t.Fatalf("expected second device to be disconnected")
	}

	snapshot, err := service.Snapshot(nil)
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	if len(snapshot.AliveIPs) != 1 {
		t.Fatalf("expected exactly one active user alive record, got %d", len(snapshot.AliveIPs))
	}
	if len(snapshot.AliveIPs[0].IPs) != 1 || snapshot.AliveIPs[0].IPs[0] != "127.0.0.2" {
		t.Fatalf("expected only first device to be counted online, got %+v", snapshot.AliveIPs[0].IPs)
	}
}

func TestDeviceLimitRejectsExtraUDPDevice(t *testing.T) {
	target, closeTarget := startUDPEchoServer(t)
	defer closeTarget()

	port := pickFreePort(t)
	service := NewService(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer func() { _ = service.Close() }()

	cfg := RuntimeConfig{
		Server: ServerConfig{
			ListenIP:           "127.0.0.1",
			ServerPort:         port,
			Cipher:             "aes-256-gcm",
			EnableTCP:          false,
			EnableUDP:          true,
			EnforceDeviceLimit: true,
		},
		Users: []UserConfig{
			{ID: 1, UUID: "user-1", Method: "aes-256-gcm", Password: "f07a0130-b901-442f-82ca-4bb51113f193", DeviceLimit: 1},
		},
	}

	if err := service.Apply(cfg); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	def, _ := LookupCipher("aes-256-gcm")
	masterKey := DeriveMasterKey(def, cfg.Users[0].Password)

	firstUDP := bindUDPClient(t, "127.0.0.2")
	defer firstUDP.Close()
	got1, err := udpReporterRoundTripFromConn(firstUDP, port, def, masterKey, target, "allowed-udp")
	if err != nil {
		t.Fatalf("first udp roundtrip failed: %v", err)
	}
	if got1 != "allowed-udp" {
		t.Fatalf("unexpected first udp payload: %s", got1)
	}

	secondUDP := bindUDPClient(t, "127.0.0.3")
	defer secondUDP.Close()
	_, err = udpReporterRoundTripFromConn(secondUDP, port, def, masterKey, target, "blocked-udp")
	if err == nil {
		t.Fatalf("expected second udp device to be blocked")
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

func udpReporterRoundTrip(conn *net.UDPConn, port int, def CipherDef, masterKey []byte, target string, payload string) (string, error) {
	return udpReporterRoundTripFromConn(conn, port, def, masterKey, target, payload)
}

func udpReporterRoundTripFromConn(conn *net.UDPConn, port int, def CipherDef, masterKey []byte, target string, payload string) (string, error) {
	serverAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:"+itoa(port))
	if err != nil {
		return "", err
	}
	targetAddr, err := EncodeAddr(target)
	if err != nil {
		return "", err
	}
	packet, err := encryptUDPPacket(def, masterKey, append(targetAddr, []byte(payload)...))
	if err != nil {
		return "", err
	}
	if _, err := conn.WriteToUDP(packet, serverAddr); err != nil {
		return "", err
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 64*1024)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return "", err
	}
	plaintext, err := decryptUDPPacketForUser(def, masterKey, buf[:n])
	if err != nil {
		return "", err
	}
	_, headerLen, err := SplitAddr(plaintext)
	if err != nil {
		return "", err
	}
	return string(plaintext[headerLen:]), nil
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

func startTCPBurstServer(t *testing.T, size int) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte('a' + (i % 26))
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
				buf := make([]byte, 16)
				_, _ = c.Read(buf)
				_, _ = c.Write(payload)
			}(conn)
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func startUDPSourceReporter(t *testing.T) (string, func()) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 64*1024)
		for {
			_, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = conn.WriteTo([]byte(addr.String()), addr)
		}
	}()
	return conn.LocalAddr().String(), func() { _ = conn.Close() }
}

func openSSClient(t *testing.T, port int, def CipherDef, masterKey []byte, localIP string) (net.Conn, io.Reader, func()) {
	t.Helper()
	dialer := &net.Dialer{
		LocalAddr: &net.TCPAddr{IP: net.ParseIP(localIP)},
		Timeout:   3 * time.Second,
	}
	conn, err := dialer.Dial("tcp", "127.0.0.1:"+itoa(port))
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	reader := NewClientStreamReader(conn, def, masterKey)
	return conn, reader, func() { _ = conn.Close() }
}

func sendSSRequest(t *testing.T, conn net.Conn, target string, payload []byte) {
	t.Helper()
	def, _ := LookupCipher("aes-256-gcm")
	masterKey := DeriveMasterKey(def, "f07a0130-b901-442f-82ca-4bb51113f193")
	writer := NewStreamWriter(conn, def, masterKey)
	addr, err := EncodeAddr(target)
	if err != nil {
		t.Fatalf("EncodeAddr failed: %v", err)
	}
	if _, err := writer.Write(append(addr, payload...)); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

func bindUDPClient(t *testing.T, ip string) *net.UDPConn {
	t.Helper()
	addr := &net.UDPAddr{IP: net.ParseIP(ip), Port: 0}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("ListenUDP failed: %v", err)
	}
	return conn
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
