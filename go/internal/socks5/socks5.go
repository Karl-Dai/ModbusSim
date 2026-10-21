// Package socks5 implements a minimal SOCKS5 CONNECT client for the
// TCP-based Modbus master transports. Ported from
// crates/modbussim-core/src/socks5.rs with identical semantics, including
// proxy-side DNS (domain names are sent to the proxy, not resolved locally).
package socks5

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// Config mirrors Rust's Socks5Config (serde field names).
type Config struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// DefaultConfig mirrors Socks5Config::default().
func DefaultConfig() Config {
	return Config{Host: "127.0.0.1", Port: 1080}
}

// Validate mirrors Socks5Config::validate.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Host == "" || len(trimSpace(c.Host)) == 0 {
		return fmt.Errorf("SOCKS5 proxy host is required")
	}
	if c.Port == 0 {
		return fmt.Errorf("SOCKS5 proxy port must be between 1 and 65535")
	}
	if (c.Username == "") != (c.Password == "") {
		return fmt.Errorf("SOCKS5 username and password must either both be set or both be empty")
	}
	if len(c.Username) > 255 || len(c.Password) > 255 {
		return fmt.Errorf("SOCKS5 username and password must not exceed 255 bytes")
	}
	return nil
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && isSpace(s[start]) {
		start++
	}
	for end > start && isSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// ConnectError distinguishes timeout vs connection failures (TcpConnectError).
type ConnectError struct {
	Timeout bool
	Msg     string
}

func (e *ConnectError) Error() string { return e.Msg }

// ConnectTCP connects to targetHost:targetPort directly or through the
// configured SOCKS5 proxy. The timeout covers DNS lookup, proxy connection,
// authentication, and CONNECT.
func ConnectTCP(targetHost string, targetPort uint16, proxy Config, timeout time.Duration) (net.Conn, error) {
	return ConnectTCPContext(context.Background(), targetHost, targetPort, proxy, timeout)
}

// ConnectTCPContext covers dial, authentication and CONNECT with one deadline.
func ConnectTCPContext(ctx context.Context, targetHost string, targetPort uint16, proxy Config, timeout time.Duration) (net.Conn, error) {
	if trimSpace(targetHost) == "" || len(targetHost) > 255 || targetPort == 0 {
		return nil, &ConnectError{Msg: "invalid target host or port"}
	}
	if err := proxy.Validate(); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	host, port := targetHost, targetPort
	if proxy.Enabled {
		host, port = proxy.Host, proxy.Port
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		return nil, &ConnectError{Timeout: ctx.Err() == context.DeadlineExceeded, Msg: fmt.Sprintf("TCP connection failed: %v", err)}
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	if proxy.Enabled {
		err = connectViaProxy(conn, targetHost, targetPort, proxy)
	}
	if !stop() || ctx.Err() != nil {
		conn.Close()
		return nil, ctx.Err()
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	return conn, nil
}

func connectViaProxy(conn net.Conn, targetHost string, targetPort uint16, proxy Config) error {

	// Greeting: offer NoAuth (0x00) always, Username/Password (0x02) if set.
	method := byte(0x00)
	if proxy.Username != "" {
		method = 0x02
	}
	greeting := []byte{0x05, 0x01, method}
	if _, err := conn.Write(greeting); err != nil {
		conn.Close()
		return &ConnectError{Msg: fmt.Sprintf("proxy greeting write failed: %v", err)}
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		conn.Close()
		return &ConnectError{Msg: fmt.Sprintf("proxy greeting read failed: %v", err)}
	}
	if resp[0] != 0x05 {
		conn.Close()
		return &ConnectError{Msg: fmt.Sprintf("proxy protocol mismatch: version 0x%02X", resp[0])}
	}
	if resp[1] == 0xFF {
		conn.Close()
		return &ConnectError{Msg: "proxy authentication methods unacceptable"}
	}
	if resp[1] != method {
		conn.Close()
		return &ConnectError{Msg: fmt.Sprintf("proxy chose unexpected auth method 0x%02X", resp[1])}
	}

	// Optional username/password subnegotiation (RFC 1929).
	if method == 0x02 {
		auth := make([]byte, 0, 3+len(proxy.Username)+len(proxy.Password))
		auth = append(auth, 0x01, byte(len(proxy.Username)))
		auth = append(auth, proxy.Username...)
		auth = append(auth, byte(len(proxy.Password)))
		auth = append(auth, proxy.Password...)
		if _, err := conn.Write(auth); err != nil {
			conn.Close()
			return &ConnectError{Msg: fmt.Sprintf("proxy auth write failed: %v", err)}
		}
		authResp := make([]byte, 2)
		if _, err := io.ReadFull(conn, authResp); err != nil {
			conn.Close()
			return &ConnectError{Msg: fmt.Sprintf("proxy auth read failed: %v", err)}
		}
		if authResp[1] != 0x00 {
			conn.Close()
			return &ConnectError{Msg: "proxy authentication failed"}
		}
	}

	// CONNECT request: proxy-side DNS for hostnames (ATYP=domain), IPv4/IPv6 literal.
	req := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(targetHost); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			req = append(req, 0x01)
			req = append(req, ip4...)
		} else {
			req = append(req, 0x04)
			req = append(req, ip.To16()...)
		}
	} else {
		req = append(req, 0x03, byte(len(targetHost)))
		req = append(req, targetHost...)
	}
	var portBytes [2]byte
	binary.BigEndian.PutUint16(portBytes[:], targetPort)
	req = append(req, portBytes[:]...)

	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return &ConnectError{Msg: fmt.Sprintf("proxy CONNECT write failed: %v", err)}
	}

	// Reply: VER REP RSV ATYP BND.ADDR BND.PORT. Read header, then skip the
	// bound address so the stream is positioned for data.
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		conn.Close()
		return &ConnectError{Msg: fmt.Sprintf("proxy CONNECT read failed: %v", err)}
	}
	if head[1] != 0x00 {
		conn.Close()
		return &ConnectError{Msg: fmt.Sprintf("proxy CONNECT rejected: 0x%02X", head[1])}
	}
	var skip int
	switch head[3] {
	case 0x01:
		skip = 4 + 2
	case 0x04:
		skip = 16 + 2
	case 0x03:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			conn.Close()
			return &ConnectError{Msg: fmt.Sprintf("proxy CONNECT read failed: %v", err)}
		}
		skip = int(lenBuf[0]) + 2
	default:
		conn.Close()
		return &ConnectError{Msg: fmt.Sprintf("proxy CONNECT bad address type 0x%02X", head[3])}
	}
	if _, err := io.CopyN(io.Discard, conn, int64(skip)); err != nil {
		conn.Close()
		return &ConnectError{Msg: fmt.Sprintf("proxy CONNECT read failed: %v", err)}
	}
	return nil
}
