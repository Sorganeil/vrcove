package server

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"net/http"
	"net/url"
	"strings"
)

const maxAccountPictureBytes int64 = 2 << 20

func (s *Server) accountPictureURL(r *http.Request, id int64, username string) string {
	var version string
	if err := s.accounts.QueryRowContext(r.Context(), "SELECT version FROM account_pictures WHERE account_id = ?", id).Scan(&version); err != nil {
		return ""
	}
	return "/api/cove/picture?username=" + url.QueryEscape(username) + "&v=" + version
}

func (s *Server) saveAccountPicture(w http.ResponseWriter, r *http.Request, a *account) error {
	raw, err := readBounded(r.Body, maxAccountPictureBytes, "Choose an image smaller than 2 MB.")
	if err != nil {
		return err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 {
		return errStatus(http.StatusBadRequest, "Choose a PNG, JPEG or GIF image up to 4096 × 4096 pixels.")
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return errStatus(http.StatusBadRequest, "This image could not be read.")
	}
	// Keep a small square raster, stripping metadata and GIF animation.
	size := min(cfg.Width, cfg.Height)
	left, top := src.Bounds().Min.X+(cfg.Width-size)/2, src.Bounds().Min.Y+(cfg.Height-size)/2
	dst := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := range 256 {
		for x := range 256 {
			dst.Set(x, y, src.At(left+x*size/256, top+y*size/256))
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, dst); err != nil {
		return err
	}
	digest := sha256.Sum256(encoded.Bytes())
	if _, err := s.accounts.ExecContext(r.Context(), `INSERT INTO account_pictures (account_id,image,version) VALUES (?,?,?)
 ON CONFLICT(account_id) DO UPDATE SET image=excluded.image, version=excluded.version`, a.ID, encoded.Bytes(), hex.EncodeToString(digest[:])); err != nil {
		return err
	}
	return s.accountMe(w, r, a)
}

func (s *Server) accountPicture(w http.ResponseWriter, r *http.Request, viewer *account) error {
	var id int64
	var public bool
	var data []byte
	err := s.accounts.QueryRowContext(r.Context(), `SELECT a.id,a.public,p.image FROM accounts a JOIN account_pictures p ON p.account_id=a.id WHERE a.username_key=?`, strings.ToLower(r.URL.Query().Get("username"))).Scan(&id, &public, &data)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !public && (viewer == nil || viewer.ID != id)) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Write(data)
	return nil
}
