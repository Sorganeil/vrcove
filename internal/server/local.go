package server

import "net/http"

// localAccountID stands in for the VRCove account in local mode, where the
// VRChat session alone identifies the person using the device.
const localAccountID = 1

// APIHandler serves the /api routes without the site's pages, for embedding
// in the desktop app.
func (s *Server) APIHandler() http.Handler { return http.HandlerFunc(s.handleAPI) }

// LocalUser reports the VRChat account signed in on this request, if any.
func (s *Server) LocalUser(r *http.Request) (userID, username string) {
	if session := s.readSession(r); session != nil && session.Pending == nil && session.Auth != "" {
		return session.UserID, session.Username
	}
	return "", ""
}

// deviceLinks lists the VRChat account signed in on this device, in the
// shape the website uses for accounts linked to VRCove.
func (s *Server) deviceLinks(w http.ResponseWriter, r *http.Request) {
	links := []map[string]any{}
	if session := s.readSession(r); session != nil && session.Pending == nil && session.UserID != "" {
		name := session.Username
		links = append(links, map[string]any{"id": localAccountID, "vrchatId": session.UserID, "username": name,
			"displayName": name, "linked": "", "active": true, "sessionExpires": session.Expires})
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}
