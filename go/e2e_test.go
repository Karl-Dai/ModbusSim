// End-to-end tests: Go master <-> Go slave over real sockets, covering
// TCP, TLS (mutual auth), and RTU-over-TCP, plus the write/read roundtrip
// and log capture. Serial transports need hardware and are covered by unit
// tests only.
package go_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"net"
	"testing"
	"time"

	sslmatepkcs12 "software.sslmate.com/src/go-pkcs12"

	"github.com/Karl-Dai/ModbusSim/go/internal/master"
	"github.com/Karl-Dai/ModbusSim/go/internal/slave"
)

func newSlave(t *testing.T, maxAddr uint16) *slave.Server {
	t.Helper()
	srv := slave.NewServer()
	if err := srv.AddDevice(slave.WithDefaultRegisters(1, "dev", maxAddr)); err != nil {
		t.Fatal(err)
	}
	dev, _ := srv.GetDevice(1)
	dev.RegisterMap.WriteHoldingRegisters(100, []uint16{0xCAFE, 0x1234})
	dev.RegisterMap.WriteCoils(10, []bool{true})
	return srv
}

// TestMasterSlaveTCP exercises FC03 read batching, FC06/FC16 writes and
// read-back over a real TCP connection.
func TestMasterSlaveTCP(t *testing.T) {
	srv := newSlave(t, 1000)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = slave.RunTCP(ctx, ln, srv) }()

	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	conn := master.NewConnection(
		master.Config{TargetAddress: "127.0.0.1", Port: port, SlaveID: 1, TimeoutMs: 2000, Requests: master.DefaultRequestSettings()},
		master.Transport{Type: master.TransportTCP, Host: "127.0.0.1", Port: port},
	)
	if err := conn.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer conn.Disconnect()

	// FC03 read 2 registers starting at 100.
	res, err := conn.Read(ctx, master.ReadHoldingRegisters, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Registers) != 2 || res.Registers[0] != 0xCAFE || res.Registers[1] != 0x1234 {
		t.Fatalf("read = %+v", res)
	}

	// FC06 write single.
	if err := conn.WriteSingleRegister(ctx, 100, 0x0A0B); err != nil {
		t.Fatal(err)
	}
	res, _ = conn.Read(ctx, master.ReadHoldingRegisters, 100, 1)
	if res.Registers[0] != 0x0A0B {
		t.Fatalf("after FC06 = %04X", res.Registers[0])
	}

	// FC16 write multiple (123-register max per request) + batched read.
	vals := make([]uint16, 123)
	for i := range vals {
		vals[i] = uint16(i)
	}
	if err := conn.WriteMultipleRegisters(ctx, 100, vals); err != nil {
		t.Fatal(err)
	}
	res, err = conn.Read(ctx, master.ReadHoldingRegisters, 100, 123)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range res.Registers {
		if v != uint16(i) {
			t.Fatalf("register[%d] = %d", i, v)
		}
	}

	// FC01/FC05 coils.
	if err := conn.WriteSingleCoil(ctx, 10, true); err != nil {
		t.Fatal(err)
	}
	bits, err := conn.Read(ctx, master.ReadCoils, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bits.Bits[0] {
		t.Fatal("coil 10 should be on")
	}
}

// TestMasterSlaveRTUOverTCP exercises RTU framing over TCP.
func TestMasterSlaveRTUOverTCP(t *testing.T) {
	srv := newSlave(t, 100)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = slave.RunRTUOverTCP(ctx, ln, srv) }()

	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	conn := master.NewConnection(
		master.Config{TargetAddress: "127.0.0.1", Port: port, SlaveID: 1, TimeoutMs: 2000, Requests: master.DefaultRequestSettings()},
		master.Transport{Type: master.TransportRTUOverTCP, Host: "127.0.0.1", Port: port},
	)
	if err := conn.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer conn.Disconnect()

	res, err := conn.Read(ctx, master.ReadHoldingRegisters, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	if res.Registers[0] != 0xCAFE || res.Registers[1] != 0x1234 {
		t.Fatalf("read = % X", res.Registers)
	}
}

// TestMasterSlaveTLSMutualAuth runs slave and master over TLS with
// client-certificate authentication, using throwaway PKCS#12 identities.
func TestMasterSlaveTLSMutualAuth(t *testing.T) {
	srv := newSlave(t, 100)

	// Generate CA + server + client certs in memory.
	caKey, _ := ecdsa.GenerateKey(ellipticCurve(), randReader)
	caTmpl := &x509.Certificate{
		SerialNumber:          bigOne(),
		Subject:               pkix.Name{CommonName: "Test CA"},
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
	}
	caDER, err := x509.CreateCertificate(randReader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	makeIdentity := func(cn string, extKeyUsage x509.ExtKeyUsage) ([]byte, error) {
		key, err := ecdsa.GenerateKey(ellipticCurve(), randReader)
		if err != nil {
			return nil, err
		}
		tmpl := &x509.Certificate{
			SerialNumber: bigOne(),
			Subject:      pkix.Name{CommonName: cn},
			DNSNames:     []string{"localhost"},
			IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
			ExtKeyUsage:  []x509.ExtKeyUsage{extKeyUsage},
			KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
		}
		der, err := x509.CreateCertificate(randReader, tmpl, caCert, &key.PublicKey, caKey)
		if err != nil {
			return nil, err
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, err
		}
		return sslmatepkcs12.Encode(randReader, key, cert, []*x509.Certificate{caCert}, "pw")
	}

	serverP12, err := makeIdentity("server", x509.ExtKeyUsageServerAuth)
	if err != nil {
		t.Fatal(err)
	}
	clientP12, err := makeIdentity("client", x509.ExtKeyUsageClientAuth)
	if err != nil {
		t.Fatal(err)
	}

	// Write PKCS#12 blobs and the CA PEM to temp files (the API is file-based,
	// like the app).
	dir := t.TempDir()
	serverP12File := dir + "/server.p12"
	clientP12File := dir + "/client.p12"
	caFile := dir + "/ca.pem"
	if err := osWriteFile(serverP12File, serverP12); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(clientP12File, clientP12); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(caFile, pemEncodeCert(caCert)); err != nil {
		t.Fatal(err)
	}

	slaveTLS, err := slave.BuildTLSConfig(slave.TLSConfig{
		PKCS12File:        serverP12File,
		PKCS12Password:    "pw",
		RequireClientCert: true,
		CAFile:            caFile,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = slave.RunTLS(ctx, ln, slaveTLS, srv) }()

	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	conn := master.NewConnection(
		master.Config{
			TargetAddress: "127.0.0.1", Port: port, SlaveID: 1, TimeoutMs: 5000, Requests: master.DefaultRequestSettings(),
			TLS: master.TLSConfig{PKCS12File: clientP12File, PKCS12Password: "pw", CAFile: caFile},
		},
		master.Transport{Type: master.TransportTcpTls, Host: "127.0.0.1", Port: port},
	)
	if err := conn.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer conn.Disconnect()

	res, err := conn.Read(ctx, master.ReadHoldingRegisters, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	if res.Registers[0] != 0xCAFE {
		t.Fatalf("read = % X", res.Registers)
	}

	// A client without a certificate must fail the handshake.
	noCert := master.NewConnection(
		master.Config{TargetAddress: "127.0.0.1", Port: port, SlaveID: 1, TimeoutMs: 3000, Requests: master.DefaultRequestSettings()},
		master.Transport{Type: master.TransportTcpTls, Host: "127.0.0.1", Port: port},
	)
	if err := noCert.Connect(ctx); err == nil {
		t.Fatal("client without cert should be rejected")
	}
}

// TestLogCapture verifies the log collector receives RX/TX entries.
func TestLogCapture(t *testing.T) {
	srv := newSlave(t, 10)
	srv.Log = logcolNewCollector()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = slave.RunTCP(ctx, ln, srv) }()

	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	conn := master.NewConnection(
		master.Config{TargetAddress: "127.0.0.1", Port: port, SlaveID: 1, TimeoutMs: 2000, Requests: master.DefaultRequestSettings()},
		master.Transport{Type: master.TransportTCP, Host: "127.0.0.1", Port: port},
	)
	conn.SetLogSink(logcolSink{})
	if err := conn.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer conn.Disconnect()

	if _, err := conn.Read(ctx, master.ReadHoldingRegisters, 0, 5); err != nil {
		t.Fatal(err)
	}

	entries := srv.Log.GetAll()
	if len(entries) < 2 {
		t.Fatalf("slave log entries = %d, want >= 2 (rx+tx)", len(entries))
	}
	if entries[0].Direction != "rx" || entries[1].Direction != "tx" {
		t.Errorf("directions = %s/%s", entries[0].Direction, entries[1].Direction)
	}
	if !bytes.Contains([]byte(entries[0].Detail), []byte("R 0 x5")) {
		t.Errorf("rx detail = %q", entries[0].Detail)
	}
}

func TestEndToEndDeadlines(t *testing.T) {
	// Connecting to a closed port must produce a transport error, not hang.
	conn := master.NewConnection(
		master.Config{TargetAddress: "127.0.0.1", Port: 1, SlaveID: 1, TimeoutMs: 300},
		master.Transport{Type: master.TransportTCP, Host: "127.0.0.1", Port: 1},
	)
	if err := conn.Connect(context.Background()); err == nil {
		t.Fatal("connect to closed port should fail")
	} else if e, ok := err.(*master.Error); ok && e.Kind == "timeout" {
		// either timeout or connection refused is fine
		_ = e
	}
}
