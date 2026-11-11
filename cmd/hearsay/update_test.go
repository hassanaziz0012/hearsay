package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		v, cur string
		want   bool
	}{
		{"v0.4.0", "v0.3.0", true},
		{"v0.10.0", "v0.9.9", true},
		{"v1.0.0", "v0.99.0", true},
		{"v0.3.1", "v0.3.0", true},
		{"v0.3.0", "v0.3.0", false},
		{"v0.2.9", "v0.3.0", false},
		{"v0.4.0", "dev", false},
		{"v0.4.0-rc1", "v0.3.0", false},
		{"0.4.0", "v0.3.0", false},
	} {
		if got := newer(c.v, c.cur); got != c.want {
			t.Errorf("newer(%q, %q) = %v", c.v, c.cur, got)
		}
	}
}

func TestChecksum(t *testing.T) {
	sums := "aa11  hearsay.apk\nBB22  hearsay-linux-amd64.tar.gz\ncc33 *hearsay-windows-amd64.zip\n"
	if got := checksum(sums, "hearsay-linux-amd64.tar.gz"); got != "bb22" {
		t.Errorf("linux: %q", got)
	}
	if got := checksum(sums, "hearsay-windows-amd64.zip"); got != "cc33" {
		t.Errorf("windows: %q", got)
	}
	if got := checksum(sums, "hearsay"); got != "" {
		t.Errorf("missing: %q", got)
	}
}

func TestExtractBinary(t *testing.T) {
	var tgz bytes.Buffer
	gz := gzip.NewWriter(&tgz)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ name, body string }{
		{"hearsay-linux-amd64/README.md", "readme"},
		{"hearsay-linux-amd64/hearsay", "ELF"},
	} {
		tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(f.body))
	}
	tw.Close()
	gz.Close()
	if b, err := extractBinary("hearsay-linux-amd64.tar.gz", tgz.Bytes()); err != nil || string(b) != "ELF" {
		t.Errorf("tar.gz: %q, %v", b, err)
	}

	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	for _, f := range []struct{ name, body string }{
		{"hearsay-windows-amd64/LICENSE", "license"},
		{"hearsay-windows-amd64/hearsay.exe", "MZ"},
	} {
		w, _ := zw.Create(f.name)
		w.Write([]byte(f.body))
	}
	zw.Close()
	if b, err := extractBinary("hearsay-windows-amd64.zip", zb.Bytes()); err != nil || string(b) != "MZ" {
		t.Errorf("zip: %q, %v", b, err)
	}

	if _, err := extractBinary("hearsay-windows-amd64.zip", tgz.Bytes()); err == nil {
		t.Error("a tar.gz read as a zip should fail")
	}
}

func TestReplaceExe(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "hearsay")
	os.WriteFile(exe, []byte("old"), 0o755)
	if err := replaceExe(exe, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Errorf("got %q", b)
	}
	if fi, _ := os.Stat(exe); fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("not executable: %v", fi.Mode())
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".hearsay-update-*"))
	if len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

func TestPhoneAppState(t *testing.T) {
	t.Setenv("HEARSAY_RUN_DIR", t.TempDir())
	defer func(old string) { phoneApp = old }(phoneApp)

	phoneApp = "3"
	if p := phoneAppState(); p != nil {
		t.Errorf("before the phone said anything: %+v", p)
	}
	for _, c := range []struct {
		r    phoneReply
		want PhoneApp
	}{
		{phoneReply{}, PhoneApp{Outdated: true}}, // an app too old to say
		{phoneReply{App: 2, AppVersion: "0.3.0"}, PhoneApp{"0.3.0", true, false}},
		{phoneReply{App: 3, AppVersion: "0.4.0"}, PhoneApp{"0.4.0", false, false}},
		{phoneReply{App: 4, AppVersion: "0.5.0"}, PhoneApp{"0.5.0", false, true}},
	} {
		notePhoneApp(c.r)
		if p := phoneAppState(); p == nil || *p != c.want {
			t.Errorf("app %d: got %+v, want %+v", c.r.App, p, c.want)
		}
	}
	forgetPhoneApp()
	if p := phoneAppState(); p != nil {
		t.Errorf("after forgetting: %+v", p)
	}

	phoneApp = "" // a build that doesn't know
	notePhoneApp(phoneReply{App: 1})
	if p := phoneAppState(); p != nil {
		t.Errorf("without phoneApp: %+v", p)
	}
}
