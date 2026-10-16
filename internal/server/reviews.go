package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Reviews: one rating (1–5) with an optional comment per VRCove account and
// target. They are public, signed with the VRCove username only, and dated
// to the day. Posting one never contacts VRChat.

var reviewKinds = map[string]*regexp.Regexp{"user": userID, "world": worldID, "avatar": avatarID, "group": groupID}

const (
	maxReviewLength = 2000
	reviewPageSize  = 50
)

// reviewTarget validates the kind and id fields of a query or JSON body.
func reviewTarget(kind, id string) (string, string, bool) {
	pattern, ok := reviewKinds[kind]
	return kind, id, ok && pattern.MatchString(id)
}

func (s *Server) listReviews(w http.ResponseWriter, r *http.Request, a *account) error {
	query := r.URL.Query()
	kind, id, ok := reviewTarget(query.Get("kind"), query.Get("id"))
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid review target")
		return nil
	}
	linked := false
	if a != nil {
		var err error
		linked, err = s.hasVRChatLink(r, a, "")
		if err != nil {
			return err
		}
	}
	ctx := r.Context()
	distribution := make([]int, 5)
	rows, err := s.accounts.QueryContext(ctx, "SELECT rating, count(*) FROM reviews WHERE kind = ? AND target = ? GROUP BY rating", kind, id)
	if err != nil {
		return err
	}
	total, sum := 0, 0
	for rows.Next() {
		var rating, n int
		if err := rows.Scan(&rating, &n); err != nil {
			rows.Close()
			return err
		}
		distribution[rating-1] = n
		total += n
		sum += rating * n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var accountID int64 = -1
	if a != nil {
		accountID = a.ID
	}
	rows, err = s.accounts.QueryContext(ctx, `SELECT r.id, a.username, a.public, r.rating, r.body, r.created, r.updated, r.account_id = ?
		FROM reviews r JOIN accounts a ON a.id = r.account_id WHERE r.kind = ? AND r.target = ?
		ORDER BY r.account_id = ? DESC, r.updated DESC, r.id DESC LIMIT ? OFFSET ?`,
		accountID, kind, id, accountID, reviewPageSize, queryInt(query.Get("offset"), 100000))
	if err != nil {
		return err
	}
	defer rows.Close()
	reviews := []map[string]any{}
	var mine map[string]any
	for rows.Next() {
		var rid int64
		var author, body string
		var rating int
		var created, updated int64
		var own, profile bool
		if err := rows.Scan(&rid, &author, &profile, &rating, &body, &created, &updated, &own); err != nil {
			return err
		}
		review := map[string]any{"id": rid, "author": author, "rating": rating, "body": body,
			"date": s.day(time.Unix(created, 0)), "edited": s.day(time.Unix(updated, 0)) != s.day(time.Unix(created, 0)), "mine": own, "profile": profile}
		if own {
			mine = review
		}
		reviews = append(reviews, review)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	average := 0.0
	if total > 0 {
		average = float64(sum) / float64(total)
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": total, "average": average, "distribution": distribution,
		"reviews": reviews, "mine": mine, "signedIn": a != nil, "linked": linked, "pageSize": reviewPageSize})
	return nil
}

// cleanReview normalizes line endings and trims the comment. It rejects
// control characters other than newlines and tabs.
func cleanReview(body string) (string, bool) {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	for _, c := range body {
		if (unicode.IsControl(c) && c != '\n' && c != '\t') || c == ' ' || c == ' ' {
			return "", false
		}
	}
	return body, true
}

func (s *Server) saveReview(w http.ResponseWriter, r *http.Request, a *account) error {
	if !s.limiter.allow("veil-review:"+strings.ToLower(a.Username), s.now()) {
		return errStatus(http.StatusTooManyRequests, "You’re posting too quickly. Wait a few minutes.")
	}
	in, err := requestJSON(r)
	if err != nil {
		return err
	}
	kindText, _ := str(in, "kind")
	idText, _ := str(in, "id")
	kind, id, ok := reviewTarget(kindText, idText)
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid review target")
		return nil
	}
	n, _ := in["rating"].(json.Number)
	rating, err := n.Int64()
	if err != nil || rating < 1 || rating > 5 {
		writeError(w, http.StatusBadRequest, "Choose a rating from 1 to 5 stars.")
		return nil
	}
	nameText, _ := str(in, "name")
	name := s.targetName(r.Context(), kind, id, nameText)
	text, _ := str(in, "body")
	body, ok := cleanReview(text)
	if !ok || jsLength(body) > maxReviewLength {
		writeError(w, http.StatusBadRequest, "Comments are plain text of up to 2000 characters.")
		return nil
	}
	ctx := r.Context()
	if kind == "user" {
		var linked int
		err := s.accounts.QueryRowContext(ctx, "SELECT 1 FROM vrchat_links WHERE account_id = ? AND vrc_key = ?", a.ID, s.linkKey(id)).Scan(&linked)
		if err == nil {
			writeError(w, http.StatusForbidden, "You can’t review a VRChat account you’ve linked.")
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	now := s.now().Unix()
	if _, err := s.accounts.ExecContext(ctx, `INSERT INTO reviews (account_id, kind, target, rating, body, created, updated, target_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (account_id, kind, target) DO UPDATE SET rating = excluded.rating, body = excluded.body, updated = excluded.updated,
		target_name = CASE WHEN excluded.target_name != '' THEN excluded.target_name ELSE reviews.target_name END`,
		a.ID, kind, id, rating, body, now, now, name); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func (s *Server) deleteReview(w http.ResponseWriter, r *http.Request, a *account) error {
	in, err := requestJSON(r)
	if err != nil {
		return err
	}
	kindText, _ := str(in, "kind")
	idText, _ := str(in, "id")
	kind, id, ok := reviewTarget(kindText, idText)
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid review target")
		return nil
	}
	if _, err := s.accounts.ExecContext(r.Context(), "DELETE FROM reviews WHERE account_id = ? AND kind = ? AND target = ?", a.ID, kind, id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}
