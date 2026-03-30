package ss

import (
	"io"
	"net"
	"net/netip"
	"testing"

	"github.com/pires/go-proxyproto"
)

func TestCompilePassXAccessNormalizesTrustedCIDRs(t *testing.T) {
	access := compilePassXAccess(PassXConfig{
		Enabled: true,
		TrustedCIDRs: []string{
			"1.1.1.1",
			"2001:db8::1",
			"10.0.0.0/24",
			"bad-entry",
		},
	})

	if !access.Enabled {
		t.Fatalf("expected passx access enabled")
	}
	if len(access.trustedPrefixes) != 3 {
		t.Fatalf("expected 3 trusted prefixes, got %d", len(access.trustedPrefixes))
	}
	if got := access.TrustedCIDRs[0]; got != "1.1.1.1/32" {
		t.Fatalf("unexpected normalized ipv4 cidr: %s", got)
	}
	if got := access.TrustedCIDRs[1]; got != "2001:db8::1/128" {
		t.Fatalf("unexpected normalized ipv6 cidr: %s", got)
	}
	if len(access.InvalidCIDRs) != 1 || access.InvalidCIDRs[0] != "bad-entry" {
		t.Fatalf("unexpected invalid cidrs: %+v", access.InvalidCIDRs)
	}
}

func TestResolveUDPIdentityIPv4(t *testing.T) {
	transport := netip.MustParseAddrPort("127.0.0.2:3000")
	packet := append(buildPassXHeader(netip.MustParseAddrPort("203.0.113.10:4000")), []byte("payload")...)

	identity, offset, err := resolveUDPIdentity(packet, transport, true)
	if err != nil {
		t.Fatalf("resolveUDPIdentity returned error: %v", err)
	}
	if got := identity.TransportAddr; got != transport {
		t.Fatalf("unexpected transport addr: %s", got)
	}
	if got := identity.RealAddr; got != netip.MustParseAddrPort("203.0.113.10:4000") {
		t.Fatalf("unexpected real addr: %s", got)
	}
	if offset != 9 {
		t.Fatalf("unexpected payload offset: %d", offset)
	}
}

func TestResolveUDPIdentityIPv6(t *testing.T) {
	transport := netip.MustParseAddrPort("[2001:db8::10]:3000")
	packet := append(buildPassXHeader(netip.MustParseAddrPort("[2001:db8::20]:4000")), []byte("payload")...)

	identity, offset, err := resolveUDPIdentity(packet, transport, true)
	if err != nil {
		t.Fatalf("resolveUDPIdentity returned error: %v", err)
	}
	if got := identity.RealAddr; got != netip.MustParseAddrPort("[2001:db8::20]:4000") {
		t.Fatalf("unexpected real addr: %s", got)
	}
	if offset != 21 {
		t.Fatalf("unexpected payload offset: %d", offset)
	}
}

func TestResolveUDPIdentityFallbackForDirectTraffic(t *testing.T) {
	transport := netip.MustParseAddrPort("127.0.0.2:3000")

	identity, offset, err := resolveUDPIdentity([]byte("plain-packet"), transport, true)
	if err != nil {
		t.Fatalf("resolveUDPIdentity returned error: %v", err)
	}
	if got := identity.RealAddr; got != transport {
		t.Fatalf("expected real addr fallback to transport, got %s", got)
	}
	if offset != 0 {
		t.Fatalf("unexpected payload offset: %d", offset)
	}
}

func TestResolveUDPIdentityRejectsBrokenPassXHeader(t *testing.T) {
	transport := netip.MustParseAddrPort("127.0.0.2:3000")

	tests := [][]byte{
		{'P', 'X'},
		{'P', 'X', 0x01},
		{'P', 'X', 0x02, 0x00},
		{'P', 'X', 0x03, 0x00, 0x00},
	}

	for _, packet := range tests {
		_, _, err := resolveUDPIdentity(packet, transport, true)
		if err == nil {
			t.Fatalf("expected error for packet %v", packet)
		}
	}
}

func TestResolveUDPIdentityRejectsUnspecifiedRealAddr(t *testing.T) {
	transport := netip.MustParseAddrPort("127.0.0.2:3000")
	packet := append(buildPassXHeader(netip.MustParseAddrPort("0.0.0.0:4000")), []byte("payload")...)

	_, _, err := resolveUDPIdentity(packet, transport, true)
	if err == nil {
		t.Fatalf("expected unspecified real address to be rejected")
	}
}

func TestResolveUDPIdentityIgnoresSpoofedHeaderFromUntrustedTransport(t *testing.T) {
	transport := netip.MustParseAddrPort("127.0.0.2:3000")
	packet := append(buildPassXHeader(netip.MustParseAddrPort("203.0.113.10:4000")), []byte("payload")...)

	identity, offset, err := resolveUDPIdentity(packet, transport, false)
	if err != nil {
		t.Fatalf("resolveUDPIdentity returned error: %v", err)
	}
	if got := identity.RealAddr; got != transport {
		t.Fatalf("expected real addr fallback to transport, got %s", got)
	}
	if offset != 0 {
		t.Fatalf("expected payload offset 0 for untrusted spoofed header, got %d", offset)
	}
}

func TestResolveTCPIdentityWithProxyProtocolV2(t *testing.T) {
	rawListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	defer rawListener.Close()

	listener := &proxyproto.Listener{
		Listener: rawListener,
		Policy: func(net.Addr) (proxyproto.Policy, error) {
			return proxyproto.USE, nil
		},
	}
	defer listener.Close()

	accepted := make(chan ClientIdentity, 1)
	errs := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errs <- err
			return
		}
		defer conn.Close()
		accepted <- resolveTCPIdentity(conn)
		_, _ = io.Copy(io.Discard, conn)
	}()

	conn, err := net.Dial("tcp", rawListener.Addr().String())
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}

	source := &net.TCPAddr{IP: net.ParseIP("203.0.113.10"), Port: 42311}
	dest, ok := rawListener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener addr type: %T", rawListener.Addr())
	}
	header := proxyproto.HeaderProxyFromAddrs(2, source, dest)
	if _, err := header.WriteTo(conn); err != nil {
		t.Fatalf("WriteTo returned error: %v", err)
	}
	_ = conn.Close()

	select {
	case err := <-errs:
		t.Fatalf("accept loop failed: %v", err)
	case identity := <-accepted:
		if got := identity.RealAddr; got != netip.MustParseAddrPort("203.0.113.10:42311") {
			t.Fatalf("unexpected real addr: %s", got)
		}
		if got := identity.TransportAddr; got != identity.RealAddr {
			t.Fatalf("expected tcp transport addr to match resolved addr, got %s vs %s", got, identity.RealAddr)
		}
	}
}

func TestResolveTCPIdentityIgnoresProxyHeaderFromUntrustedTransport(t *testing.T) {
	rawListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	defer rawListener.Close()

	listener := &proxyproto.Listener{
		Listener: rawListener,
		Policy: func(net.Addr) (proxyproto.Policy, error) {
			return proxyproto.IGNORE, nil
		},
	}
	defer listener.Close()

	type acceptedConn struct {
		identity ClientIdentity
	}
	accepted := make(chan acceptedConn, 1)
	errs := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errs <- err
			return
		}
		defer conn.Close()
		accepted <- acceptedConn{identity: resolveTCPIdentity(conn)}
		_, _ = io.Copy(io.Discard, conn)
	}()

	conn, err := net.Dial("tcp", rawListener.Addr().String())
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	localAddr, ok := conn.LocalAddr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected client local addr type: %T", conn.LocalAddr())
	}

	source := &net.TCPAddr{IP: net.ParseIP("203.0.113.10"), Port: 42311}
	dest, ok := rawListener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener addr type: %T", rawListener.Addr())
	}
	header := proxyproto.HeaderProxyFromAddrs(2, source, dest)
	if _, err := header.WriteTo(conn); err != nil {
		t.Fatalf("WriteTo returned error: %v", err)
	}
	_ = conn.Close()

	select {
	case err := <-errs:
		t.Fatalf("accept loop failed: %v", err)
	case acceptedConn := <-accepted:
		expected := netip.MustParseAddrPort(localAddr.String())
		if got := acceptedConn.identity.RealAddr; got != expected {
			t.Fatalf("expected fallback to original remote addr %s, got %s", expected, got)
		}
	}
}

func buildPassXHeader(realAddr netip.AddrPort) []byte {
	addr := realAddr.Addr()
	out := []byte{'P', 'X'}
	if addr.Is4() {
		raw := addr.As4()
		out = append(out, 0x01)
		out = append(out, raw[:]...)
	} else {
		raw := addr.As16()
		out = append(out, 0x02)
		out = append(out, raw[:]...)
	}
	out = append(out, byte(realAddr.Port()>>8), byte(realAddr.Port()))
	return out
}
