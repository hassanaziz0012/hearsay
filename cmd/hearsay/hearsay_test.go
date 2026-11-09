package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestShellQuote(t *testing.T) {
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Errorf("got %s", got)
	}
}

// useConfigDir points os.UserConfigDir at dir on every OS (XDG, macOS, Windows), so tests
// never touch the real config.
func useConfigDir(t *testing.T, dir string) {
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
	t.Setenv("HOME", dir)
	if d, err := os.UserConfigDir(); err != nil || !strings.HasPrefix(d, dir) {
		t.Fatalf("config dir %q, %v", d, err)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	useConfigDir(t, t.TempDir())
	if c, err := loadConfig(); err != nil || c.Keyboard != "" || c.Phone != nil {
		t.Fatalf("no config: got %+v, %v", c, err)
	}
	off := false
	want := config{Keyboard: "a.b/.C", Hotkey: "ctrl+alt+h", Paste: &off,
		Phone: &pairedPhone{Name: "Hearsay on Test", Addr: "192.0.2.1:47800", Cert: "ab"}}
	if err := saveConfig(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadConfig()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, %v", got, err)
	}
	os.WriteFile(configPath(), []byte("{"), 0o600)
	if _, err := loadConfig(); err == nil {
		t.Fatal("no error for a broken config")
	}
}

func TestLockRun(t *testing.T) {
	t.Setenv("HEARSAY_RUN_DIR", t.TempDir())
	unlock, err := lockRun()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockRun(); err != errBusy {
		t.Fatalf("second lock: %v", err)
	}
	unlock()
	unlock, err = lockRun()
	if err != nil {
		t.Fatalf("after unlock: %v", err)
	}
	unlock()
	// A lock left by a process that died is taken over.
	os.WriteFile(runFile("lock"), []byte("1\n"), 0o600)
	old := time.Now().Add(-2 * time.Minute)
	os.Chtimes(runFile("lock"), old, old)
	unlock, err = lockRun()
	if err != nil {
		t.Fatalf("stale lock: %v", err)
	}
	unlock()
}

func TestTrayDisplay(t *testing.T) {
	t.Setenv("HEARSAY_RUN_DIR", t.TempDir())
	a := &app{}
	check := func(age time.Duration, wantSt, wantMsg string) {
		t.Helper()
		then := time.Now().Add(-age)
		os.Chtimes(runFile("status"), then, then)
		if st, msg := trayDisplay(); st != wantSt || msg != wantMsg {
			t.Errorf("age %v: got %q %q, want %q %q", age, st, msg, wantSt, wantMsg)
		}
	}
	if st, msg := trayDisplay(); st != "idle" || msg != "" {
		t.Errorf("no status file: got %q %q", st, msg)
	}
	a.status("busy", "Starting…")
	check(0, "busy", "Starting…")
	check(busyStale+time.Second, "idle", "") // the press died
	a.status("error", "no phone")
	check(0, "error", "no phone")
	check(errorIconFor+time.Second, "idle", "Last error: no phone")
	a.status("retry", "lost the phone")
	check(time.Hour, "retry", "lost the phone") // stays until the next press
	a.status("bogus", "")
	check(0, "idle", "")
}

func TestRotatingLog(t *testing.T) {
	t.Setenv("HEARSAY_RUN_DIR", t.TempDir())
	w, err := openLog()
	if err != nil {
		t.Fatal(err)
	}
	line := []byte(strings.Repeat("x", 1023) + "\n")
	for range logMax/len(line) + 1 { // just past logMax
		w.Write(line)
	}
	w.Write([]byte("last\n"))
	b, _ := os.ReadFile(runFile("hearsay.log"))
	if string(b) != "last\n" {
		t.Errorf("new log has %d bytes", len(b))
	}
	if fi, err := os.Stat(runFile("hearsay.log.1")); err != nil || fi.Size() <= logMax {
		t.Errorf("rotated log: %v", err)
	}
}

func TestSentence(t *testing.T) {
	for in, want := range map[string]string{
		"Run `hearsay pair`":              "Run `hearsay pair`.",
		"Is the key presser on?":          "Is the key presser on?",
		"couldn't connect: i/o timeout. ": "couldn't connect: i/o timeout.",
		"":                                "",
	} {
		if got := sentence(in); got != want {
			t.Errorf("sentence(%q) = %q", in, got)
		}
	}
}

// An mDNS answer as Android's NsdManager sends it: PTR in the answers, SRV, TXT and A in
// the additional records, and an instance name with spaces.
func TestBrowserParse(t *testing.T) {
	inst := dnsmessage.MustNewName("Hearsay on Pixel 9._hearsay._tcp.local.")
	host := dnsmessage.MustNewName("Android-3.local.")
	hdr := func(n dnsmessage.Name, typ dnsmessage.Type) dnsmessage.ResourceHeader {
		return dnsmessage.ResourceHeader{Name: n, Type: typ, Class: dnsmessage.ClassINET, TTL: 120}
	}
	msg := func(withA bool) []byte {
		b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true, Authoritative: true})
		b.StartAnswers()
		b.PTRResource(hdr(serviceName, dnsmessage.TypePTR), dnsmessage.PTRResource{PTR: inst})
		b.StartAdditionals()
		b.SRVResource(hdr(inst, dnsmessage.TypeSRV), dnsmessage.SRVResource{Target: host, Port: 47801})
		b.TXTResource(hdr(inst, dnsmessage.TypeTXT), dnsmessage.TXTResource{TXT: []string{"fp=0123456789abcdef"}})
		if withA {
			b.AResource(hdr(host, dnsmessage.TypeA), dnsmessage.AResource{A: [4]byte{192, 0, 2, 7}})
		}
		out, err := b.Finish()
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	want := phoneService{name: "Hearsay on Pixel 9", addr: "192.0.2.7:47801", fp: "0123456789abcdef"}

	b := newBrowser()
	b.parse(msg(true), net.IPv4(192, 0, 2, 99))
	if got := b.complete(); len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v", got)
	}
	if got := b.complete(); len(got) != 0 {
		t.Fatalf("returned again: %+v", got)
	}

	// Without an A record, the address the answer came from.
	b = newBrowser()
	b.parse(msg(false), net.IPv4(192, 0, 2, 99))
	if got := b.complete(); len(got) != 1 || got[0].addr != "192.0.2.99:47801" {
		t.Fatalf("got %+v", got)
	}

	// Only the PTR so far: the next query asks for the instance's SRV and TXT.
	b = newBrowser()
	pb := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true})
	pb.StartAnswers()
	pb.PTRResource(hdr(serviceName, dnsmessage.TypePTR), dnsmessage.PTRResource{PTR: inst})
	ptrOnly, _ := pb.Finish()
	b.parse(ptrOnly, net.IPv4(192, 0, 2, 99))
	if got := b.complete(); len(got) != 0 {
		t.Fatalf("complete without SRV: %+v", got)
	}
	if qs := b.questions(); len(qs) != 3 || qs[1].Name != inst || qs[1].Type != dnsmessage.TypeSRV {
		t.Fatalf("questions %+v", qs)
	}
}

func TestSplitHotkey(t *testing.T) {
	mods, key, err := splitHotkey("Ctrl + Alt+Space")
	if err != nil || key != "space" || !reflect.DeepEqual(mods, []string{"ctrl", "alt"}) {
		t.Fatalf("got %v %q %v", mods, key, err)
	}
	if _, _, err := splitHotkey("ctrl+"); err == nil {
		t.Fatal("no error for a missing key")
	}
}

// The same vector as Python's hashlib computes; PhoneServer.pairCode must agree.
func TestPairCode(t *testing.T) {
	n1, n2 := make([]byte, 32), make([]byte, 32)
	for i := range n1 {
		n1[i], n2[i] = byte(i), byte(32+i)
	}
	if got := pairCode([]byte("phone-cert"), []byte("pc-cert"), n1, n2); got != "071347" {
		t.Fatalf("got %s", got)
	}
}

// A fake phone: the pinned client gets its reply, a wrong pin is refused in the handshake.
func TestPinnedExchange(t *testing.T) {
	useConfigDir(t, t.TempDir())
	pc, err := newIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := newIdentity(); !bytes.Equal(again.Certificate[0], pc.Certificate[0]) {
		t.Fatal("newIdentity replaced the existing key")
	}
	os.Remove(identityPath())
	phone, err := newIdentity() // another key, for the phone's side
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{phone},
		ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS13})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				line, err := bufio.NewReader(c).ReadString('\n')
				if err == nil && strings.Contains(line, `"op":"status"`) {
					fmt.Fprintln(c, `{"ok":true,"state":"idle"}`)
				}
			}()
		}
	}()
	pin := fingerprint(phone.Certificate[0])
	conn, err := dialTLS(ln.Addr().String(), tlsConfig(pc, pin), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r, err := exchange(conn, map[string]string{"op": "status"}, time.Second)
	conn.Close()
	if err != nil || !r.OK || r.State != "idle" {
		t.Fatalf("got %+v, %v", r, err)
	}
	if _, err := dialTLS(ln.Addr().String(), tlsConfig(pc, strings.Repeat("0", 64)), time.Second); err == nil {
		t.Fatal("connected despite a wrong pin")
	}
}

// pairBegin and finish against a fake phone that first isn't ready (the app not on
// screen), then pairs: the phone checks the PC's commitment, and the PC saves the phone.
func TestPairing(t *testing.T) {
	useConfigDir(t, t.TempDir()) // the phone's key, then the PC's config
	phone, err := newIdentity()
	if err != nil {
		t.Fatal(err)
	}
	useConfigDir(t, t.TempDir())
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{phone},
		ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS13})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	revealed := make(chan bool, 1)
	go func() {
		for n := 0; ; n++ {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			rd := bufio.NewReader(c)
			var req struct{ Op, Commit, Nonce string }
			line, _ := rd.ReadBytes('\n')
			json.Unmarshal(line, &req)
			if n == 0 {
				fmt.Fprintln(c, `{"ok":false,"error":"not_paired"}`)
				c.Close()
				continue
			}
			fmt.Fprintf(c, `{"ok":true,"nonce":"%s"}`+"\n", strings.Repeat("ab", 32))
			line, _ = rd.ReadBytes('\n')
			commit := req.Commit
			json.Unmarshal(line, &req)
			nonce, _ := hex.DecodeString(req.Nonce)
			sum := sha256.Sum256(nonce)
			ok := req.Op == "reveal" && hex.EncodeToString(sum[:]) == commit
			revealed <- ok
			fmt.Fprintf(c, `{"ok":%v}`+"\n", ok)
			c.Close()
			return
		}
	}()
	a := &app{}
	waited := false
	p, err := a.pairBegin(context.Background(), ln.Addr().String(), "Hearsay on Test", func() { waited = true })
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	if !waited || len(p.code) != 6 {
		t.Fatalf("waited %v, code %q", waited, p.code)
	}
	if err := p.finish(); err != nil {
		t.Fatal(err)
	}
	if !<-revealed {
		t.Fatal("the commitment didn't match the revealed nonce")
	}
	c, err := loadConfig()
	if err != nil || c.Phone == nil || c.Phone.Cert != fingerprint(phone.Certificate[0]) || c.Phone.Name != "Hearsay on Test" {
		t.Fatalf("saved %+v, %v", c.Phone, err)
	}
}

func TestPlainError(t *testing.T) {
	if got := plainError("dial tcp 192.0.2.1:47800: i/o timeout"); !strings.HasPrefix(got, "The phone didn't answer") {
		t.Errorf("got %q", got)
	}
	if got := plainError("couldn't find the phone."); got != "Couldn't find the phone." {
		t.Errorf("got %q", got)
	}
}

func TestWifiIP(t *testing.T) {
	out := `1: lo    inet 127.0.0.1/8 scope host lo\       valid_lft forever preferred_lft forever
12: rmnet_data0    inet 10.84.3.7/30 scope global rmnet_data0\       valid_lft forever preferred_lft forever
31: tun0    inet 10.8.0.2/24 scope global tun0\       valid_lft forever preferred_lft forever
40: wlan1    inet 192.168.18.57/24 brd 192.168.18.255 scope global wlan1\       valid_lft forever preferred_lft forever`
	for _, c := range []struct{ out, want string }{
		{out, "192.168.18.57"},
		{"5: eth0    inet 192.168.1.9/24 scope global eth0", "192.168.1.9"},
		{"12: rmnet_data0    inet 10.84.3.7/30 scope global rmnet_data0", ""},
		{"", ""},
	} {
		if got := wifiIP(c.out); got != c.want {
			t.Errorf("wifiIP(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}
