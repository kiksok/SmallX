package ss

import (
	"fmt"
	"sync"
)

type sessionLimiter struct {
	mu        sync.Mutex
	tcpCounts map[int]int
	ipRefs    map[int]map[string]int
}

func newSessionLimiter() *sessionLimiter {
	return &sessionLimiter{
		tcpCounts: make(map[int]int),
		ipRefs:    make(map[int]map[string]int),
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
