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
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestShellQuote(t *testing.T) {
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Errorf("got %s", got)
	}
}

func TestMigrateConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	if _, ok := migrateConfig(); ok {
		t.Fatal("migrated without an old config")
	}
	old := filepath.Join(home, "rambler-pc")
	os.MkdirAll(old, 0o755)
	c, ok := migrateConfig()
	if !ok || c.Keyboard != pixelboard {
		t.Fatalf("got %+v", c)
	}
	os.WriteFile(filepath.Join(old, "keyboard"), []byte("a.b/.C\n"), 0o644)
	if c, _ := migrateConfig(); c.Keyboard != "a.b/.C" {
		t.Fatalf("keyboard %q", c.Keyboard)
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // the phone's key, then the PC's config
	phone, err := newIdentity()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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
