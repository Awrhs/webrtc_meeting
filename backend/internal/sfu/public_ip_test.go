package sfu

import (
	"errors"
	"net"
	"reflect"
	"testing"

	"go.uber.org/zap"
)

func TestResolvePublicIPs(t *testing.T) {
	lookup := func(host string) ([]net.IP, error) {
		switch host {
		case "meeting.lan":
			return []net.IP{net.ParseIP("fe80::1"), net.ParseIP("192.168.1.10")}, nil
		case "v6only.lan":
			return []net.IP{net.ParseIP("fe80::2")}, nil
		default:
			return nil, errors.New("no such host")
		}
	}
	got := resolvePublicIPs(
		[]string{"203.0.113.7", "meeting.lan", "192.168.1.10", "nope.invalid", "v6only.lan"},
		lookup, zap.NewNop())
	want := []string{"203.0.113.7", "192.168.1.10"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolvePublicIPs = %v, want %v", got, want)
	}
}
