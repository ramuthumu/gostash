package archive

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestGuardedDialBlocksInternal(t *testing.T) {
	addrs := []string{
		"127.0.0.1:80",
		"[::1]:80",
		"169.254.169.254:80",
		"10.0.0.1:80",
		"192.168.1.1:80",
		"172.16.0.1:80",
		"0.0.0.0:80",
	}
	for _, addr := range addrs {
		t.Run(addr, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, err := GuardedDial(ctx, "tcp", addr)
			if err == nil {
				if conn != nil {
					_ = conn.Close()
				}
				t.Fatalf("GuardedDial(%q) returned nil error; expected refusal", addr)
			}
			if conn != nil {
				_ = conn.Close()
				t.Errorf("GuardedDial(%q) returned non-nil conn alongside error", addr)
			}
			if !strings.Contains(err.Error(), "internal address") {
				t.Errorf("GuardedDial(%q) error = %q; want substring \"internal address\"", addr, err.Error())
			}
		})
	}
}
