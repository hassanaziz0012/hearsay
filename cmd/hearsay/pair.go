package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

// pairPhone is `hearsay pair [ADDRESS]` and `hearsay pair --forget`: pairing with the phone
// for dictation. The phone has to show the Hearsay app while pairing; both
// sides then show a 6-digit code and the user confirms on the phone that they match.
func (a *app) pairPhone(args []string) error {
	if len(args) > 0 && args[0] == "--forget" {
		return a.forgetPhone()
	}
	addr := ""
	if len(args) > 0 {
		addr = args[0]
	}
	return a.pairNet(addr, "")
}

// pairNet pairs with the phone at addr (port optional), or else the one mDNS finds, or else
// the one at guess (an IP address), or else where the user says.
func (a *app) pairNet(addr, guess string) error {
	say("Open the Hearsay app on the phone and keep it on screen.")

	name := "phone"
	if addr != "" {
		addr = withPort(addr)
	} else {
		say("Looking for the phone on the network…")
		found := browse(4*time.Second, nil)
		switch {
		case len(found) == 1:
			addr, name = found[0].addr, found[0].name
			done("found %s (%s)", found[0].name, addr)
		case len(found) > 1:
			names := make([]string, len(found))
			for i, s := range found {
				names[i] = s.name + " (" + s.addr + ")"
			}
			s := found[choose("Several phones found. Which one?", names)]
			addr, name = s.addr, s.name
		case guess != "":
			addr = net.JoinHostPort(guess, defaultPort)
		default:
			say("Not found. Is the phone on the same Wi-Fi, with the Hearsay key presser on?")
			ip := ask("Type the address the Hearsay app shows under This phone (e.g. 192.168.1.20:47800),\nor press Enter to stop:")
			if ip == "" {
				return errors.New("no phone to pair with")
			}
			addr = withPort(ip)
		}
	}

	p, err := a.pairBegin(context.Background(), addr, name, func() {
		say("Waiting for the Hearsay app to be on screen on the phone…")
	})
	if err != nil {
		return err
	}
	defer p.close()
	fmt.Printf("\n  Code: %s %s\n\n", p.code[:3], p.code[3:])
	say("Check that the phone shows the same code, then tap Pair on the phone.")
	say("If the codes differ, tap Cancel: something else answered instead of your phone.")
	if err := p.finish(); err != nil {
		return err
	}
	done("paired (saved in %s)", configPath())
	return nil
}

// withPort adds PhoneServer's usual port to an address without one. The app shows its
// port too, since it takes another one when 47800 is in use.
func withPort(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return net.JoinHostPort(addr, defaultPort)
	}
	return addr
}

// pairing is a pairing in progress: the phone has answered and both sides show code.
type pairing struct {
	a          *app
	conn       *tls.Conn
	rd         *bufio.Reader // conn's reader, kept for both requests
	addr, name string
	nonce      []byte
	phoneCert  []byte
	code       string // 6 digits
}

// pairBegin asks the phone at addr to pair and returns the code both sides show. The
// phone only pairs while the app is on screen, which can take the user a moment
// (unlocking it, for one): it keeps asking for a minute, calling waiting once.
func (a *app) pairBegin(ctx context.Context, addr, name string, waiting func()) (*pairing, error) {
	id, err := newIdentity()
	if err != nil {
		return nil, fmt.Errorf("creating this computer's key: %v", err)
	}
	host, _ := os.Hostname()
	p := &pairing{a: a, addr: addr, name: name}
	var r phoneReply
	for waitUntil := time.Now().Add(time.Minute); ; {
		if p.conn, err = dialTLS(addr, tlsConfig(id, ""), 5*time.Second); err != nil {
			return nil, fmt.Errorf("couldn't connect to %s: %v", addr, err)
		}
		p.nonce = make([]byte, 32)
		rand.Read(p.nonce)
		commit := sha256.Sum256(p.nonce)
		p.rd = bufio.NewReader(p.conn)
		r, err = exchangeOn(p.conn, p.rd, map[string]any{"op": "pair", "name": host, "commit": hex.EncodeToString(commit[:])}, 10*time.Second)
		if err != nil {
			p.conn.Close()
			return nil, fmt.Errorf("pairing: %v", err)
		}
		if r.OK || r.Error != "not_paired" {
			break
		}
		p.conn.Close()
		if time.Now().After(waitUntil) {
			return nil, errors.New("the phone isn't ready to pair: open the Hearsay app on it, keep it on screen, and try again")
		}
		if waiting != nil {
			waiting()
			waiting = nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	p.phoneCert = p.conn.ConnectionState().PeerCertificates[0].Raw
	if !r.OK {
		p.conn.Close()
		return nil, fmt.Errorf("pairing: %s", r.Msg)
	}
	phoneNonce, err := hex.DecodeString(r.Nonce)
	if err != nil || len(phoneNonce) != 32 {
		p.conn.Close()
		return nil, errors.New("pairing: the phone sent a bad nonce")
	}
	p.code = pairCode(p.phoneCert, id.Certificate[0], p.nonce, phoneNonce)
	return p, nil
}

// finish waits for the user to tap Pair on the phone and saves the phone in the config.
func (p *pairing) finish() error {
	r, err := exchangeOn(p.conn, p.rd, map[string]any{"op": "reveal", "nonce": hex.EncodeToString(p.nonce)}, 90*time.Second)
	if err != nil {
		return fmt.Errorf("pairing: %v", err)
	}
	if !r.OK {
		return fmt.Errorf("not paired: %s", r.Msg)
	}
	p.a.cfg.Phone = &pairedPhone{Name: p.name, Addr: p.addr, Cert: fingerprint(p.phoneCert)}
	return saveConfig(p.a.cfg)
}

// close ends the pairing; a finish waiting on the phone returns with an error.
func (p *pairing) close() { p.conn.Close() }

// forgetPhone unpairs on both sides (on the phone only if it is reachable now).
func (a *app) forgetPhone() error {
	phoneErr, err := a.unpair()
	if err != nil {
		return err
	}
	if phoneErr != nil {
		say("Couldn't reach the phone (%v). Remove this computer in the Hearsay app there.", phoneErr)
	} else {
		done("the phone forgot this computer")
	}
	done("forgot the phone")
	return nil
}

// unpair forgets the paired phone here and asks the phone to forget this computer;
// phoneErr says why the phone couldn't be asked (this side is forgotten anyway).
func (a *app) unpair() (phoneErr, err error) {
	if a.cfg.Phone == nil {
		return nil, errors.New("no phone is paired")
	}
	phoneErr = func() error {
		conn, err := a.dialPhone()
		if err != nil {
			return err
		}
		defer conn.Close()
		r, err := exchange(conn, map[string]string{"op": "forget"}, 5*time.Second)
		if err == nil && !r.OK {
			err = errors.New(r.Msg)
		}
		return err
	}()
	a.cfg.Phone = nil
	return phoneErr, saveConfig(a.cfg)
}

// pairCode is the 6-digit code both sides show (the same in PhoneServer.pairCode): SHA-256
// of "hearsay-pair-v1", the SHA-256 of the phone's and then the PC's certificate, the
// PC's nonce and the phone's nonce; the first 4 bytes as an unsigned big-endian number,
// modulo 1 000 000. The PC commits to its nonce before it sees the phone's, so someone in
// the middle can't search for certificates that make both sides show the same code.
func pairCode(phoneCert, pcCert, pcNonce, phoneNonce []byte) string {
	h := sha256.New()
	h.Write([]byte("hearsay-pair-v1"))
	pc, ph := sha256.Sum256(pcCert), sha256.Sum256(phoneCert)
	h.Write(ph[:])
	h.Write(pc[:])
	h.Write(pcNonce)
	h.Write(phoneNonce)
	return fmt.Sprintf("%06d", binary.BigEndian.Uint32(h.Sum(nil)[:4])%1_000_000)
}
