package server

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

const maxImageBytes = 10_000_000

var (
	vrchatImagePath = regexp.MustCompile(`^/api/1/(?:file|image)/file_[a-f0-9-]+/`)
	// Signed file redirects, used for group icons and banners, often have no
	// extension, so a VRChat file ID also qualifies. Content types are checked
	// on every response.
	vrchatFilePath = regexp.MustCompile(`(?i)(?:\.(?:png|jpe?g|webp|gif)|file_[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}[^/]*)$`)
	imageTypes     = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true, "image/avif": true}
)

// allowedImage accepts only HTTPS URLs on VRChat's image hosts. Paths must be
// canonical so dot segments or encoded separators cannot escape the prefix.
func allowedImage(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Opaque != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	p := u.EscapedPath()
	lower := strings.ToLower(p)
	if p == "" || strings.Contains(p, `\`) || strings.Contains(lower, "%2e") || strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || path.Clean(p) != p {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "api.vrchat.cloud":
		return vrchatImagePath.MatchString(p)
	case "files.vrchat.cloud":
		return vrchatFilePath.MatchString(p)
	case "s3-us-west-2.amazonaws.com":
		return strings.HasPrefix(p, "/vrc-uploads/")
	case "s3.amazonaws.com", "s3.us-east-1.amazonaws.com":
		return strings.HasPrefix(p, "/files.vrchat.cloud/") && vrchatFilePath.MatchString(p)
	case "assets.vrchat.com":
		return true
	}
	return false
}

// image proxies avatar and profile images so browsers never contact VRChat
// directly. Every redirect hop is re-validated against the allow-list.
func (s *Server) image(w http.ResponseWriter, r *http.Request) error {
	target, err := url.Parse(r.URL.Query().Get("url"))
	if err != nil || !allowedImage(target) {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}
	var session *Session
	if s.cfg.SessionKey != nil {
		session = s.readSession(r)
		if session != nil {
			acct, err := s.readAccount(r)
			if err != nil {
				return err
			}
			if acct == nil || session.AccountID != acct.ID || session.UserID == "" || session.Pending != nil {
				session = nil
			} else {
				linked, err := s.hasVRChatLink(r, acct, session.UserID)
				if err != nil {
					return err
				}
				if !linked {
					session = nil
				}
			}
		}
	}
	for range 4 {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			cancel()
			return err
		}
		req.Header.Set("Accept", "image/*")
		req.Header.Set("User-Agent", s.userAgent())
		if strings.EqualFold(target.Hostname(), "api.vrchat.cloud") && session != nil && session.Auth != "" {
			req.Header.Set("Cookie", vrcCookie(session))
		}
		resp, err := s.Client.Do(req)
		if err != nil {
			cancel()
			return err
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			next, err := target.Parse(resp.Header.Get("Location"))
			resp.Body.Close()
			cancel()
			if err != nil || !allowedImage(next) {
				w.WriteHeader(http.StatusNotFound)
				return nil
			}
			target = next
			continue
		}
		err = s.writeImage(w, resp)
		resp.Body.Close()
		cancel()
		return err
	}
	w.WriteHeader(http.StatusNotFound)
	return nil
}

func (s *Server) writeImage(w http.ResponseWriter, resp *http.Response) error {
	contentType, _, _ := strings.Cut(resp.Header.Get("Content-Type"), ";")
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	// Extensionless uploads are often served as generic binary data, so
	// those are identified from their bytes instead.
	generic := contentType == "application/octet-stream" || contentType == "binary/octet-stream" || contentType == ""
	if resp.StatusCode < 200 || resp.StatusCode > 299 || (!imageTypes[contentType] && !generic) {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}
	if resp.ContentLength > maxImageBytes {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return nil
	}
	image, err := readBounded(resp.Body, maxImageBytes, "Image too large.")
	if err != nil {
		return err
	}
	if generic {
		contentType = http.DetectContentType(image)
		if !imageTypes[contentType] {
			w.WriteHeader(http.StatusNotFound)
			return nil
		}
	}
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Cache-Control", "private, no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	w.Write(image)
	return nil
}
