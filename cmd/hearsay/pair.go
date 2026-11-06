package main

import (
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
	id, err := newIdentity()
	if err != nil {
		return fmt.Errorf("creating this computer's key: %v", err)
	}
	say("Open the Hearsay app on the phone and keep it on screen.")

	name := "phone"
	if addr != "" {
		if _, _, err := net.SplitHostPort(addr); err != nil {
			addr = net.JoinHostPort(addr, defaultPort)
		}
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
			ip := ask("Type the address the Hearsay app shows (e.g. 192.168.1.20), or press Enter to stop:")
			if ip == "" {
				return errors.New("no phone to pair with")
			}
			addr = net.JoinHostPort(ip, defaultPort)
		}
	}

	// The phone only pairs while the app is on screen, which can take the user a moment
	// (unlocking it, for one): keep asking for a minute.
	host, _ := os.Hostname()
	var conn *tls.Conn
	var r phoneReply
	var nonce []byte
	waiting := false
	for waitUntil := time.Now().Add(time.Minute); ; {
		if conn, err = dialTLS(addr, tlsConfig(id, ""), 5*time.Second); err != nil {
			return fmt.Errorf("couldn't connect to %s: %v", addr, err)
		}
		nonce = make([]byte, 32)
		rand.Read(nonce)
		commit := sha256.Sum256(nonce)
		r, err = exchange(conn, map[string]any{"op": "pair", "name": host, "commit": hex.EncodeToString(commit[:])}, 10*time.Second)
		if err != nil {
			conn.Close()
			return fmt.Errorf("pairing: %v", err)
		}
		if r.OK || r.Error != "not_paired" {
			break
		}
		conn.Close()
		if time.Now().After(waitUntil) {
			return errors.New("the phone isn't ready to pair: open the Hearsay app on it, keep it on screen, and try again")
		}
		if !waiting {
			say("Waiting for the Hearsay app to be on screen on the phone…")
			waiting = true
		}
		time.Sleep(time.Second)
	}
	defer conn.Close()
	phoneCert := conn.ConnectionState().PeerCertificates[0].Raw
	if !r.OK {
		return fmt.Errorf("pairing: %s", r.Msg)
	}
	phoneNonce, err := hex.DecodeString(r.Nonce)
	if err != nil || len(phoneNonce) != 32 {
		return errors.New("pairing: the phone sent a bad nonce")
	}
	code := pairCode(phoneCert, id.Certificate[0], nonce, phoneNonce)
	fmt.Printf("\n  Code: %s %s\n\n", code[:3], code[3:])
	say("Check that the phone shows the same code, then tap Pair on the phone.")
	say("If the codes differ, tap Cancel: something else answered instead of your phone.")
	r, err = exchange(conn, map[string]any{"op": "reveal", "nonce": hex.EncodeToString(nonce)}, 90*time.Second)
	if err != nil {
		return fmt.Errorf("pairing: %v", err)
	}
	if !r.OK {
		return fmt.Errorf("not paired: %s", r.Msg)
	}

	a.cfg.Phone = &pairedPhone{Name: name, Addr: addr, Cert: fingerprint(phoneCert)}
	if err := saveConfig(a.cfg); err != nil {
		return err
	}
	done("paired (saved in %s)", configPath())
	return nil
}

// forgetPhone unpairs on both sides (on the phone only if it is reachable now).
func (a *app) forgetPhone() error {
	if a.cfg.Phone == nil {
		return errors.New("no phone is paired")
	}
	if conn, err := a.dialPhone(); err == nil {
		exchange(conn, map[string]string{"op": "forget"}, 5*time.Second)
		conn.Close()
		done("the phone forgot this computer")
	} else {
		say("Couldn't reach the phone (%v). Remove this computer in the Hearsay app there.", err)
	}
	a.cfg.Phone = nil
	if err := saveConfig(a.cfg); err != nil {
		return err
	}
	done("forgot the phone")
	return nil
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
