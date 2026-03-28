package ss

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"smallx/internal/model"
)

type Service struct {
	logger *slog.Logger

	mu        sync.Mutex
	cfg       RuntimeConfig
	state     *serviceState
	tcpLn     net.Listener
	udpConn   *net.UDPConn
	closed    bool
	startedAt time.Time

	traffic *trafficStore
	online  *onlineTracker
	limits  *sessionLimiter

	udpMu       sync.Mutex
	udpSessions map[string]*udpSession
}

type serviceState struct {
	Config RuntimeConfig
	Cipher CipherDef
	Users  []*UserEntry
	Rules  *targetRules
}

type UserEntry struct {
	Config    UserConfig
	MasterKey []byte
}

type udpSession struct {
	user   *UserEntry
	client netip.AddrPort
	pc     net.PacketConn
	done   func()
}

type trafficStore struct {
	mu   sync.Mutex
	data map[int]*trafficValue
}

type trafficValue struct {
	up   int64
	down int64
}

type onlineTracker struct {
	mu   sync.Mutex
	ttl  time.Duration
	data map[int]map[string]time.Time
}

func NewService(logger *slog.Logger) *Service {
	return &Service{
		logger:      logger.With(slog.String("component", "ss-service")),
		startedAt:   time.Now(),
		traffic:     &trafficStore{data: make(map[int]*trafficValue)},
		online:      &onlineTracker{ttl: 5 * time.Minute, data: make(map[int]map[string]time.Time)},
		limits:      newSessionLimiter(),
		udpSessions: make(map[string]*udpSession),
	}
}

func (s *Service) Apply(cfg RuntimeConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("service is closed")
	}

	state, err := buildState(cfg)
	if err != nil {
		return err
	}

	restart := s.tcpLn == nil || s.udpConn == nil || needsRestart(s.cfg, cfg)
	s.state = state
	s.cfg = cfg

	if !restart {
		s.logger.Info("updated user state without listener restart",
			slog.Int("users", len(cfg.Users)),
			slog.String("cipher", cfg.Server.Cipher),
		)
		return nil
	}

	if err := s.restartLocked(); err != nil {
		return err
	}

	s.logger.Info("started ss listeners",
		slog.String("listen_ip", cfg.Server.ListenIP),
		slog.Int("port", cfg.Server.ServerPort),
		slog.String("cipher", cfg.Server.Cipher),
		slog.Int("users", len(cfg.Users)),
	)
	return nil
}

func (s *Service) Snapshot(_ context.Context) (model.RuntimeSnapshot, error) {
	return model.RuntimeSnapshot{
		Status: model.StatusReport{
			Uptime: int64(time.Since(s.startedAt).Seconds()),
		},
		Traffic:  s.traffic.snapshotAndReset(),
		AliveIPs: s.limits.SnapshotAliveIPs(),
	}, nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true

	var errs []error
	if s.tcpLn != nil {
		if err := s.tcpLn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
		s.tcpLn = nil
	}
	if s.udpConn != nil {
		if err := s.udpConn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
		s.udpConn = nil
	}
	s.closeUDPSessions()
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func buildState(cfg RuntimeConfig) (*serviceState, error) {
	if cfg.Server.Obfs.Enabled {
		return nil, fmt.Errorf("obfs is not supported by ss-native yet")
	}
	def, ok := LookupCipher(cfg.Server.Cipher)
	if !ok {
		return nil, fmt.Errorf("unsupported shadowsocks cipher: %s", cfg.Server.Cipher)
	}

	users := make([]*UserEntry, 0, len(cfg.Users))
	for _, user := range cfg.Users {
		masterKey := DeriveMasterKey(def, user.Password)
		users = append(users, &UserEntry{
			Config:    user,
			MasterKey: masterKey,
		})
	}

	rules, err := compileTargetRules(cfg)
	if err != nil {
		return nil, err
	}

	return &serviceState{
		Config: cfg,
		Cipher: def,
		Users:  users,
		Rules:  rules,
	}, nil
}

func needsRestart(oldCfg, newCfg RuntimeConfig) bool {
	return oldCfg.Server.ListenIP != newCfg.Server.ListenIP ||
		oldCfg.Server.ServerPort != newCfg.Server.ServerPort ||
		oldCfg.Server.Cipher != newCfg.Server.Cipher ||
		oldCfg.Server.EnableTCP != newCfg.Server.EnableTCP ||
		oldCfg.Server.EnableUDP != newCfg.Server.EnableUDP
}

func (s *Service) restartLocked() error {
	if s.tcpLn != nil {
		_ = s.tcpLn.Close()
		s.tcpLn = nil
	}
	if s.udpConn != nil {
		_ = s.udpConn.Close()
		s.udpConn = nil
	}
	s.closeUDPSessions()

	listenAddr := net.JoinHostPort(s.cfg.Server.ListenIP, fmt.Sprintf("%d", s.cfg.Server.ServerPort))

	if s.cfg.Server.EnableTCP {
		ln, err := net.Listen("tcp", listenAddr)
		if err != nil {
			return err
		}
		s.tcpLn = ln
		go s.serveTCP(ln)
	}

	if s.cfg.Server.EnableUDP {
		addr, err := net.ResolveUDPAddr("udp", listenAddr)
		if err != nil {
			return err
		}
		conn, err := net.ListenUDP("udp", addr)
		if err != nil {
			if s.tcpLn != nil {
				_ = s.tcpLn.Close()
				s.tcpLn = nil
			}
			return err
		}
		s.udpConn = conn
		go s.serveUDP(conn)
	}

	return nil
}

func (s *Service) serveTCP(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			s.logger.Warn("tcp accept failed", slog.Any("error", err))
			continue
		}
		go s.handleTCP(conn)
	}
}

func (s *Service) handleTCP(conn net.Conn) {
	defer conn.Close()

	state := s.currentState()
	if state == nil {
		return
	}
	if len(state.Users) == 0 {
		s.logger.Warn("tcp connection rejected because no users are loaded")
		return
	}

	reader := bufio.NewReader(conn)
	user, aead, nonce, firstPayload, err := identifyTCPUser(reader, state.Cipher, state.Users)
	if err != nil {
		s.logger.Warn("tcp handshake failed", slog.Any("error", err), slog.String("remote", conn.RemoteAddr().String()))
		return
	}

	target, headerLen, err := SplitAddr(firstPayload)
	if err != nil {
		s.logger.Warn("failed to parse target address", slog.Any("error", err))
		return
	}

	if err := state.Rules.Validate(target); err != nil {
		s.logger.Warn("tcp target rejected", slog.Any("error", err), slog.String("target", target))
		return
	}

	clientIP := RemoteIP(conn.RemoteAddr())
	release, err := s.limits.AcquireTCP(user.Config, clientIP, state.Config.Server.EnforceDeviceLimit)
	if err != nil {
		s.logger.Warn("tcp client rejected",
			slog.Any("error", err),
			slog.Int("user_id", user.Config.ID),
			slog.String("client_ip", clientIP),
		)
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			_ = tcpConn.SetLinger(0)
		}
		return
	}
	defer release()

	s.online.seen(user.Config.ID, clientIP)

	remote, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		s.logger.Warn("failed to connect target", slog.Any("error", err), slog.String("target", target))
		return
	}
	defer remote.Close()

	initialPayload := firstPayload[headerLen:]
	if len(initialPayload) > 0 {
		n, err := remote.Write(initialPayload)
		if err != nil {
			s.logger.Warn("failed to write initial payload", slog.Any("error", err))
			return
		}
		s.traffic.addUp(user.Config.ID, int64(n))
	}

	ssReader := NewEstablishedStreamReader(reader, aead, nonce)
	ssWriter := NewStreamWriter(conn, state.Cipher, user.MasterKey)

	upDone := make(chan error, 1)
	go func() {
		_, err := copyCount(remote, ssReader, func(n int) {
			s.traffic.addUp(user.Config.ID, int64(n))
		})
		if tcpConn, ok := remote.(*net.TCPConn); ok {
			_ = tcpConn.CloseWrite()
		}
		upDone <- err
	}()

	_, downErr := copyCount(ssWriter, remote, func(n int) {
		s.traffic.addDown(user.Config.ID, int64(n))
	})
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.CloseWrite()
	}

	upErr := <-upDone
	if downErr != nil && !errors.Is(downErr, io.EOF) {
		s.logger.Debug("tcp downstream copy finished", slog.Any("error", downErr))
	}
	if upErr != nil && !errors.Is(upErr, io.EOF) {
		s.logger.Debug("tcp upstream copy finished", slog.Any("error", upErr))
	}
}

func (s *Service) serveUDP(conn *net.UDPConn) {
	buf := make([]byte, 64*1024)
	for {
		n, clientAddr, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			s.logger.Warn("udp read failed", slog.Any("error", err))
			continue
		}

		packet := append([]byte(nil), buf[:n]...)
		go s.handleUDP(conn, clientAddr, packet)
	}
}

func (s *Service) handleUDP(serverConn *net.UDPConn, clientAddr netip.AddrPort, packet []byte) {
	state := s.currentState()
	if state == nil || len(state.Users) == 0 {
		return
	}

	user, plaintext, err := decryptUDPPacket(state.Cipher, state.Users, packet)
	if err != nil {
		s.logger.Debug("udp packet rejected", slog.Any("error", err))
		return
	}

	target, headerLen, err := SplitAddr(plaintext)
	if err != nil {
		s.logger.Debug("udp target parse failed", slog.Any("error", err))
		return
	}
	if err := state.Rules.Validate(target); err != nil {
		s.logger.Debug("udp target rejected", slog.Any("error", err), slog.String("target", target))
		return
	}
	payload := plaintext[headerLen:]
	if len(payload) == 0 {
		return
	}

	session, err := s.getOrCreateUDPSession(serverConn, state, user, clientAddr)
	if err != nil {
		s.logger.Warn("udp session create failed",
			slog.Any("error", err),
			slog.Int("user_id", user.Config.ID),
			slog.String("client_ip", clientAddr.Addr().String()),
		)
		return
	}

	s.online.seen(user.Config.ID, clientAddr.Addr().String())

	targetAddr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		s.logger.Debug("udp resolve target failed", slog.Any("error", err), slog.String("target", target))
		return
	}

	if _, err := session.pc.WriteTo(payload, targetAddr); err != nil {
		s.logger.Debug("udp forward failed", slog.Any("error", err))
		return
	}
	s.traffic.addUp(user.Config.ID, int64(len(payload)))
}

func (s *Service) getOrCreateUDPSession(serverConn *net.UDPConn, state *serviceState, user *UserEntry, clientAddr netip.AddrPort) (*udpSession, error) {
	key := fmt.Sprintf("%d|%s", user.Config.ID, clientAddr.String())

	s.udpMu.Lock()
	defer s.udpMu.Unlock()
	if session, ok := s.udpSessions[key]; ok {
		return session, nil
	}

	pc, err := net.ListenPacket("udp", "")
	if err != nil {
		return nil, err
	}
	done, err := s.limits.AcquireUDP(user.Config, clientAddr.Addr().String(), state.Config.Server.EnforceDeviceLimit)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}

	session := &udpSession{
		user:   user,
		client: clientAddr,
		pc:     pc,
		done:   done,
	}
	s.udpSessions[key] = session

	go s.serveUDPSession(key, serverConn, state.Cipher, session)
	return session, nil
}

func (s *Service) serveUDPSession(key string, serverConn *net.UDPConn, def CipherDef, session *udpSession) {
	defer func() {
		if session.done != nil {
			session.done()
		}
		_ = session.pc.Close()
		s.udpMu.Lock()
		delete(s.udpSessions, key)
		s.udpMu.Unlock()
	}()

	buf := make([]byte, 64*1024)
	for {
		_ = session.pc.SetReadDeadline(time.Now().Add(5 * time.Minute))
		n, addr, err := session.pc.ReadFrom(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return
			}
			return
		}

		addrBytes, err := AddrFromNet(addr)
		if err != nil {
			continue
		}
		plaintext := append(addrBytes, buf[:n]...)
		encrypted, err := encryptUDPPacket(def, session.user.MasterKey, plaintext)
		if err != nil {
			continue
		}
		if _, err := serverConn.WriteToUDPAddrPort(encrypted, session.client); err != nil {
			continue
		}
		s.traffic.addDown(session.user.Config.ID, int64(n))
	}
}

func (s *Service) closeUDPSessions() {
	s.udpMu.Lock()
	defer s.udpMu.Unlock()
	for key, session := range s.udpSessions {
		_ = session.pc.Close()
		delete(s.udpSessions, key)
	}
}

func (s *Service) currentState() *serviceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func decryptUDPPacket(def CipherDef, users []*UserEntry, packet []byte) (*UserEntry, []byte, error) {
	for _, user := range users {
		plaintext, err := decryptUDPPacketForUser(def, user.MasterKey, packet)
		if err == nil {
			return user, plaintext, nil
		}
	}
	return nil, nil, fmt.Errorf("unable to decrypt udp packet")
}

func decryptUDPPacketForUser(def CipherDef, masterKey, packet []byte) ([]byte, error) {
	saltSize := def.SaltSize()
	if len(packet) < saltSize+def.Overhead() {
		return nil, fmt.Errorf("packet too short")
	}
	salt := packet[:saltSize]
	aead, err := DeriveSessionAEAD(def, masterKey, salt)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	return aead.Open(nil, nonce, packet[saltSize:], nil)
}

func encryptUDPPacket(def CipherDef, masterKey, plaintext []byte) ([]byte, error) {
	salt := make([]byte, def.SaltSize())
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	aead, err := DeriveSessionAEAD(def, masterKey, salt)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	sealed := aead.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(salt)+len(sealed))
	out = append(out, salt...)
	out = append(out, sealed...)
	return out, nil
}

func copyCount(dst io.Writer, src io.Reader, onCount func(int)) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			total += int64(nw)
			if nw > 0 {
				onCount(nw)
			}
			if ew != nil {
				return total, ew
			}
			if nw != nr {
				return total, io.ErrShortWrite
			}
		}
		if er != nil {
			if errors.Is(er, io.EOF) {
				return total, nil
			}
			return total, er
		}
	}
}

func (t *trafficStore) addUp(userID int, value int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	item := t.ensure(userID)
	item.up += value
}

func (t *trafficStore) addDown(userID int, value int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	item := t.ensure(userID)
	item.down += value
}

func (t *trafficStore) ensure(userID int) *trafficValue {
	item, ok := t.data[userID]
	if !ok {
		item = &trafficValue{}
		t.data[userID] = item
	}
	return item
}

func (t *trafficStore) snapshotAndReset() []model.TrafficReport {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]model.TrafficReport, 0, len(t.data))
	for userID, item := range t.data {
		if item.up == 0 && item.down == 0 {
			continue
		}
		out = append(out, model.TrafficReport{
			ID: userID,
			U:  item.up,
			D:  item.down,
		})
		item.up = 0
		item.down = 0
	}
	return out
}

func (o *onlineTracker) seen(userID int, ip string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.data[userID]; !ok {
		o.data[userID] = make(map[string]time.Time)
	}
	o.data[userID][ip] = time.Now()
}

func (o *onlineTracker) snapshot() []model.AliveIP {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := time.Now()
	out := make([]model.AliveIP, 0, len(o.data))
	for userID, items := range o.data {
		ips := make([]string, 0, len(items))
		for ip, ts := range items {
			if now.Sub(ts) > o.ttl {
				delete(items, ip)
				continue
			}
			ips = append(ips, ip)
		}
		if len(ips) > 0 {
			out = append(out, model.AliveIP{
				ID:  userID,
				IPs: ips,
			})
		}
		if len(items) == 0 {
			delete(o.data, userID)
		}
	}
	return out
}
