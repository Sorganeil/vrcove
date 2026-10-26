package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/zalando/go-keyring"
)

const keyringService = "VRCove Desktop"

// state is everything the app keeps between runs. It is written encrypted
// with a key held in the system keyring (Windows Credential Manager or the
// Secret Service on Linux), so the file alone reveals nothing.
type state struct {
	// Local holds this device's VRChat session cookies, already sealed by the
	// local API with SessionKey. Remote holds the VRCove website's cookies.
	Local  map[string]string `json:"local"`
	Remote map[string]string `json:"remote"`
	Notify notifyPrefs       `json:"notify"`
}

// notifyPrefs are the desktop notification settings. Text holds the alert
// wording in the window's language, sent by the window, since translations
// live in the frontend.
type notifyPrefs struct {
	Enabled bool              `json:"enabled"`
	Kinds   []string          `json:"kinds"`
	Text    map[string]string `json:"text,omitempty"`
}

type store struct {
	mu   sync.Mutex
	path string
	// fileKey seals the state file; SessionKey seals VRChat sessions.
	fileKey, SessionKey []byte
	// persistent is false when the keyring is unavailable; the app then
	// works but forgets sign-ins when it closes.
	persistent bool
	data       state
}

func openStore(dir string) (*store, error) {
	s := &store{path: filepath.Join(dir, "state.bin"), data: state{Local: map[string]string{}, Remote: map[string]string{}}}
	keys, err := keyring.Get(keyringService, "keys")
	raw, decodeErr := base64.StdEncoding.DecodeString(keys)
	if err != nil || decodeErr != nil || len(raw) != 64 {
		raw = make([]byte, 64)
		rand.Read(raw)
		if errors.Is(err, keyring.ErrNotFound) && keyring.Set(keyringService, "keys", base64.StdEncoding.EncodeToString(raw)) == nil {
			s.persistent = true
			os.Remove(s.path) // Sealed with a key that no longer exists.
		}
	} else {
		s.persistent = true
	}
	s.fileKey, s.SessionKey = raw[:32], raw[32:]
	if !s.persistent {
		return s, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	sealed, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(s.fileKey)
	if err != nil || len(sealed) < gcm.NonceSize() {
		return s, nil
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	if err == nil {
		json.Unmarshal(plain, &s.data)
	}
	if s.data.Local == nil {
		s.data.Local = map[string]string{}
	}
	if s.data.Remote == nil {
		s.data.Remote = map[string]string{}
	}
	return s, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// update changes the state under the lock and saves it.
func (s *store) update(change func(*state)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(&s.data)
	if !s.persistent {
		return nil
	}
	plain, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	gcm, err := newGCM(s.fileKey)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, gcm.Seal(nonce, nonce, plain, nil), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// notify returns the notification settings.
func (s *store) notify() notifyPrefs {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Notify
}

// cookies returns a copy of one cookie set.
func (s *store) cookies(remote bool) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	src := s.data.Local
	if remote {
		src = s.data.Remote
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
