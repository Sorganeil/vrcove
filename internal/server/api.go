package server

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

var (
	// Legacy 10-character user IDs still exist, e.g. for older world authors.
	userID         = regexp.MustCompile(`^(?:usr_[a-f0-9-]{36}|[A-Za-z0-9]{10})$`)
	groupID        = regexp.MustCompile(`^grp_[a-f0-9-]{36}$`)
	favoriteID     = regexp.MustCompile(`^fvrt_[a-f0-9-]{36}$`)
	notificationID = regexp.MustCompile(`^not_[a-zA-Z0-9_-]{10,100}$`)
	favoriteGroup  = regexp.MustCompile(`^avatars[1-9][0-9]*$`)
	twoFactorCode  = regexp.MustCompile(`^[a-zA-Z0-9-]{6,32}$`)
)

const (
	pendingLifetime = 10 * time.Minute
	sessionLifetime = 12 * time.Hour
)

// jsLength counts UTF-16 code units, matching the frontend's length limits.
func jsLength(s string) int { return len(utf16.Encode([]rune(s))) }

// str returns body[key] if it is a string.
func str(body map[string]any, key string) (string, bool) {
	v, ok := body[key].(string)
	return v, ok
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	p := r.URL.Path[len("/api"):]
	if r.Method != http.MethodGet {
		if r.Header.Get("Origin") != s.cfg.PublicOrigin {
			writeError(w, http.StatusForbidden, "Request origin did not match.")
			return
		}
		limit := int64(8192)
		if p == "/veil/picture" {
			limit = maxAccountPictureBytes
		}
		if r.ContentLength > limit {
			writeError(w, http.StatusRequestEntityTooLarge, "Request too large.")
			return
		}
	}
	if err := s.route(w, r, p); err != nil {
		var failure *sessionFailure
		if errors.As(err, &failure) {
			s.invalidateRequestSession(w, r)
			writeJSON(w, failure.status, map[string]string{"error": failure.message, "code": failure.code})
		} else {
			writeFailure(w, err)
		}
	}
}

func (s *Server) route(w http.ResponseWriter, r *http.Request, p string) error {
	post := r.Method == http.MethodPost
	switch {
	case p == "/catalog" && !post:
		return s.catalog(w, r)
	case p == "/stats":
		return s.stats(w, r)
	case p == "/image":
		return s.image(w, r)
	}
	if rest, ok := strings.CutPrefix(p, "/veil/"); ok {
		return s.veil(w, r, rest)
	}
	if s.cfg.SessionKey == nil {
		writeError(w, http.StatusServiceUnavailable, "Account connection is not configured on this deployment.")
		return nil
	}
	acct, err := s.readAccount(r)
	if err != nil {
		return err
	}
	if acct == nil {
		return errStatus(http.StatusUnauthorized, "Sign in to your VRCove account first, then link VRChat.")
	}
	session := s.readSession(r)
	if p == "/account/logout" && session != nil {
		if expected := r.Header.Get("X-VRChat-Active-User"); expected != "" && expected != session.UserID {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "vrchat_account_changed", "error": "The active VRChat account changed in another tab. Refresh the account before trying this action again."})
			return nil
		}
	}
	switch {
	case p == "/account/login" && post:
		return s.login(w, r, acct)
	case p == "/account/switch" && post:
		return s.switchVRChat(w, r, acct)
	case p == "/account/2fa" && post:
		session = s.readPendingSession(r)
		if session == nil || session.AccountID != acct.ID {
			return errStatus(http.StatusUnauthorized, "Start linking VRChat from your VRCove account again.")
		}
		return s.verifyTwoFactor(w, r, session, acct)
	case p == "/account/logout" && post:
		if session != nil && session.AccountID == acct.ID && session.Auth != "" {
			s.vrc(r.Context(), "/logout", session, vrcRequest{Method: http.MethodPut})
		}
		s.clearActiveSession(w, r)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return nil
	}
	if session == nil || session.Auth == "" {
		linked, err := s.hasVRChatLink(r, acct, "")
		if err != nil {
			return err
		}
		if linked {
			return s.missingVRChatSession(r)
		}
	}
	if session == nil || session.Auth == "" || session.Pending != nil {
		writeError(w, http.StatusUnauthorized, "Link your VRChat account from your VRCove account.")
		return nil
	}
	if session.AccountID != acct.ID || session.UserID == "" {
		return errStatus(http.StatusForbidden, "Link VRChat to this VRCove account before using account actions.")
	}
	linked, err := s.hasVRChatLink(r, acct, session.UserID)
	if err != nil {
		return err
	}
	if !linked {
		return errStatus(http.StatusForbidden, "Link VRChat to this VRCove account before using account actions.")
	}
	if expected := r.Header.Get("X-VRChat-Active-User"); p != "/account/me" && expected != "" && expected != session.UserID {
		writeJSON(w, http.StatusConflict, map[string]string{"code": "vrchat_account_changed", "error": "The active VRChat account changed in another tab. Refresh the account before trying this action again."})
		return nil
	}
	return s.account(w, r, p, session)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request, acct *account) error {
	if !s.limiter.allow(clientIP(r), s.now()) {
		return errStatus(http.StatusTooManyRequests, "Too many sign-in attempts. Wait 10 minutes.")
	}
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	username, ok1 := str(body, "username")
	password, ok2 := str(body, "password")
	if !ok1 || !ok2 || username == "" || password == "" || jsLength(username) > 256 || jsLength(password) > 1024 {
		writeError(w, http.StatusBadRequest, "Enter a username and password.")
		return nil
	}
	expected, _ := str(body, "expectedUserId")
	if expected != "" {
		linked, err := s.hasVRChatLink(r, acct, expected)
		if err != nil {
			return err
		}
		if !userID.MatchString(expected) || !linked {
			return errStatus(http.StatusForbidden, "Choose one of your linked VRChat accounts.")
		}
	}
	basic := base64.StdEncoding.EncodeToString([]byte(encodeURIComponent(username) + ":" + encodeURIComponent(password)))
	data, updates, err := s.vrc(r.Context(), "/auth/user", nil, vrcRequest{Header: http.Header{"Authorization": {"Basic " + basic}}})
	if err != nil {
		return err
	}
	account, _ := data.(map[string]any)
	pending, isPending := account["requiresTwoFactorAuth"]
	isPending = isPending && pending != nil && pending != false
	lifetime := sessionLifetime
	session := &Session{AccountID: acct.ID, ExpectedUserID: expected, Username: username}
	session.apply(updates)
	if isPending {
		lifetime = pendingLifetime
		session.Pending = []string{}
		if list, ok := pending.([]any); ok {
			for _, m := range list {
				if m, ok := m.(string); ok {
					session.Pending = append(session.Pending, m)
				}
			}
		}
	}
	session.Expires = s.now().Add(lifetime).UnixMilli()
	if session.Auth == "" {
		writeError(w, http.StatusBadGateway, "VRChat did not issue a session.")
		return nil
	}
	if isPending {
		s.writeWithSession(w, map[string]any{"requiresTwoFactorAuth": pending}, session)
	} else {
		return s.storeVRChatLink(w, r, acct, session, data, true)
	}
	return nil
}

func (s *Server) verifyTwoFactor(w http.ResponseWriter, r *http.Request, session *Session, acct *account) error {
	if !s.limiter.allow(clientIP(r), s.now()) {
		return errStatus(http.StatusTooManyRequests, "Too many sign-in attempts. Wait 10 minutes.")
	}
	if session == nil || session.Auth == "" || session.Pending == nil {
		writeError(w, http.StatusUnauthorized, "Sign in again before entering a code.")
		return nil
	}
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	var allowed []string
	if slices.Contains(session.Pending, "totp") {
		allowed = []string{"totp", "otp"}
	} else if slices.Contains(session.Pending, "emailOtp") {
		allowed = []string{"emailotp"}
	}
	code, _ := str(body, "code")
	method, _ := str(body, "type")
	if !slices.Contains(allowed, method) || !twoFactorCode.MatchString(code) {
		writeError(w, http.StatusBadRequest, "Enter a valid verification code.")
		return nil
	}
	ctx := r.Context()
	result, updates, err := s.vrc(ctx, "/auth/twofactorauth/"+method+"/verify", session, vrcRequest{Method: http.MethodPost, Body: map[string]string{"code": code}})
	if err != nil {
		return err
	}
	if status, _ := result.(map[string]any); status["verified"] != true {
		writeError(w, http.StatusUnauthorized, "The code was not accepted.")
		return nil
	}
	session.apply(updates)
	session.Expires = s.now().Add(sessionLifetime).UnixMilli()
	session.Pending = nil
	user, updates, err := s.vrc(ctx, "/auth/user", session, vrcRequest{})
	if err != nil {
		var failure *sessionFailure
		if errors.As(err, &failure) {
			s.clearNamedSession(w, s.cookieName()+"-pending")
			return errStatus(http.StatusUnauthorized, "The VRChat verification session is no longer valid. Start linking this account again. Your active account has not changed.")
		}
		return err
	}
	session.apply(updates)
	if field(user, "id") == "" {
		writeError(w, http.StatusUnauthorized, "Verification is incomplete. Please sign in again.")
		return nil
	}
	return s.storeVRChatLink(w, r, acct, session, user, true)
}

// currentUserID looks up the signed-in user, applying any token rotation.
func (s *Server) currentUserID(r *http.Request, session *Session) (string, error) {
	me, updates, err := s.vrc(r.Context(), "/auth/user", session, vrcRequest{})
	if err != nil {
		return "", err
	}
	session.apply(updates)
	id := field(me, "id")
	if !userID.MatchString(id) {
		return "", errStatus(http.StatusBadGateway, "VRChat returned an unexpected account.")
	}
	return id, nil
}

func offsetParam(r *http.Request) string {
	return strconv.Itoa(queryInt(r.URL.Query().Get("offset"), 10000))
}

// account serves the signed-in endpoints. Most map to a single VRChat call
// that is forwarded after the input is validated.
func (s *Server) account(w http.ResponseWriter, r *http.Request, p string, session *Session) error {
	post := r.Method == http.MethodPost
	query := r.URL.Query()
	req := vrcRequest{}
	endpoint := ""
	// mapper, when set, filters the VRChat response before it is returned.
	var mapper func(any) (any, error)
	// idParam validates ?id= and writes a 400 when it does not match.
	idParam := func(pattern *regexp.Regexp, what string) (string, bool) {
		id := query.Get("id")
		if !pattern.MatchString(id) {
			writeError(w, http.StatusBadRequest, "Invalid "+what+" ID")
			return "", false
		}
		return id, true
	}
	switch {
	case p == "/account/profile" && post:
		input, err := requestJSON(r)
		if err != nil {
			return err
		}
		patch := map[string]string{}
		if v, present := input["status"]; present {
			status, _ := v.(string)
			if !slices.Contains([]string{"active", "join me", "ask me", "busy"}, status) {
				writeError(w, http.StatusBadRequest, "Invalid status")
				return nil
			}
			patch["status"] = status
		}
		for _, f := range []struct {
			key string
			max int
		}{{"statusDescription", 32}, {"bio", 512}} {
			if v, present := input[f.key]; present {
				text, ok := v.(string)
				if !ok || jsLength(text) > f.max {
					writeError(w, http.StatusBadRequest, "Invalid "+f.key)
					return nil
				}
				patch[f.key] = text
			}
		}
		if len(patch) == 0 {
			writeError(w, http.StatusBadRequest, "No profile changes supplied")
			return nil
		}
		result, updates, err := s.vrc(r.Context(), "/auth/user", session, vrcRequest{})
		if err != nil {
			return err
		}
		session.apply(updates)
		id := field(result, "id")
		if !userID.MatchString(id) {
			return errStatus(http.StatusBadGateway, "VRChat returned an unexpected account.")
		}
		bio, hasBio := patch["bio"]
		delete(patch, "bio")
		if len(patch) > 0 {
			result, updates, err = s.vrc(r.Context(), "/users/"+id, session, vrcRequest{Method: http.MethodPut, Body: patch})
			if err != nil {
				return err
			}
			session.apply(updates)
		}
		out := publicUser(result)
		if hasBio {
			profile, updates, err := s.vrc(r.Context(), "/profile/"+id, session, vrcRequest{Method: http.MethodPut, Body: map[string]string{"bio": bio}})
			if err != nil {
				return err
			}
			session.apply(updates)
			pick(out, asObject(profile), "bio", "bioLinks")
		}
		s.writeWithSession(w, out, session)
		return nil
	case p == "/account/search":
		kind := query.Get("type")
		search := []rune(query.Get("q"))
		if len(search) > 100 {
			search = search[:100]
		}
		params := url.Values{"n": {"50"}, "offset": {offsetParam(r)}}
		switch kind {
		case "users", "worlds":
			params.Set("search", string(search))
		case "groups":
			params.Set("query", string(search))
		default:
			writeError(w, http.StatusBadRequest, "Invalid search type")
			return nil
		}
		if kind == "worlds" {
			params.Set("releaseStatus", "public")
		}
		result, updates, err := s.vrc(r.Context(), "/"+kind+"?"+params.Encode(), session, req)
		if err != nil {
			return err
		}
		session.apply(updates)
		if kind == "users" {
			if result, err = publicUsers(result); err != nil {
				return err
			}
		}
		s.writeWithSession(w, result, session)
		return nil
	case p == "/account/groups":
		id, err := s.currentUserID(r, session)
		if err != nil {
			return err
		}
		endpoint = "/users/" + id + "/groups"
	case p == "/account/group":
		id, ok := idParam(groupID, "group")
		if !ok {
			return nil
		}
		endpoint, mapper = "/groups/"+id, publicGroup
	case p == "/account/group-posts":
		id, ok := idParam(groupID, "group")
		if !ok {
			return nil
		}
		endpoint, mapper = "/groups/"+id+"/posts?n=20&offset=0", groupPosts
	case p == "/account/group-members":
		id, ok := idParam(groupID, "group")
		if !ok {
			return nil
		}
		endpoint, mapper = "/groups/"+id+"/members?n=50&offset=0", groupMembers
	case p == "/account/group-instances":
		id, ok := idParam(groupID, "group")
		if !ok {
			return nil
		}
		endpoint, mapper = "/groups/"+id+"/instances", groupInstances
	case p == "/account/world":
		id, ok := idParam(worldID, "world")
		if !ok {
			return nil
		}
		endpoint, mapper = "/worlds/"+id, publicWorld
	case p == "/account/user":
		id, ok := idParam(userID, "user")
		if !ok {
			return nil
		}
		endpoint, mapper = "/users/"+id, publicProfile
	case p == "/account/user-worlds":
		id, ok := idParam(userID, "user")
		if !ok {
			return nil
		}
		params := url.Values{"userId": {id}, "releaseStatus": {"public"}, "sort": {"updated"}, "order": {"descending"}, "n": {"24"}}
		endpoint, mapper = "/worlds?"+params.Encode(), worldSummaries
	case p == "/account/user-groups":
		id, ok := idParam(userID, "user")
		if !ok {
			return nil
		}
		endpoint, mapper = "/users/"+id+"/groups", userGroups
	case p == "/account/raw":
		// Deliberately unfiltered: the full VRChat record, asset URLs included.
		kinds := map[string]struct {
			prefix  string
			pattern *regexp.Regexp
		}{"world": {"/worlds/", worldID}, "avatar": {"/avatars/", avatarID}, "group": {"/groups/", groupID}, "user": {"/users/", userID}}
		kind, ok := kinds[query.Get("kind")]
		if !ok {
			writeError(w, http.StatusBadRequest, "Invalid record type")
			return nil
		}
		id, ok := idParam(kind.pattern, query.Get("kind"))
		if !ok {
			return nil
		}
		endpoint = kind.prefix + id
	case p == "/account/my-avatars", p == "/account/my-worlds":
		endpoint = "/avatars"
		if p == "/account/my-worlds" {
			endpoint = "/worlds"
		}
		endpoint += "?user=me&releaseStatus=all&sort=updated&order=descending&n=100&offset=" + offsetParam(r)
	case p == "/account/action" && post:
		a, err := requestJSON(r)
		if err != nil {
			return err
		}
		action, _ := str(a, "action")
		id, _ := str(a, "id")
		switch action {
		case "friend", "unfriend", "cancelFriend", "requestInvite":
			if !userID.MatchString(id) {
				writeError(w, http.StatusBadRequest, "Invalid user ID")
				return nil
			}
			switch action {
			case "friend":
				endpoint, req.Method = "/user/"+id+"/friendRequest", http.MethodPost
			case "cancelFriend":
				endpoint, req.Method = "/user/"+id+"/friendRequest", http.MethodDelete
			case "unfriend":
				endpoint, req.Method = "/auth/user/friends/"+id, http.MethodDelete
			case "requestInvite":
				endpoint, req.Method, req.Body = "/requestInvite/"+id, http.MethodPost, map[string]int{"requestSlot": 0}
			}
		case "accept", "hide", "see":
			if !notificationID.MatchString(id) {
				writeError(w, http.StatusBadRequest, "Invalid notification ID")
				return nil
			}
			endpoint, req.Method = "/auth/user/notifications/"+id+"/"+action, http.MethodPut
		case "joinGroup", "leaveGroup":
			if !groupID.MatchString(id) {
				writeError(w, http.StatusBadRequest, "Invalid group ID")
				return nil
			}
			suffix := "/join"
			if action == "leaveGroup" {
				suffix = "/leave"
			}
			endpoint, req.Method = "/groups/"+id+suffix, http.MethodPost
		case "selfInvite":
			if !location.MatchString(id) {
				writeError(w, http.StatusBadRequest, "Invalid instance")
				return nil
			}
			endpoint, req.Method = "/invite/myself/to/"+id, http.MethodPost
		case "newInstance":
			region, _ := str(a, "region")
			if !worldID.MatchString(id) || !slices.Contains([]string{"us", "use", "eu", "jp"}, region) {
				writeError(w, http.StatusBadRequest, "Choose a world and region.")
				return nil
			}
			created, updates, err := s.vrc(r.Context(), "/instances", session, vrcRequest{Method: http.MethodPost,
				Body: map[string]string{"worldId": id, "type": "public", "region": region}})
			if err != nil {
				return err
			}
			session.apply(updates)
			info := asObject(created)
			loc, _ := info["location"].(string)
			if loc == "" {
				instance, _ := info["instanceId"].(string)
				loc = id + ":" + instance
			}
			if !location.MatchString(loc) {
				writeError(w, http.StatusBadGateway, "VRChat did not return a joinable instance.")
				return nil
			}
			s.writeWithSession(w, map[string]string{"location": loc}, session)
			return nil
		case "fallback":
			if !avatarID.MatchString(id) {
				writeError(w, http.StatusBadRequest, "Invalid avatar ID")
				return nil
			}
			endpoint, req.Method = "/avatars/"+id+"/selectfallback", http.MethodPut
		default:
			writeError(w, http.StatusBadRequest, "Unsupported action")
			return nil
		}
	case p == "/account/me":
		endpoint = "/auth/user"
	case p == "/account/friends":
		offline := "false"
		if query.Get("offline") == "true" {
			offline = "true"
		}
		endpoint = "/auth/user/friends?offline=" + offline + "&n=100&offset=" + offsetParam(r)
	case p == "/account/notifications":
		endpoint = "/auth/user/notifications?n=100"
	case p == "/account/worlds":
		endpoint = "/worlds?sort=popularity&order=descending&n=48&releaseStatus=public"
	case p == "/account/favorites" && !post:
		kind := query.Get("type")
		if kind == "" {
			kind = "avatar"
		}
		if kind != "avatar" && kind != "world" {
			writeError(w, http.StatusBadRequest, "Choose avatar or world favorites.")
			return nil
		}
		endpoint = "/favorites?type=" + kind + "&n=100&offset=" + offsetParam(r)
	case p == "/account/favorite-groups":
		endpoint = "/favorite/groups?type=avatar&n=100"
	case p == "/account/avatar" && !post:
		id := query.Get("id")
		if !avatarID.MatchString(id) {
			writeError(w, http.StatusBadRequest, "Invalid avatar ID")
			return nil
		}
		endpoint = "/avatars/" + id
	case p == "/account/select" && post:
		body, err := requestJSON(r)
		if err != nil {
			return err
		}
		id, _ := str(body, "avatarId")
		if !avatarID.MatchString(id) {
			writeError(w, http.StatusBadRequest, "Invalid avatar ID")
			return nil
		}
		endpoint, req.Method = "/avatars/"+id+"/select", http.MethodPut
	case p == "/account/favorites" && post:
		body, err := requestJSON(r)
		if err != nil {
			return err
		}
		id, _ := str(body, "avatarId")
		group, _ := str(body, "group")
		if !avatarID.MatchString(id) || !favoriteGroup.MatchString(group) {
			writeError(w, http.StatusBadRequest, "Choose an avatar and favorite group.")
			return nil
		}
		endpoint, req.Method = "/favorites", http.MethodPost
		req.Body = map[string]any{"type": "avatar", "favoriteId": id, "tags": []string{group}}
	case p == "/account/unfavorite" && post:
		body, err := requestJSON(r)
		if err != nil {
			return err
		}
		id, _ := str(body, "favoriteId")
		if !favoriteID.MatchString(id) {
			writeError(w, http.StatusBadRequest, "Invalid favorite ID")
			return nil
		}
		endpoint, req.Method = "/favorites/"+id, http.MethodDelete
	default:
		writeError(w, http.StatusNotFound, "Not found")
		return nil
	}
	result, updates, err := s.vrc(r.Context(), endpoint, session, req)
	if err != nil {
		return err
	}
	session.apply(updates)
	switch p {
	case "/account/me":
		if !userID.MatchString(field(result, "id")) {
			return invalidVRChatSession()
		}
		if err := s.rememberVRChatUsername(r, session, result); err != nil {
			return err
		}
		result = publicUser(result)
	case "/account/friends":
		mapper = func(data any) (any, error) { return publicUsers(data) }
	}
	if mapper != nil {
		if result, err = mapper(result); err != nil {
			return err
		}
	}
	if p == "/account/user" {
		// Bios moved to the profile API. Keep relationship and activity metadata
		// from /users, but take bio fields from their authoritative endpoint.
		profile, updates, err := s.vrc(r.Context(), "/profile/"+query.Get("id"), session, vrcRequest{})
		if err != nil {
			return err
		}
		session.apply(updates)
		pick(asObject(result), asObject(profile), "bio", "bioLinks")
	}
	s.writeWithSession(w, result, session)
	return nil
}
