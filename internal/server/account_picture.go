package server

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"net/http"
	"net/url"
	"strconv"
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
	if !s.limiter.allow("cove-picture:"+strconv.FormatInt(a.ID, 10), s.now()) {
		return errStatus(http.StatusTooManyRequests, "Too many picture changes. Wait 10 minutes.")
	}
	raw, err := readBounded(r.Body, maxAccountPictureBytes, "Choose an image smaller than 2 MB.")
	if err != nil {
		return err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 {
		return errStatus(http.StatusBadRequest, "Choose a PNG, JPEG or GIF image up to 4096 × 4096 pixels.")
	}
	var encoded bytes.Buffer
	var decodeErr error
	if err := runIn(r.Context(), s.pictureSlots, func() { decodeErr = squarePicture(&encoded, raw, cfg) }); err != nil {
		return err
	}
	if decodeErr != nil {
		return decodeErr
	}
	digest := sha256.Sum256(encoded.Bytes())
	if _, err := s.accounts.ExecContext(r.Context(), `INSERT INTO account_pictures (account_id,image,version) VALUES (?,?,?)
 ON CONFLICT(account_id) DO UPDATE SET image=excluded.image, version=excluded.version`, a.ID, encoded.Bytes(), hex.EncodeToString(digest[:])); err != nil {
		return err
	}
	return s.accountMe(w, r, a)
}

// squarePicture writes a small square PNG of raw, stripping metadata and GIF
// animation. Each output pixel averages every source pixel it covers, so an
// upload cannot hide a different picture in the pixels a sampling resize keeps.
func squarePicture(out *bytes.Buffer, raw []byte, cfg image.Config) error {
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return errStatus(http.StatusBadRequest, "This image could not be read.")
	}
	size := min(cfg.Width, cfg.Height)
	left, top := src.Bounds().Min.X+(cfg.Width-size)/2, src.Bounds().Min.Y+(cfg.Height-size)/2
	crop := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(crop, crop.Bounds(), src, image.Pt(left, top), draw.Src)
	const side = 256
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := range side {
		y0, y1 := span(y, size, side)
		for x := range side {
			x0, x1 := span(x, size, side)
			var sum [4]int
			for sy := y0; sy < y1; sy++ {
				row := crop.Pix[sy*crop.Stride:]
				for sx := x0; sx < x1; sx++ {
					for c := range 4 {
						sum[c] += int(row[sx*4+c])
					}
				}
			}
			n := (y1 - y0) * (x1 - x0)
			o := dst.PixOffset(x, y)
			for c := range 4 {
				dst.Pix[o+c] = uint8((sum[c] + n/2) / n)
			}
		}
	}
	return png.Encode(out, dst)
}

// span is the range of source pixels that output pixel i of side covers in a
// source of size pixels, always at least one pixel wide.
func span(i, size, side int) (int, int) {
	a := i * size / side
	return a, max((i+1)*size/side, a+1)
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
