package ss

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
)

const (
	atypIPv4   = 0x01
	atypDomain = 0x03
	atypIPv6   = 0x04
)

func SplitAddr(payload []byte) (target string, headerLen int, err error) {
	if len(payload) < 1 {
		return "", 0, fmt.Errorf("short socks address")
	}
	switch payload[0] {
	case atypIPv4:
		if len(payload) < 1+4+2 {
			return "", 0, fmt.Errorf("short ipv4 address")
		}
		ip := net.IP(payload[1 : 1+4]).String()
		port := binary.BigEndian.Uint16(payload[5:7])
		return net.JoinHostPort(ip, strconv.Itoa(int(port))), 7, nil
	case atypDomain:
		if len(payload) < 2 {
			return "", 0, fmt.Errorf("short domain address")
		}
		size := int(payload[1])
		if len(payload) < 2+size+2 {
			return "", 0, fmt.Errorf("short domain payload")
		}
		host := string(payload[2 : 2+size])
		port := binary.BigEndian.Uint16(payload[2+size : 2+size+2])
		return net.JoinHostPort(host, strconv.Itoa(int(port))), 2 + size + 2, nil
	case atypIPv6:
		if len(payload) < 1+16+2 {
			return "", 0, fmt.Errorf("short ipv6 address")
		}
		ip := net.IP(payload[1 : 1+16]).String()
		port := binary.BigEndian.Uint16(payload[17:19])
		return net.JoinHostPort(ip, strconv.Itoa(int(port))), 19, nil
	default:
		return "", 0, fmt.Errorf("unsupported atyp: %d", payload[0])
	}
}

func EncodeAddr(target string) ([]byte, error) {
	host, portString, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	portValue, err := strconv.Atoi(portString)
	if err != nil {
		return nil, err
	}
	if portValue < 0 || portValue > 65535 {
		return nil, fmt.Errorf("invalid port: %d", portValue)
	}

	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, uint16(portValue))

	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			out := make([]byte, 1+4+2)
			out[0] = atypIPv4
			copy(out[1:], v4)
			copy(out[5:], port)
			return out, nil
		}
		if v6 := ip.To16(); v6 != nil {
			out := make([]byte, 1+16+2)
			out[0] = atypIPv6
			copy(out[1:], v6)
			copy(out[17:], port)
			return out, nil
		}
	}

	host = strings.TrimSpace(host)
	if len(host) == 0 || len(host) > 255 {
		return nil, fmt.Errorf("invalid domain length")
	}

	out := make([]byte, 1+1+len(host)+2)
	out[0] = atypDomain
	out[1] = byte(len(host))
	copy(out[2:], host)
	copy(out[2+len(host):], port)
	return out, nil
}

func AddrFromNet(addr net.Addr) ([]byte, error) {
	switch v := addr.(type) {
	case *net.TCPAddr:
		return EncodeAddr(v.String())
	case *net.UDPAddr:
		return EncodeAddr(v.String())
	default:
		return EncodeAddr(addr.String())
	}
}

func RemoteIP(addr net.Addr) string {
	switch v := addr.(type) {
	case *net.TCPAddr:
		return v.IP.String()
	case *net.UDPAddr:
		return v.IP.String()
	default:
		host, _, err := net.SplitHostPort(addr.String())
		if err != nil {
			return addr.String()
		}
		return host
	}
}

