package ss

import (
	"fmt"
	"sort"
	"sync"

	"smallx/internal/model"
	"golang.org/x/time/rate"
)

type sessionLimiter struct {
	mu           sync.Mutex
	tcpCounts    map[int]int
	ipRefs       map[int]map[string]int
	speedLimiter map[int]*rate.Limiter
}

func newSessionLimiter() *sessionLimiter {
	return &sessionLimiter{
		tcpCounts:    make(map[int]int),
		ipRefs:       make(map[int]map[string]int),
		speedLimiter: make(map[int]*rate.Limiter),
	}
}

func (l *sessionLimiter) AcquireTCP(user UserConfig, ip string, enforceDeviceLimit bool) (func(), error) {
	return l.acquire(user, ip, true, enforceDeviceLimit)
}

func (l *sessionLimiter) AcquireUDP(user UserConfig, ip string, enforceDeviceLimit bool) (func(), error) {
	return l.acquire(user, ip, false, enforceDeviceLimit)
}

func (l *sessionLimiter) acquire(user UserConfig, ip string, tcp bool, enforceDeviceLimit bool) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if tcp && user.TCPConnLimit > 0 && l.tcpCounts[user.ID] >= user.TCPConnLimit {
		return nil, fmt.Errorf("tcp connection limit reached")
	}

	if enforceDeviceLimit && user.DeviceLimit > 0 {
		refs := l.ipRefs[user.ID]
		if refs == nil {
			refs = make(map[string]int)
			l.ipRefs[user.ID] = refs
		}
		if refs[ip] == 0 && len(refs) >= user.DeviceLimit {
			return nil, fmt.Errorf("device/ip limit reached")
		}
		refs[ip]++
	} else {
		if _, ok := l.ipRefs[user.ID]; !ok {
			l.ipRefs[user.ID] = make(map[string]int)
		}
		l.ipRefs[user.ID][ip]++
	}

	if tcp {
		l.tcpCounts[user.ID]++
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			l.release(user, ip, tcp)
		})
	}, nil
}

func (l *sessionLimiter) release(user UserConfig, ip string, tcp bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if tcp {
		if l.tcpCounts[user.ID] > 0 {
			l.tcpCounts[user.ID]--
		}
		if l.tcpCounts[user.ID] == 0 {
			delete(l.tcpCounts, user.ID)
		}
	}

	refs := l.ipRefs[user.ID]
	if refs == nil {
		return
	}
	if refs[ip] > 1 {
		refs[ip]--
	} else {
		delete(refs, ip)
	}
	if len(refs) == 0 {
		delete(l.ipRefs, user.ID)
	}
}

func (l *sessionLimiter) SnapshotAliveIPs() []model.AliveIP {
	l.mu.Lock()
	defer l.mu.Unlock()

	userIDs := make([]int, 0, len(l.ipRefs))
	for userID := range l.ipRefs {
		userIDs = append(userIDs, userID)
	}
	sort.Ints(userIDs)

	out := make([]model.AliveIP, 0, len(userIDs))
	for _, userID := range userIDs {
		refs := l.ipRefs[userID]
		if len(refs) == 0 {
			continue
		}
		ips := make([]string, 0, len(refs))
		for ip := range refs {
			ips = append(ips, ip)
		}
		sort.Strings(ips)
		out = append(out, model.AliveIP{
			ID:  userID,
			IPs: ips,
		})
	}
	return out
}

func (l *sessionLimiter) SyncUsers(users []UserConfig) {
	l.mu.Lock()
	defer l.mu.Unlock()

	seen := make(map[int]struct{}, len(users))
	for _, user := range users {
		seen[user.ID] = struct{}{}

		bytesPerSecond := speedLimitToBytes(user.SpeedLimit)
		if bytesPerSecond <= 0 {
			delete(l.speedLimiter, user.ID)
			continue
		}

		burst := bytesPerSecond
		if burst < 64*1024 {
			burst = 64 * 1024
		}

		if limiter, ok := l.speedLimiter[user.ID]; ok {
			limiter.SetLimit(rate.Limit(bytesPerSecond))
			limiter.SetBurst(burst)
			continue
		}
		l.speedLimiter[user.ID] = rate.NewLimiter(rate.Limit(bytesPerSecond), burst)
	}

	for userID := range l.speedLimiter {
		if _, ok := seen[userID]; !ok {
			delete(l.speedLimiter, userID)
		}
	}
}

func (l *sessionLimiter) GetSpeedLimiter(userID int) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.speedLimiter[userID]
}

func speedLimitToBytes(speedLimitMbps int) int {
	if speedLimitMbps <= 0 {
		return 0
	}
	return (speedLimitMbps * 1000 * 1000) / 8
}
