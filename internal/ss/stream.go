package ss

import (
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

const maxPayloadSize = 0x3FFF

type StreamReader struct {
	src      io.Reader
	def      CipherDef
	masterKey []byte
	aead     cipher.AEAD
	nonce    []byte
	leftover []byte
	buf      []byte
	initFn   func() error
}

type StreamWriter struct {
	dst       io.Writer
	def       CipherDef
	masterKey []byte
	aead      cipher.AEAD
	nonce     []byte
	ready     bool
}

func NewClientStreamReader(src io.Reader, def CipherDef, masterKey []byte) *StreamReader {
	r := &StreamReader{
		src:       src,
		def:       def,
		masterKey: append([]byte(nil), masterKey...),
	}
	r.initFn = r.initFromSalt
	return r
}

func NewEstablishedStreamReader(src io.Reader, aead cipher.AEAD, nonce []byte) *StreamReader {
	return &StreamReader{
		src:    src,
		aead:   aead,
		nonce:  append([]byte(nil), nonce...),
		buf:    make([]byte, maxPayloadSize+aead.Overhead()),
		initFn: func() error { return nil },
	}
}

func NewStreamWriter(dst io.Writer, def CipherDef, masterKey []byte) *StreamWriter {
	return &StreamWriter{
		dst:       dst,
		def:       def,
		masterKey: append([]byte(nil), masterKey...),
	}
}

func (r *StreamReader) Read(p []byte) (int, error) {
	if len(r.leftover) > 0 {
		n := copy(p, r.leftover)
		r.leftover = r.leftover[n:]
		return n, nil
	}
	if err := r.initFn(); err != nil {
		return 0, err
	}
	n, err := r.readChunk()
	if err != nil {
		return 0, err
	}
	copied := copy(p, r.buf[:n])
	if copied < n {
		r.leftover = append(r.leftover[:0], r.buf[copied:n]...)
	}
	return copied, nil
}

func (w *StreamWriter) Write(p []byte) (int, error) {
	if err := w.ensureReady(); err != nil {
		return 0, err
	}

	written := 0
	overhead := w.aead.Overhead()
	for len(p) > 0 {
		size := len(p)
		if size > maxPayloadSize {
			size = maxPayloadSize
		}
		chunk := p[:size]
		sizeBuf := []byte{byte(size >> 8), byte(size)}
		encSize := w.aead.Seal(nil, w.nonce, sizeBuf, nil)
		incrementNonce(w.nonce)
		encPayload := w.aead.Seal(nil, w.nonce, chunk, nil)
		incrementNonce(w.nonce)

		frame := make([]byte, 0, 2+overhead+len(chunk)+overhead)
		frame = append(frame, encSize...)
		frame = append(frame, encPayload...)
		if _, err := w.dst.Write(frame); err != nil {
			return written, err
		}

		written += size
		p = p[size:]
	}
	return written, nil
}

func (r *StreamReader) initFromSalt() error {
	salt := make([]byte, r.def.SaltSize())
	if _, err := io.ReadFull(r.src, salt); err != nil {
		return err
	}
	aead, err := DeriveSessionAEAD(r.def, r.masterKey, salt)
	if err != nil {
		return err
	}
	r.aead = aead
	r.nonce = make([]byte, aead.NonceSize())
	r.buf = make([]byte, maxPayloadSize+aead.Overhead())
	r.initFn = func() error { return nil }
	return nil
}

func (r *StreamReader) readChunk() (int, error) {
	overhead := r.aead.Overhead()
	sizeFrame := make([]byte, 2+overhead)
	if _, err := io.ReadFull(r.src, sizeFrame); err != nil {
		return 0, err
	}
	sizeRaw, err := r.aead.Open(nil, r.nonce, sizeFrame, nil)
	if err != nil {
		return 0, err
	}
	incrementNonce(r.nonce)
	if len(sizeRaw) != 2 {
		return 0, fmt.Errorf("invalid size frame")
	}

	size := (int(sizeRaw[0])<<8 | int(sizeRaw[1])) & maxPayloadSize
	payloadFrame := make([]byte, size+overhead)
	if _, err := io.ReadFull(r.src, payloadFrame); err != nil {
		return 0, err
	}
	payload, err := r.aead.Open(r.buf[:0], r.nonce, payloadFrame, nil)
	if err != nil {
		return 0, err
	}
	incrementNonce(r.nonce)
	copy(r.buf, payload)
	return len(payload), nil
}

func (w *StreamWriter) ensureReady() error {
	if w.ready {
		return nil
	}
	salt := make([]byte, w.def.SaltSize())
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}
	aead, err := DeriveSessionAEAD(w.def, w.masterKey, salt)
	if err != nil {
		return err
	}
	if _, err := w.dst.Write(salt); err != nil {
		return err
	}
	w.aead = aead
	w.nonce = make([]byte, aead.NonceSize())
	w.ready = true
	return nil
}

func identifyTCPUser(src io.Reader, def CipherDef, users []*UserEntry) (*UserEntry, cipher.AEAD, []byte, []byte, error) {
	salt := make([]byte, def.SaltSize())
	if _, err := io.ReadFull(src, salt); err != nil {
		return nil, nil, nil, nil, err
	}

	sizeFrame := make([]byte, 2+def.Overhead())
	if _, err := io.ReadFull(src, sizeFrame); err != nil {
		return nil, nil, nil, nil, err
	}

	for _, user := range users {
		aead, err := DeriveSessionAEAD(def, user.MasterKey, salt)
		if err != nil {
			continue
		}
		nonce := make([]byte, aead.NonceSize())
		sizeRaw, err := aead.Open(nil, nonce, sizeFrame, nil)
		if err != nil {
			continue
		}
		if len(sizeRaw) != 2 {
			continue
		}
		size := (int(sizeRaw[0])<<8 | int(sizeRaw[1])) & maxPayloadSize
		payloadFrame := make([]byte, size+aead.Overhead())
		if _, err := io.ReadFull(src, payloadFrame); err != nil {
			return nil, nil, nil, nil, err
		}
		incrementNonce(nonce)
		payload, err := aead.Open(nil, nonce, payloadFrame, nil)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to decrypt first payload frame")
		}
		incrementNonce(nonce)
		return user, aead, nonce, payload, nil
	}

	return nil, nil, nil, nil, fmt.Errorf("unable to identify shadowsocks user")
}

func incrementNonce(nonce []byte) {
	for i := range nonce {
		nonce[i]++
		if nonce[i] != 0 {
			return
		}
	}
}
