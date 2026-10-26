package server

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// checkUsername is only exposed by the local desktop API. It uses the same
// session, cookie rotation and rate-limit cooldown as other VRChat requests.
func (s *Server) checkUsername(w http.ResponseWriter, r *http.Request, session *Session) error {
	name := strings.TrimSpace(r.URL.Query().Get("username"))
	if name == "" || !utf8.ValidString(name) || len(name) > 128 || strings.ContainsFunc(name, unicode.IsControl) {
		return errStatus(http.StatusBadRequest, "Enter a username of at most 128 bytes without control characters.")
	}
	key := backoffKey(session)
	var data any
	var updates sessionUpdates
	var err error
	if s.backoff.coolingDown(serverKey, s.now()) {
		err = rateLimitedError()
	} else {
		data, updates, err = s.vrc(r.Context(), "/auth/exists?"+url.Values{"username": {name}}.Encode(), session, vrcRequest{})
	}
	if err != nil {
		var he *httpError
		if errors.As(err, &he) && he.status == http.StatusTooManyRequests {
			wait := s.backoff.remaining(key, s.now())
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int((wait+time.Second-1)/time.Second))))
		}
		return err
	}
	obj := asObject(data)
	exists, ok := obj["userExists"].(bool)
	if !ok {
		return errStatus(http.StatusBadGateway, "VRChat returned an unreadable username result.")
	}
	status := "unconfirmed"
	if exists {
		status = "taken"
	} else if valid, present := obj["nameOk"].(bool); present {
		if valid {
			status = "available"
		} else {
			status = "invalid"
		}
	}
	session.apply(updates)
	s.writeWithSession(w, map[string]string{"username": name, "status": status}, session)
	return nil
}
