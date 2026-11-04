package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/Sorganeil/vrcove/vrchat"
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
// Cache results on the server. Tell browsers not to store them so images
// do not remain on a shared device.
func (s *Server) image(w http.ResponseWriter, r *http.Request) error {
	target, err := url.Parse(r.URL.Query().Get("url"))
	if err != nil || !allowedImage(target) {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}
	var session *Session
	// After the cutover, private images load through the browser's VRChat
	// worker, so the proxy never sends a viewer's cookie.
	if s.cfg.SessionKey != nil && s.cfg.VRChatWorker != "only" {
		session = s.readSession(r)
		if session != nil {
			acct, err := s.readAccount(r)
			if err != nil {
				return err
			}
			if acct == nil || session.AccountID != acct.ID || session.UserID == "" || session.Pending != nil || session.Auth == "" {
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
	user := ""
	if session != nil {
		user = session.UserID
	}
	raw := target.String()
	if img, ok := s.images.get(raw, user, s.now()); ok {
		return writeImage(w, img)
	}
	img, err := s.images.do(r.Context(), user+"\x00"+raw, func() (imageResult, error) {
		// Cache misses wait for one of a few upstream slots, so a flood of
		// unknown images can't open unlimited connections to VRChat.
		var img imageResult
		var err error
		if slotErr := runIn(r.Context(), s.imageSlots, func() {
			img, err = s.fetchImage(context.WithoutCancel(r.Context()), target, session)
		}); slotErr != nil {
			return imageResult{status: http.StatusServiceUnavailable}, nil
		}
		if err == nil && img.status == http.StatusOK {
			s.images.put(raw, user, img, s.now())
		}
		return img, err
	})
	if err != nil {
		return err
	}
	return writeImage(w, img)
}

// imageResult is one proxied image, or the status to answer with instead.
type imageResult struct {
	status      int
	contentType string
	body        []byte
	// private marks images fetched with the viewer's VRChat cookie, which
	// are never shared with other viewers.
	private bool
}

// fetchImage follows up to four validated redirects. Only hops to the VRChat
// API carry the viewer's cookie, and only those are subject to backoff.
func (s *Server) fetchImage(ctx context.Context, target *url.URL, session *Session) (imageResult, error) {
	var res imageResult
	client, err := s.vrchatClient()
	if err != nil {
		return res, err
	}
	options := vrchat.DownloadOptions{Timeout: 15 * time.Second, MaxBytes: maxImageBytes}
	for range 4 {
		api := client.IsAPIURL(target)
		withCookie := api && session != nil
		key := serverKey
		if withCookie {
			key = backoffKey(session)
		}
		if api && (s.backoff.coolingDown(serverKey, s.now()) || s.backoff.coolingDown(key, s.now())) {
			return imageResult{status: http.StatusTooManyRequests}, nil
		}
		res.private = res.private || withCookie
		fetched, _, err := client.FetchImage(ctx, vrchatTokens(session), target.String(), options)
		if err != nil {
			var apiErr *vrchat.APIError
			switch {
			case errors.As(err, &apiErr):
				if apiErr.StatusCode == http.StatusTooManyRequests {
					if api {
						now := s.now()
						s.backoff.limited(key, apiErr.RetryDelay(now, maxRetryAfter), now)
					}
					return imageResult{status: http.StatusTooManyRequests}, nil
				}
				return imageResult{status: http.StatusNotFound}, nil
			case errors.Is(err, vrchat.ErrResponseTooLarge):
				contentType, generic := imageContentType(fetched.ContentType)
				if !generic && !imageTypes[contentType] {
					return imageResult{status: http.StatusNotFound}, nil
				}
				return imageResult{status: http.StatusRequestEntityTooLarge}, nil
			case errors.Is(err, vrchat.ErrInvalidRedirect), errors.Is(err, vrchat.ErrInvalidAssetURL):
				return imageResult{status: http.StatusNotFound}, nil
			default:
				return res, err
			}
		}
		if fetched.RedirectURL != nil {
			if !allowedImage(fetched.RedirectURL) {
				return imageResult{status: http.StatusNotFound}, nil
			}
			target = fetched.RedirectURL
			continue
		}
		img, err := readImage(fetched)
		img.private = res.private
		return img, err
	}
	return imageResult{status: http.StatusNotFound}, nil
}

func imageContentType(value string) (contentType string, generic bool) {
	contentType, _, _ = strings.Cut(value, ";")
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	generic = contentType == "application/octet-stream" || contentType == "binary/octet-stream" || contentType == ""
	return contentType, generic
}

func readImage(resp vrchat.AssetResponse) (imageResult, error) {
	contentType, generic := imageContentType(resp.ContentType)
	if resp.StatusCode == http.StatusTooManyRequests {
		return imageResult{status: http.StatusTooManyRequests}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 || (!imageTypes[contentType] && !generic) {
		return imageResult{status: http.StatusNotFound}, nil
	}
	if resp.ContentLength > maxImageBytes || len(resp.Body) > maxImageBytes {
		return imageResult{status: http.StatusRequestEntityTooLarge}, nil
	}
	if generic {
		contentType = http.DetectContentType(resp.Body)
		if !imageTypes[contentType] {
			return imageResult{status: http.StatusNotFound}, nil
		}
	}
	return imageResult{status: http.StatusOK, contentType: contentType, body: resp.Body}, nil
}

func writeImage(w http.ResponseWriter, img imageResult) error {
	if img.status != http.StatusOK {
		w.WriteHeader(img.status)
		return nil
	}
	h := w.Header()
	h.Set("Content-Type", img.contentType)
	h.Set("Cache-Control", "private, no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	w.Write(img.body)
	return nil
}
