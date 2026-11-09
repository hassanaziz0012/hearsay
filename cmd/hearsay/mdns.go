package main

import (
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// phoneService is a phone advertising PhoneServer (_hearsay._tcp).
type phoneService struct {
	name string // e.g. "Hearsay on Pixel 9"
	addr string // host:port
	fp   string // first 16 hex digits of the certificate's SHA-256 (TXT "fp")
}

var (
	mdnsGroup   = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
	serviceName = dnsmessage.MustNewName("_hearsay._tcp.local.")
)

// browse looks for phones over mDNS until timeout, or until one that want accepts has
// been found. It asks again every second: a phone with its screen off drops some
// multicast, so the first query often goes unanswered.
func browse(timeout time.Duration, want func(phoneService) bool) []phoneService {
	// Port 5353 gets the multicast answers. If something holds it exclusively, a query
	// from any other port asks for direct ("legacy unicast") answers instead.
	conn, err := net.ListenMulticastUDP("udp4", nil, mdnsGroup)
	if err != nil {
		if conn, err = net.ListenUDP("udp4", nil); err != nil {
			return nil
		}
	}
	defer conn.Close()

	b := newBrowser()
	var found []phoneService
	end := time.Now().Add(timeout)
	next := time.Now()
	buf := make([]byte, 9000)
	for time.Now().Before(end) {
		if !time.Now().Before(next) {
			if q, err := mdnsQuery(b.questions()); err == nil {
				conn.WriteTo(q, mdnsGroup)
			}
			next = next.Add(time.Second)
		}
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		b.parse(buf[:n], from.IP)
		for _, s := range b.complete() {
			found = append(found, s)
			if want != nil && want(s) {
				return []phoneService{s}
			}
		}
	}
	if want != nil {
		return nil
	}
	return found
}

// browser collects what mDNS answers say about _hearsay._tcp instances until each has its
// port, fingerprint and address.
type browser struct {
	instances map[string]*instance // by key2 of the instance name
	names     map[string]dnsmessage.Name
	hosts     map[string]net.IP // A records, by key2 of the host name
	done      map[string]bool   // already returned by complete
}

type instance struct {
	srv    *dnsmessage.SRVResource
	fp     string
	source net.IP // who answered, for when the A record is missing
}

func newBrowser() *browser {
	return &browser{instances: map[string]*instance{}, names: map[string]dnsmessage.Name{},
		hosts: map[string]net.IP{}, done: map[string]bool{}}
}

// questions asks for the services, then for whatever is still missing about each one.
func (b *browser) questions() []dnsmessage.Question {
	qs := []dnsmessage.Question{{Name: serviceName, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET}}
	for key, in := range b.instances {
		if in.srv == nil {
			qs = append(qs, dnsmessage.Question{Name: b.names[key], Type: dnsmessage.TypeSRV, Class: dnsmessage.ClassINET},
				dnsmessage.Question{Name: b.names[key], Type: dnsmessage.TypeTXT, Class: dnsmessage.ClassINET})
		} else if b.hosts[key2(in.srv.Target)] == nil {
			qs = append(qs, dnsmessage.Question{Name: in.srv.Target, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET})
		}
	}
	return qs
}

// instance returns the instance called name, adding it if it is new.
func (b *browser) instance(name dnsmessage.Name) *instance {
	key := key2(name)
	in := b.instances[key]
	if in == nil {
		in = &instance{}
		b.instances[key], b.names[key] = in, name
	}
	return in
}

// parse takes in the records of one mDNS response from the address from.
func (b *browser) parse(msg []byte, from net.IP) {
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil || !h.Response {
		return
	}
	p.SkipAllQuestions()
	record := func(hdr dnsmessage.ResourceHeader) {
		key := key2(hdr.Name)
		switch hdr.Type {
		case dnsmessage.TypePTR:
			if r, err := p.PTRResource(); err == nil && key == key2(serviceName) {
				b.instance(r.PTR)
			}
		case dnsmessage.TypeSRV:
			if r, err := p.SRVResource(); err == nil && strings.HasSuffix(key, "._hearsay._tcp.local.") {
				in := b.instance(hdr.Name)
				in.srv, in.source = &r, from
			}
		case dnsmessage.TypeTXT:
			if r, err := p.TXTResource(); err == nil && strings.HasSuffix(key, "._hearsay._tcp.local.") {
				for _, kv := range r.TXT {
					if v, ok := strings.CutPrefix(kv, "fp="); ok {
						b.instance(hdr.Name).fp = v
					}
				}
			}
		case dnsmessage.TypeA:
			if r, err := p.AResource(); err == nil {
				b.hosts[key] = net.IP(r.A[:])
			}
		default:
			p.SkipAnswer()
		}
	}
	for {
		hdr, err := p.AnswerHeader()
		if err != nil {
			break
		}
		record(hdr)
	}
	p.SkipAllAuthorities()
	for {
		hdr, err := p.AdditionalHeader()
		if err != nil {
			break
		}
		// The resource methods read whichever section the last header came from.
		switch hdr.Type {
		case dnsmessage.TypePTR, dnsmessage.TypeSRV, dnsmessage.TypeTXT, dnsmessage.TypeA:
			record(hdr)
		default:
			p.SkipAdditional()
		}
	}
}

// complete returns the instances that have become complete since the last call.
func (b *browser) complete() []phoneService {
	var out []phoneService
	for key, in := range b.instances {
		if b.done[key] || in.srv == nil || in.fp == "" {
			continue
		}
		ip := b.hosts[key2(in.srv.Target)]
		if ip == nil {
			ip = in.source
		}
		b.done[key] = true
		// Name.String() gives the label's raw bytes (spaces and all), not escapes.
		out = append(out, phoneService{
			name: strings.TrimSuffix(b.names[key].String(), "._hearsay._tcp.local."),
			addr: net.JoinHostPort(ip.String(), strconv.Itoa(int(in.srv.Port))),
			fp:   in.fp,
		})
	}
	return out
}

// key2 is a DNS name as a map key: names are case-insensitive.
func key2(n dnsmessage.Name) string { return strings.ToLower(n.String()) }

func mdnsQuery(qs []dnsmessage.Question) ([]byte, error) {
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{})
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	for _, q := range qs {
		if err := b.Question(q); err != nil {
			return nil, err
		}
	}
	return b.Finish()
}
