package ss

import "testing"

func TestSessionLimiterTCPConnLimit(t *testing.T) {
	limiter := newSessionLimiter()
	user := UserConfig{ID: 1, TCPConnLimit: 1}

	release, err := limiter.AcquireTCP(user, "1.1.1.1", false)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer release()

	if _, err := limiter.AcquireTCP(user, "1.1.1.1", false); err == nil {
		t.Fatalf("expected tcp limit error")
	}
}

func TestSessionLimiterDeviceLimit(t *testing.T) {
	limiter := newSessionLimiter()
	user := UserConfig{ID: 1, DeviceLimit: 2}

	r1, err := limiter.AcquireUDP(user, "1.1.1.1", true)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer r1()
	r2, err := limiter.AcquireUDP(user, "2.2.2.2", true)
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	defer r2()

	if _, err := limiter.AcquireUDP(user, "3.3.3.3", true); err == nil {
		t.Fatalf("expected device limit error")
	}
}

func TestSessionLimiterSpeedLimitSync(t *testing.T) {
	limiter := newSessionLimiter()
	limiter.SyncUsers([]UserConfig{
		{ID: 1, SpeedLimit: 1},
	})

	bucket := limiter.GetSpeedLimiter(1)
	if bucket == nil {
		t.Fatalf("expected speed limiter to exist")
	}
	if bucket.Burst() < 64*1024 {
		t.Fatalf("expected burst to be at least 64k, got %d", bucket.Burst())
	}

	limiter.SyncUsers([]UserConfig{
		{ID: 1, SpeedLimit: 0},
	})
	if limiter.GetSpeedLimiter(1) != nil {
		t.Fatalf("expected speed limiter to be removed when speed limit is 0")
	}
}
