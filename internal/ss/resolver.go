package ss

import (
	"errors"
	"net"
	"net/netip"
	"strings"
)

var errInvalidPassXHeader = errors.New("invalid passx udp header")

type ClientIdentity struct {
	TransportAddr netip.AddrPort
	RealAddr      netip.AddrPort
}

func (i ClientIdentity) RealIP() string {
	if i.RealAddr.IsValid() {
		return i.RealAddr.Addr().String()
	}
	if i.TransportAddr.IsValid() {
		return i.TransportAddr.Addr().String()
	}
	return ""
}

type passXAccess struct {
	Enabled         bool
	TrustedCIDRs    []string
	InvalidCIDRs    []string
	trustedPrefixes []netip.Prefix
}

func compilePassXAccess(cfg PassXConfig) passXAccess {
	access := passXAccess{
		Enabled: cfg.Enabled,
	}
	if !cfg.Enabled {
		return access
	}

	prefixes := make([]netip.Prefix, 0, len(cfg.TrustedCIDRs))
	for _, raw := range cfg.TrustedCIDRs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		prefix, err := netip.ParsePrefix(raw)
		if err == nil {
			access.TrustedCIDRs = append(access.TrustedCIDRs, prefix.Masked().String())
			prefixes = append(prefixes, prefix.Masked())
			continue
		}

		addr, addrErr := netip.ParseAddr(raw)
		if addrErr != nil {
			access.InvalidCIDRs = append(access.InvalidCIDRs, raw)
			continue
		}
		prefix = netip.PrefixFrom(addr.Unmap(), addr.BitLen())
		access.TrustedCIDRs = append(access.TrustedCIDRs, prefix.String())
		prefixes = append(prefixes, prefix)
	}
	access.trustedPrefixes = prefixes
	return access
}

func (p passXAccess) isTrustedTransport(addr netip.AddrPort) bool {
	if !p.Enabled || !addr.IsValid() {
		return false
	}
	if len(p.trustedPrefixes) == 0 {
		return true
	}

	ip := addr.Addr()
	for _, prefix := range p.trustedPrefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func (p passXAccess) hasTrustedPrefixes() bool {
	return len(p.trustedPrefixes) > 0
}

func resolveTCPIdentity(conn net.Conn) ClientIdentity {
	addr, ok := addrPortFromNetAddr(conn.RemoteAddr())
	if !ok {
		return ClientIdentity{}
	}
	return ClientIdentity{
		TransportAddr: addr,
		RealAddr:      addr,
	}
}

func resolveUDPIdentity(packet []byte, transport netip.AddrPort, trustedTransport bool) (identity ClientIdentity, payloadOffset int, err error) {
	identity = ClientIdentity{
		TransportAddr: transport,
		RealAddr:      transport,
	}
	if !trustedTransport {
		return identity, 0, nil
	}

	if !looksLikePassXHeader(packet) {
		// Compatibility fallback for direct clients when PassX mode is enabled
		// but the packet does not carry a tunnel header.
		return identity, 0, nil
	}
	if len(packet) < 3 {
		return ClientIdentity{}, 0, errInvalidPassXHeader
	}

	switch packet[2] {
	case 0x01:
		const headerLen = 9
		if len(packet) < headerLen {
			return ClientIdentity{}, 0, errInvalidPassXHeader
		}
		var raw [4]byte
		copy(raw[:], packet[3:7])
		identity.RealAddr = netip.AddrPortFrom(netip.AddrFrom4(raw), readUint16(packet[7:9]))
		return validateResolvedUDPIdentity(identity, headerLen)
	case 0x02:
		const headerLen = 21
		if len(packet) < headerLen {
			return ClientIdentity{}, 0, errInvalidPassXHeader
		}
		var raw [16]byte
		copy(raw[:], packet[3:19])
		identity.RealAddr = netip.AddrPortFrom(netip.AddrFrom16(raw), readUint16(packet[19:21]))
		return validateResolvedUDPIdentity(identity, headerLen)
	default:
		return ClientIdentity{}, 0, errInvalidPassXHeader
	}
}

func validateResolvedUDPIdentity(identity ClientIdentity, payloadOffset int) (ClientIdentity, int, error) {
	if !identity.RealAddr.IsValid() || identity.RealAddr.Addr().IsUnspecified() {
		return ClientIdentity{}, 0, errInvalidPassXHeader
	}
	return identity, payloadOffset, nil
}

func looksLikePassXHeader(packet []byte) bool {
	return len(packet) >= 2 && packet[0] == 'P' && packet[1] == 'X'
}

func addrPortFromNetAddr(addr net.Addr) (netip.AddrPort, bool) {
	switch value := addr.(type) {
	case *net.TCPAddr:
		return addrPortFromIPPort(value.IP, value.Port)
	case *net.UDPAddr:
		return addrPortFromIPPort(value.IP, value.Port)
	default:
		parsed, err := netip.ParseAddrPort(addr.String())
		if err != nil {
			return netip.AddrPort{}, false
		}
		return parsed, true
	}
}

func addrPortFromIPPort(ip net.IP, port int) (netip.AddrPort, bool) {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok || port < 0 || port > 65535 {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(addr.Unmap(), uint16(port)), true
}

func readUint16(raw []byte) uint16 {
	return uint16(raw[0])<<8 | uint16(raw[1])
}

func passXSettingsChanged(previous, next passXAccess) bool {
	if previous.Enabled != next.Enabled {
		return true
	}
	if len(previous.TrustedCIDRs) != len(next.TrustedCIDRs) {
		return true
	}
	if len(previous.InvalidCIDRs) != len(next.InvalidCIDRs) {
		return true
	}
	for idx := range previous.TrustedCIDRs {
		if previous.TrustedCIDRs[idx] != next.TrustedCIDRs[idx] {
			return true
		}
	}
	for idx := range previous.InvalidCIDRs {
		if previous.InvalidCIDRs[idx] != next.InvalidCIDRs[idx] {
			return true
		}
	}
	return false
}
