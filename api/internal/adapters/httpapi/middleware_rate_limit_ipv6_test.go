package httpapi

import (
	"fmt"
	"net/netip"
	"testing"
	"time"
)

func ipv6Host(prefix string, index int) netip.Addr {
	return netip.MustParseAddr(fmt.Sprintf("%s::%x", prefix, index+1))
}

func TestRateLimitIPv6ClientsShareSubnetBucket(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	for i := 0; i < rateLimitRequests; i++ {
		if _, ok := limiter.allow(ipv6Host("2001:db8:1:1", i)); !ok {
			t.Fatalf("request %d rejected early", i+1)
		}
	}
	if _, ok := limiter.allow(ipv6Host("2001:db8:1:1", rateLimitRequests)); ok {
		t.Fatalf("request %d from one IPv6 /64 was allowed", rateLimitRequests+1)
	}
}

func TestRateLimitIPv6SubnetOccupiesOneClientSlot(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	for i := 0; i < rateLimitMaxClients; i++ {
		limiter.allow(ipv6Host("2001:db8:1:1", i))
	}
	if len(limiter.clients) != 1 {
		t.Fatalf("one IPv6 /64 occupies %d client slots", len(limiter.clients))
	}
	if _, ok := limiter.allow(netip.MustParseAddr("203.0.113.50")); !ok {
		t.Fatalf("unrelated IPv4 client rejected after %d requests from one IPv6 /64", rateLimitMaxClients)
	}
}

func TestRateLimitIPv6SubnetsAreIndependent(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	for i := 0; i < rateLimitRequests; i++ {
		limiter.allow(ipv6Host("2001:db8:1:1", i))
	}
	if _, ok := limiter.allow(ipv6Host("2001:db8:1:1", rateLimitRequests)); ok {
		t.Fatal("exhausted /64 accepted another request")
	}
	if _, ok := limiter.allow(ipv6Host("2001:db8:1:2", 0)); !ok {
		t.Fatal("address in a different /64 rejected")
	}
}

func TestRateLimitIPv4ClientsStayPerAddress(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	for i := 0; i < rateLimitRequests; i++ {
		if _, ok := limiter.allow(netip.MustParseAddr("203.0.113.1")); !ok {
			t.Fatalf("request %d rejected early", i+1)
		}
	}
	if _, ok := limiter.allow(netip.MustParseAddr("203.0.113.1")); ok {
		t.Fatalf("request %d from one IPv4 address was allowed", rateLimitRequests+1)
	}
	if _, ok := limiter.allow(netip.MustParseAddr("203.0.113.2")); !ok {
		t.Fatal("different IPv4 address rejected")
	}
}
