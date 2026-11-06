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
// multicast, so the first query often goes unanswered (CONNECTION-HANDOFF.md, Phase A).
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

	type instance struct {
		srv    *dnsmessage.SRVResource
		fp     string
		source net.IP // who answered, for when the A record is missing
	}
	instances := map[string]*instance{}
	names := map[string]dnsmessage.Name{}
	hosts := map[string]net.IP{}
	var found []phoneService
	done := map[string]bool{}

	end := time.Now().Add(timeout)
	next := time.Now()
	buf := make([]byte, 9000)
	for time.Now().Before(end) {
		if !time.Now().Before(next) {
			// Ask for the services, then for whatever is still missing about each one.
			qs := []dnsmessage.Question{{Name: serviceName, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET}}
			for key, in := range instances {
				if in.srv == nil {
					qs = append(qs, dnsmessage.Question{Name: names[key], Type: dnsmessage.TypeSRV, Class: dnsmessage.ClassINET},
						dnsmessage.Question{Name: names[key], Type: dnsmessage.TypeTXT, Class: dnsmessage.ClassINET})
				} else if hosts[key2(in.srv.Target)] == nil {
					qs = append(qs, dnsmessage.Question{Name: in.srv.Target, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET})
				}
			}
			if q, err := mdnsQuery(qs); err == nil {
				conn.WriteTo(q, mdnsGroup)
			}
			next = next.Add(time.Second)
		}
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		var p dnsmessage.Parser
		h, err := p.Start(buf[:n])
		if err != nil || !h.Response {
			continue
		}
		p.SkipAllQuestions()
		record := func(hdr dnsmessage.ResourceHeader) {
			key := key2(hdr.Name)
			switch hdr.Type {
			case dnsmessage.TypePTR:
				r, err := p.PTRResource()
				if err == nil && key == key2(serviceName) && instances[key2(r.PTR)] == nil {
					instances[key2(r.PTR)] = &instance{}
					names[key2(r.PTR)] = r.PTR
				}
				return
			case dnsmessage.TypeSRV:
				if r, err := p.SRVResource(); err == nil && strings.HasSuffix(key, "._hearsay._tcp.local.") {
					in := instances[key]
					if in == nil {
						in = &instance{}
						instances[key], names[key] = in, hdr.Name
					}
					in.srv, in.source = &r, from.IP
				}
				return
			case dnsmessage.TypeTXT:
				if r, err := p.TXTResource(); err == nil {
					for _, kv := range r.TXT {
						if v, ok := strings.CutPrefix(kv, "fp="); ok {
							if instances[key] == nil {
								instances[key], names[key] = &instance{}, hdr.Name
							}
							instances[key].fp = v
						}
					}
				}
				return
			case dnsmessage.TypeA:
				if r, err := p.AResource(); err == nil {
					hosts[key] = net.IP(r.A[:])
				}
				return
			}
			p.SkipAnswer()
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

		for key, in := range instances {
			if done[key] || in.srv == nil || in.fp == "" {
				continue
			}
			ip := hosts[key2(in.srv.Target)]
			if ip == nil {
				ip = in.source
			}
			s := phoneService{
				name: strings.TrimSuffix(names[key].String(), "._hearsay._tcp.local."),
				addr: net.JoinHostPort(ip.String(), strconv.Itoa(int(in.srv.Port))),
				fp:   in.fp,
			}
			// Unescape the instance name's spaces etc. (RFC 6763 allows any UTF-8).
			s.name = strings.ReplaceAll(s.name, `\ `, " ")
			done[key] = true
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
