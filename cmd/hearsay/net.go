package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// The network transport talks to PhoneServer in the app (app/.../PhoneServer.kt has the
// protocol): one TLS 1.3 connection per request, both sides pinning the other's
// certificate, one JSON line each way. The phone keeps the dictation's state and puts
// itself back after a stop, so there is no state file and nothing to restore here.
type netTransport struct{ *app }

const defaultPort = "47800"

// phoneReply is PhoneServer's answer to any request.
type phoneReply struct {
	OK    bool     `json:"ok"`
	State string   `json:"state"`
	Text  string   `json:"text"`
	Error string   `json:"error"`
	Msg   string   `json:"msg"`
	Nonce string   `json:"nonce"`
	Log   []string `json:"log"`

	Keyboards []phoneKeyboard `json:"keyboards"`
}

func (a netTransport) toggle() error {
	// Only the phone knows whether it is listening; the tray's last state is a good guess
	// for what to show meanwhile.
	st, _, _ := readStatus()
	if st == "listening" {
		a.status("busy", "Finishing…")
	} else {
		a.status("busy", "Starting…")
	}
	r, err := a.call("toggle", st == "listening")
	if err != nil {
		return err
	}
	return a.finish(r, true)
}

func (a netTransport) start() error {
	a.status("busy", "Starting…")
	r, err := a.call("start", false)
	if err != nil {
		return err
	}
	return a.finish(r, true)
}

func (a netTransport) stop(paste bool) error {
	op := "cancel"
	if paste {
		op = "stop"
		a.status("busy", "Finishing…")
		a.notify("Finishing…", 2000, false)
	} else {
		a.status("busy", "Cancelling…")
	}
	r, err := a.call(op, true)
	if err != nil {
		return err
	}
	return a.finish(r, paste)
}

// finish shows what a successful request led to.
func (a netTransport) finish(r phoneReply, paste bool) error {
	switch r.State {
	case "listening":
		a.listening()
	case "restored":
		// An earlier dictation was cut off (the app restarted); the phone is back as it was.
		a.status("idle", "Phone restored")
		a.notify("Phone restored", 2000, false)
	default:
		a.logf("got %d chars", len([]rune(r.Text)))
		a.handOver(r.Text, paste)
		a.status("idle", "")
	}
	return nil
}

// call sends one request to the paired phone and returns its reply. Failures have been
// reported to the user. stopping: a dictation may be running on the phone, so if the
// phone can't be reached, the next press finishes it (the tray shows "retry").
func (a netTransport) call(op string, stopping bool) (phoneReply, error) {
	if err := a.requireKeyboard(); err != nil {
		return phoneReply{}, err
	}
	conn, err := a.dialPhone()
	if err != nil {
		if stopping {
			return phoneReply{}, a.failAs(err.Error()+" Then press again", true)
		}
		return phoneReply{}, a.failAs(err.Error(), false)
	}
	defer conn.Close()
	a.logf("connected to %s", conn.RemoteAddr())
	req := map[string]any{"op": op, "ime": a.cfg.Keyboard, "settle": settleMs,
		"timeout": settleTimeoutMs, "debug": a.debug}
	// A stop waits up to the settle timeout on the phone; a start takes up to ~9 s.
	r, err := exchange(conn, req, (settleTimeoutMs+12000)*time.Millisecond)
	for _, l := range r.Log {
		a.logf("  phone %s", l)
	}
	if err != nil {
		// The phone may have done it anyway (and is listening, or put itself back); the
		// next press finds out which.
		a.logf("request: %v", err)
		return r, a.failAs("lost the connection to the phone. Press again", true)
	}
	if !r.OK {
		msg := r.Msg
		if msg == "" {
			msg = r.Error
		}
		return r, a.failAs(msg, false)
	}
	return r, nil
}

// exchange writes req as one JSON line and reads the one-line reply.
func exchange(conn net.Conn, req any, timeout time.Duration) (phoneReply, error) {
	var r phoneReply
	conn.SetDeadline(time.Now().Add(timeout))
	b, err := json.Marshal(req)
	if err != nil {
		return r, err
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return r, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(line, &r)
}

// dialPhone connects to the paired phone: at its last known address, else wherever mDNS
// finds the certificate's fingerprint (and remembers that address).
func (a *app) dialPhone() (*tls.Conn, error) {
	p := a.cfg.Phone
	if p == nil {
		return nil, errors.New("no phone paired for the network connection. Run `hearsay pair`")
	}
	id, err := loadIdentity()
	if err != nil {
		return nil, err
	}
	conf := tlsConfig(id, p.Cert)
	conn, err := dialTLS(p.Addr, conf, 3*time.Second)
	if err == nil {
		return conn, nil
	}
	a.logf("dial %s: %v; looking for the phone over mDNS", p.Addr, err)
	refused := errors.Is(err, syscall.ECONNREFUSED)
	found := browse(5*time.Second, func(s phoneService) bool { return strings.HasPrefix(p.Cert, s.fp) })
	if len(found) == 0 {
		if refused {
			return nil, errors.New("the phone isn't listening. Is the Hearsay key presser on?")
		}
		return nil, errors.New("couldn't find the phone. Is it on the same Wi-Fi as this computer?")
	}
	addr := found[0].addr
	a.logf("found at %s", addr)
	if conn, err = dialTLS(addr, conf, 3*time.Second); err != nil {
		return nil, fmt.Errorf("couldn't connect to the phone at %s: %v", addr, err)
	}
	if addr != p.Addr {
		p.Addr = addr
		if err := saveConfig(a.cfg); err != nil {
			a.logf("save address: %v", err)
		}
	}
	return conn, nil
}

func dialTLS(addr string, conf *tls.Config, timeout time.Duration) (*tls.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	d := tls.Dialer{Config: conf}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return c.(*tls.Conn), nil
}

// tlsConfig presents this computer's certificate and accepts only the phone certificate
// with SHA-256 pin (hex). An empty pin accepts any, for pairing.
func tlsConfig(id tls.Certificate, pin string) *tls.Config {
	return &tls.Config{
		Certificates:       []tls.Certificate{id},
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, // self-signed: checked against the pin instead
		VerifyConnection: func(cs tls.ConnectionState) error {
			if pin == "" {
				return nil
			}
			if len(cs.PeerCertificates) == 0 || fingerprint(cs.PeerCertificates[0].Raw) != pin {
				return errors.New("the phone's certificate has changed. Pair again with `hearsay pair`")
			}
			return nil
		},
	}
}

func fingerprint(der []byte) string {
	h := sha256.Sum256(der)
	return hex.EncodeToString(h[:])
}

func identityPath() string { return filepath.Join(configDir(), "identity.pem") }

// loadIdentity returns this computer's key and self-signed certificate.
func loadIdentity() (tls.Certificate, error) {
	b, err := os.ReadFile(identityPath())
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("no key for the network connection (%v). Run `hearsay pair`", err)
	}
	return tls.X509KeyPair(b, b)
}

// newIdentity creates this computer's key pair (ECDSA P-256) and certificate, unless it
// exists. The private key stays in the config dir, readable only by the user.
func newIdentity() (tls.Certificate, error) {
	if id, err := loadIdentity(); err == nil {
		return id, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	host, _ := os.Hostname()
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Hearsay on " + host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(100, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	pk, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	b := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	b = append(b, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk})...)
	if err := os.MkdirAll(configDir(), 0o755); err != nil {
		return tls.Certificate{}, err
	}
	if err := writeAtomic(identityPath(), b); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(b, b)
}
