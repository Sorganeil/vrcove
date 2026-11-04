package server

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/Sorganeil/vrcove/vrchat"
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
	sessionLifetime = 14 * 24 * time.Hour
)

// jsLength counts UTF-16 code units, matching the frontend's length limits.
// worldSorts maps VRCove's world sort names to VRChat's. Without one, a search
// keeps VRChat's own best-match order.
var worldSorts = map[string]string{"popular": "popularity", "trending": "heat", "favorites": "favorites", "newest": "publicationDate", "updated": "updated"}

// worldPlatforms maps the platform filter to VRChat's platform names.
var worldPlatforms = map[string]string{"pc": "standalonewindows", "quest": "android"}

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
	// Cached pages and shared picture links may still use the old prefix.
	if rest, ok := strings.CutPrefix(p, "/veil/"); ok {
		p = "/cove/" + rest
	}
	if r.Method != http.MethodGet {
		if r.Header.Get("Origin") != s.cfg.PublicOrigin {
			writeError(w, http.StatusForbidden, "Request origin did not match.")
			return
		}
		limit := int64(8192)
		if p == "/cove/picture" {
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
	// In the desktop app, this server handles VRChat routes and pictures.
	// The catalog and VRCove accounts stay on the website.
	if s.cfg.Local && !strings.HasPrefix(p, "/account/") && p != "/image" {
		s.cfg.Remote.ServeHTTP(w, r)
		return nil
	}
	// After the cutover, the website's VRChat requests run only in the
	// browser's VRChat worker. This server no longer holds VRChat sessions.
	if s.cfg.VRChatWorker == "only" && !s.cfg.Local && (strings.HasPrefix(p, "/account/") || p == "/cove/link") {
		writeJSON(w, http.StatusGone, map[string]string{"error": "VRCove now connects to VRChat from your browser. Sign in to VRChat again.", "code": "vrchat_relink"})
		return nil
	}
	switch {
	case p == "/logger/known":
		return s.loggerKnown(w, r)
	case p == "/logger/avatars":
		return s.loggerAvatars(w, r)
	case p == "/catalog" && !post:
		return s.catalog(w, r)
	case p == "/catalog/tags" && !post:
		return s.catalogTags(w, r)
	case p == "/vrcx/avatars" && !post:
		return s.vrcxAvatars(w, r)
	case p == "/stats":
		return s.stats(w, r)
	case p == "/image":
		return s.image(w, r)
	case p == "/relay" && !post:
		return s.relay(w, r)
	}
	if rest, ok := strings.CutPrefix(p, "/cove/"); ok {
		return s.cove(w, r, rest)
	}
	if s.cfg.SessionKey == nil {
		writeError(w, http.StatusServiceUnavailable, "Account connection is not configured on this deployment.")
		return nil
	}
	acct, err := s.readAccount(r)
	if err != nil {
		return err
	}
	if s.cfg.Local {
		acct = &account{ID: localAccountID, Username: "local"}
		if p == "/account/device" {
			s.deviceLinks(w, r)
			return nil
		}
		if p == "/account/device/remove" && post {
			return s.removeLocalAccount(w, r)
		}
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
	case p == "/account/switch" && post && s.cfg.Local:
		return s.switchLocalAccount(w, r)
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
			s.vrc(r.Context(), session, vrchat.Logout())
		}
		if session != nil && session.AccountID == acct.ID {
			if err := s.forgetSession(r.Context(), session); err != nil {
				return err
			}
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
	// Sessions from before session records get one on their next request.
	if err := s.registerVRChatSession(r, session); err != nil {
		return err
	}
	if expected := r.Header.Get("X-VRChat-Active-User"); p != "/account/me" && expected != "" && expected != session.UserID {
		writeJSON(w, http.StatusConflict, map[string]string{"code": "vrchat_account_changed", "error": "The active VRChat account changed in another tab. Refresh the account before trying this action again."})
		return nil
	}
	return s.account(w, r, p, session)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request, acct *account) error {
	if !s.limiter.allow(clientKey(r), s.now()) {
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
	data, updates, err := s.vrc(r.Context(), nil, vrchat.Login(username, password))
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
	if !s.limiter.allow(clientKey(r), s.now()) {
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
	result, updates, err := s.vrc(ctx, session, vrchat.VerifyTwoFactor(method, code))
	if err != nil {
		var he *httpError
		if errors.As(err, &he) && he.status == http.StatusBadRequest {
			writeError(w, http.StatusUnauthorized, "That verification code was not accepted or has expired. Check the code and try again.")
			return nil
		}
		return err
	}
	if status, _ := result.(map[string]any); status["verified"] != true {
		writeError(w, http.StatusUnauthorized, "That verification code was not accepted or has expired. Check the code and try again.")
		return nil
	}
	session.apply(updates)
	session.Expires = s.now().Add(sessionLifetime).UnixMilli()
	session.Pending = nil
	user, updates, err := s.vrc(ctx, session, vrchat.GetCurrentUser())
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
	me, updates, err := s.vrc(r.Context(), session, vrchat.GetCurrentUser())
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

func offsetParam(r *http.Request) *int {
	return vrchat.Ptr(queryInt(r.URL.Query().Get("offset"), 10000))
}

func userPatch(patch map[string]string) *vrchat.UpdateUserRequest {
	out := &vrchat.UpdateUserRequest{}
	if value, ok := patch["status"]; ok {
		out.Status = vrchat.Ptr(vrchat.UserStatus(value))
	}
	if value, ok := patch["statusDescription"]; ok {
		out.StatusDescription = vrchat.Ptr(value)
	}
	return out
}

// account serves the signed-in endpoints. Most map to a single VRChat call
// that is forwarded after the input is validated.
func (s *Server) account(w http.ResponseWriter, r *http.Request, p string, session *Session) error {
	post := r.Method == http.MethodPost
	query := r.URL.Query()
	var call vrchat.Call
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
	case p == "/account/username-check" && !post && s.cfg.Local:
		return s.checkUsername(w, r, session)
	case p == "/account/profile" && post:
		input, err := requestJSON(r)
		if err != nil {
			return err
		}
		patch := map[string]string{}
		profilePatch := &vrchat.UpdateProfileRequest{}
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
		if v, present := input["bioLinks"]; present {
			links, ok := v.([]any)
			if !ok {
				writeError(w, http.StatusBadRequest, "Invalid bioLinks")
				return nil
			}
			bioLinks := make([]string, 0, len(links))
			for _, v := range links {
				link, ok := v.(string)
				if !ok {
					writeError(w, http.StatusBadRequest, "Invalid bioLinks")
					return nil
				}
				link = strings.TrimSpace(link)
				u, err := url.Parse(link)
				if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
					writeError(w, http.StatusBadRequest, "Enter a valid http:// or https:// URL for each social link.")
					return nil
				}
				bioLinks = append(bioLinks, link)
			}
			profilePatch.BioLinks = &bioLinks
		}
		if bio, present := patch["bio"]; present {
			profilePatch.Bio = vrchat.Ptr(bio)
			delete(patch, "bio")
		}
		if len(patch) == 0 && profilePatch.Bio == nil && profilePatch.BioLinks == nil {
			writeError(w, http.StatusBadRequest, "No profile changes supplied")
			return nil
		}
		result, updates, err := s.vrc(r.Context(), session, vrchat.GetCurrentUser())
		if err != nil {
			return err
		}
		session.apply(updates)
		id := field(result, "id")
		if !userID.MatchString(id) {
			return errStatus(http.StatusBadGateway, "VRChat returned an unexpected account.")
		}
		if len(patch) > 0 {
			result, updates, err = s.vrc(r.Context(), session, vrchat.UpdateUser(id, userPatch(patch)))
			if err != nil {
				return err
			}
			session.apply(updates)
		}
		out := publicUser(result)
		if profilePatch.Bio != nil || profilePatch.BioLinks != nil {
			profile, updates, err := s.vrc(r.Context(), session, vrchat.UpdateProfile(id, profilePatch))
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
		switch kind {
		case "users":
			call = vrchat.SearchUsers(vrchat.SearchUsersParams{Search: vrchat.Ptr(string(search)), N: vrchat.Ptr(50), Offset: offsetParam(r)})
		case "worlds":
			params := vrchat.SearchWorldsParams{
				N:             vrchat.Ptr(50),
				Offset:        offsetParam(r),
				ReleaseStatus: vrchat.Ptr(vrchat.ReleaseStatus("public")),
			}
			if len(search) > 0 {
				params.Search = vrchat.Ptr(string(search))
			}
			if sort, ok := worldSorts[query.Get("sort")]; ok {
				params.Sort, params.Order = vrchat.Ptr(vrchat.SortOption(sort)), vrchat.Ptr(vrchat.OrderOption("descending"))
			} else if len(search) == 0 {
				params.Sort, params.Order = vrchat.Ptr(vrchat.SortOption("popularity")), vrchat.Ptr(vrchat.OrderOption("descending"))
			}
			if platform, ok := worldPlatforms[query.Get("platform")]; ok {
				params.Platform = vrchat.Ptr(platform)
			}
			call = vrchat.SearchWorlds(params)
		case "groups":
			call = vrchat.SearchGroups(vrchat.SearchGroupsParams{Query: vrchat.Ptr(string(search)), N: vrchat.Ptr(50), Offset: offsetParam(r)})
		default:
			writeError(w, http.StatusBadRequest, "Invalid search type")
			return nil
		}
		result, updates, err := s.vrc(r.Context(), session, call)
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
		call = vrchat.GetUserGroups(id)
	case p == "/account/group":
		id, ok := idParam(groupID, "group")
		if !ok {
			return nil
		}
		call, mapper = vrchat.GetGroup(id, vrchat.GetGroupParams{}), publicGroup
	case p == "/account/group-posts":
		id, ok := idParam(groupID, "group")
		if !ok {
			return nil
		}
		call, mapper = vrchat.GetGroupPosts(id, vrchat.GetGroupPostsParams{N: vrchat.Ptr(20), Offset: vrchat.Ptr(0)}), groupPosts
	case p == "/account/group-members":
		id, ok := idParam(groupID, "group")
		if !ok {
			return nil
		}
		call, mapper = vrchat.GetGroupMembers(id, vrchat.GetGroupMembersParams{N: vrchat.Ptr(50), Offset: vrchat.Ptr(0)}), groupMembers
	case p == "/account/group-instances":
		id, ok := idParam(groupID, "group")
		if !ok {
			return nil
		}
		call, mapper = vrchat.GetGroupInstances(id), groupInstances
	case p == "/account/world":
		id, ok := idParam(worldID, "world")
		if !ok {
			return nil
		}
		call, mapper = vrchat.GetWorld(id), publicWorld
	case p == "/account/user":
		id, ok := idParam(userID, "user")
		if !ok {
			return nil
		}
		call, mapper = vrchat.GetUser(id), publicProfile
	case p == "/account/user-worlds":
		id, ok := idParam(userID, "user")
		if !ok {
			return nil
		}
		call, mapper = vrchat.SearchWorlds(vrchat.SearchWorldsParams{
			UserID:        vrchat.Ptr(id),
			ReleaseStatus: vrchat.Ptr(vrchat.ReleaseStatus("public")),
			Sort:          vrchat.Ptr(vrchat.SortOption("updated")),
			Order:         vrchat.Ptr(vrchat.OrderOption("descending")),
			N:             vrchat.Ptr(24),
		}), worldSummaries
	case p == "/account/user-groups":
		id, ok := idParam(userID, "user")
		if !ok {
			return nil
		}
		call, mapper = vrchat.GetUserGroups(id), userGroups
	case p == "/account/raw":
		// Deliberately unfiltered: the full VRChat record, asset URLs included.
		kinds := map[string]struct {
			call    func(string) vrchat.Call
			pattern *regexp.Regexp
		}{
			"world":  {func(id string) vrchat.Call { return vrchat.GetWorld(id) }, worldID},
			"avatar": {func(id string) vrchat.Call { return vrchat.GetAvatar(id) }, avatarID},
			"group":  {func(id string) vrchat.Call { return vrchat.GetGroup(id, vrchat.GetGroupParams{}) }, groupID},
			"user":   {func(id string) vrchat.Call { return vrchat.GetUser(id) }, userID},
		}
		kind, ok := kinds[query.Get("kind")]
		if !ok {
			writeError(w, http.StatusBadRequest, "Invalid record type")
			return nil
		}
		id, ok := idParam(kind.pattern, query.Get("kind"))
		if !ok {
			return nil
		}
		call = kind.call(id)
	case p == "/account/my-avatars", p == "/account/my-worlds":
		if p == "/account/my-worlds" {
			call = vrchat.SearchWorlds(vrchat.SearchWorldsParams{
				User:          vrchat.Ptr(vrchat.SearchWorldsUser("me")),
				ReleaseStatus: vrchat.Ptr(vrchat.ReleaseStatus("all")),
				Sort:          vrchat.Ptr(vrchat.SortOption("updated")),
				Order:         vrchat.Ptr(vrchat.OrderOption("descending")),
				N:             vrchat.Ptr(100),
				Offset:        offsetParam(r),
			})
		} else {
			call = vrchat.SearchAvatars(vrchat.SearchAvatarsParams{
				User:          vrchat.Ptr("me"),
				ReleaseStatus: vrchat.Ptr(vrchat.ReleaseStatus("all")),
				Sort:          vrchat.Ptr(vrchat.SortOptionAvatar("updated")),
				Order:         vrchat.Ptr(vrchat.OrderOption("descending")),
				N:             vrchat.Ptr(100),
				Offset:        offsetParam(r),
			})
		}
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
				call = vrchat.Friend(id)
			case "cancelFriend":
				call = vrchat.DeleteFriendRequest(id)
			case "unfriend":
				call = vrchat.Unfriend(id)
			case "requestInvite":
				call = vrchat.RequestInvite(id, &vrchat.RequestInviteRequest{RequestSlot: vrchat.Ptr(0)})
			}
		case "accept", "hide", "see":
			if !notificationID.MatchString(id) {
				writeError(w, http.StatusBadRequest, "Invalid notification ID")
				return nil
			}
			switch action {
			case "accept":
				call = vrchat.AcceptFriendRequest(id)
			case "hide":
				call = vrchat.DeleteNotification(id)
			case "see":
				call = vrchat.MarkNotificationAsRead(id)
			}
		case "joinGroup", "leaveGroup":
			if !groupID.MatchString(id) {
				writeError(w, http.StatusBadRequest, "Invalid group ID")
				return nil
			}
			if action == "leaveGroup" {
				call = vrchat.LeaveGroup(id)
			} else {
				call = vrchat.JoinGroup(id, vrchat.JoinGroupParams{}, nil)
			}
		case "selfInvite":
			if !location.MatchString(id) {
				writeError(w, http.StatusBadRequest, "Invalid instance")
				return nil
			}
			world, instance, _ := strings.Cut(id, ":")
			call = vrchat.InviteMyselfTo(world, instance)
		case "newInstance":
			region, _ := str(a, "region")
			if !worldID.MatchString(id) || !slices.Contains([]string{"us", "use", "eu", "jp"}, region) {
				writeError(w, http.StatusBadRequest, "Choose a world and region.")
				return nil
			}
			created, updates, err := s.vrc(r.Context(), session, vrchat.CreateInstance(vrchat.CreateInstanceRequest{
				WorldID: vrchat.WorldID(id),
				Type:    vrchat.InstanceType("public"),
				Region:  vrchat.CreateInstanceRequestRegion(region),
			}))
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
			call = vrchat.SelectFallbackAvatar(id)
		default:
			writeError(w, http.StatusBadRequest, "Unsupported action")
			return nil
		}
	case p == "/account/me":
		call = vrchat.GetCurrentUser()
	case p == "/account/friends":
		call = vrchat.GetFriends(vrchat.GetFriendsParams{Offline: vrchat.Ptr(query.Get("offline") == "true"), N: vrchat.Ptr(100), Offset: offsetParam(r)})
	case p == "/account/notifications":
		call = vrchat.GetNotifications(vrchat.GetNotificationsParams{N: vrchat.Ptr(100)})
	case p == "/account/live" && !post:
		return s.liveNotifications(w, r, session)
	case p == "/account/worlds":
		call = vrchat.SearchWorlds(vrchat.SearchWorldsParams{
			Sort:          vrchat.Ptr(vrchat.SortOption("popularity")),
			Order:         vrchat.Ptr(vrchat.OrderOption("descending")),
			N:             vrchat.Ptr(48),
			ReleaseStatus: vrchat.Ptr(vrchat.ReleaseStatus("public")),
		})
	case p == "/account/favorites" && !post:
		kind := query.Get("type")
		if kind == "" {
			kind = "avatar"
		}
		if kind != "avatar" && kind != "world" && kind != "friend" {
			writeError(w, http.StatusBadRequest, "Choose avatar, world or friend favorites.")
			return nil
		}
		call = vrchat.GetFavorites(vrchat.GetFavoritesParams{Type: vrchat.Ptr(kind), N: vrchat.Ptr(100), Offset: offsetParam(r)})
	case p == "/account/favorite-groups":
		kind := "avatar"
		if query.Get("type") == "friend" {
			kind = "friend"
		}
		call = vrchat.GetFavoriteGroups(vrchat.GetFavoriteGroupsParams{Type: vrchat.Ptr(vrchat.FavoriteType(kind)), N: vrchat.Ptr(100)})
	case p == "/account/avatar" && !post:
		id := query.Get("id")
		if !avatarID.MatchString(id) {
			writeError(w, http.StatusBadRequest, "Invalid avatar ID")
			return nil
		}
		call = vrchat.GetAvatar(id)
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
		call = vrchat.SelectAvatar(id)
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
		call = vrchat.AddFavorite(&vrchat.AddFavoriteRequest{Type: vrchat.FavoriteType("avatar"), FavoriteID: id, Tags: []vrchat.Tag{vrchat.Tag(group)}})
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
		call = vrchat.RemoveFavorite(id)
	default:
		writeError(w, http.StatusNotFound, "Not found")
		return nil
	}
	result, updates, err := s.vrc(r.Context(), session, call)
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
		profile, updates, err := s.vrc(r.Context(), session, vrchat.GetPublicProfile(query.Get("id"), vrchat.GetPublicProfileParams{}))
		if err != nil {
			return err
		}
		session.apply(updates)
		pick(asObject(result), asObject(profile), "bio", "bioLinks")
	}
	// Shared links usually follow a visit, so the visit fills the preview.
	// Only public display fields are kept, never who viewed it.
	if p == "/account/world" || p == "/account/group" || p == "/account/user" {
		s.cachePreview(strings.TrimPrefix(p, "/account/"), query.Get("id"), result)
	}
	s.writeWithSession(w, result, session)
	return nil
}
