package main

import "net/http"

// keepLocal ignores credentials from a response that belongs to an account
// selection which has since changed. Cookies are never sent to the window.
func keepLocal(st *store, version uint64, h http.Header, mutate bool) (bool, error) {
	lines := h.Values("Set-Cookie")
	h.Del("Set-Cookie")
	if len(lines) == 0 && !mutate {
		st.mu.Lock()
		defer st.mu.Unlock()
		return version == st.localVersion, nil
	}
	accepted := false
	err := st.update(func(s *state) {
		if version != st.localVersion {
			return
		}
		accepted = true
		for _, line := range lines {
			c, err := http.ParseSetCookie(line)
			if err != nil {
				continue
			}
			if c.Name == "atlas-vrc-local" {
				mutate = true
			}
			if c.Value == "" || c.MaxAge < 0 {
				delete(s.Local, c.Name)
			} else {
				s.Local[c.Name] = c.Value
			}
		}
		if mutate {
			st.localVersion++
			if st.localChanged != nil {
				close(st.localChanged)
			}
			st.localChanged = make(chan struct{})
		}
	})
	return accepted, err
}
