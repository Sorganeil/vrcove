package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// exportAccount sends everything VRCove stores about the signed-in account as
// a JSON download. It asks for the password again, because it includes the
// private VRChat links. Secrets are left out: passwords and recovery codes are
// only stored as hashes, and session tokens and API keys are never exported.
func (s *Server) exportAccount(w http.ResponseWriter, r *http.Request, a *account) error {
	if !s.limiter.allow(reauthKey(a), s.now()) {
		return errStatus(http.StatusTooManyRequests, "Too many attempts. Wait 10 minutes.")
	}
	body, err := requestJSON(r)
	if err != nil {
		return err
	}
	password, _ := str(body, "password")
	if ok, err := s.checkPassword(r.Context(), a.Username, password); err != nil || ok == nil {
		if err == nil {
			writeError(w, http.StatusUnauthorized, "Your password is wrong.")
		}
		return err
	}
	data, err := s.accountData(r.Context(), a)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="vrcove-data-%s-%s.json"`, a.Username, s.now().UTC().Format("2006-01-02")))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(out.Bytes())
	return err
}

func unixTime(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}

// accountData collects the account's stored data. Each query reads only rows
// owned by a.ID.
func (s *Server) accountData(ctx context.Context, a *account) (map[string]any, error) {
	db := s.accounts
	var created, bio, status, role string
	var public bool
	if err := db.QueryRowContext(ctx, "SELECT created, bio, status, public, role FROM accounts WHERE id = ?", a.ID).
		Scan(&created, &bio, &status, &public, &role); err != nil {
		return nil, err
	}
	var recoveryCodes int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM account_recovery_codes WHERE account_id = ?", a.ID).Scan(&recoveryCodes); err != nil {
		return nil, err
	}
	var picture any
	var image []byte
	err := db.QueryRowContext(ctx, "SELECT image FROM account_pictures WHERE account_id = ?", a.ID).Scan(&image)
	if err == nil {
		picture = "data:image/png;base64," + base64.StdEncoding.EncodeToString(image)
	}

	// Linked VRChat accounts are sealed; their sessions refer to them by an HMAC.
	links, linkNames := []map[string]any{}, map[string]string{}
	rows, err := db.QueryContext(ctx, "SELECT sealed, created FROM vrchat_links WHERE account_id = ? ORDER BY id", a.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var sealed, linked string
		if err := rows.Scan(&sealed, &linked); err != nil {
			rows.Close()
			return nil, err
		}
		var rec linkRecord
		if plain, err := openBytes(s.keys.linkSeal, sealed, linkAAD); err == nil {
			json.Unmarshal(plain, &rec)
		}
		links = append(links, map[string]any{"vrchatId": rec.VRChatID, "displayName": rec.DisplayName, "username": rec.Username, "linked": linked})
		if rec.VRChatID != "" {
			linkNames[string(s.linkKey(rec.VRChatID))] = rec.VRChatID
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sessions := []map[string]any{}
	rows, err = db.QueryContext(ctx, "SELECT vrc_key, device, browser, created, expires FROM vrchat_sessions WHERE account_id = ? ORDER BY id", a.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key []byte
		var device, browser, started string
		var expires int64
		if err := rows.Scan(&key, &device, &browser, &started, &expires); err != nil {
			rows.Close()
			return nil, err
		}
		sessions = append(sessions, map[string]any{"vrchatId": linkNames[string(key)], "device": device, "browser": browser, "created": started, "expires": unixTime(expires)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	keys := []map[string]any{}
	rows, err = db.QueryContext(ctx, "SELECT created FROM logger_keys WHERE account_id = ? ORDER BY id", a.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var made int64
		if err := rows.Scan(&made); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, map[string]any{"created": unixTime(made)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	reviews := []map[string]any{}
	rows, err = db.QueryContext(ctx, "SELECT kind, target, target_name, rating, body, created, updated FROM reviews WHERE account_id = ? ORDER BY id", a.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind, target, name, text string
		var rating int
		var made, changed int64
		if err := rows.Scan(&kind, &target, &name, &rating, &text, &made, &changed); err != nil {
			rows.Close()
			return nil, err
		}
		reviews = append(reviews, map[string]any{"kind": kind, "target": target, "name": name, "rating": rating, "body": text, "created": unixTime(made), "updated": unixTime(changed)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	collections := []map[string]any{}
	rows, err = db.QueryContext(ctx, "SELECT id, kind, name, description, public, created, updated FROM collections WHERE account_id = ? ORDER BY created", a.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, kind, name, description string
		var isPublic bool
		var made, changed int64
		if err := rows.Scan(&id, &kind, &name, &description, &isPublic, &made, &changed); err != nil {
			rows.Close()
			return nil, err
		}
		collections = append(collections, map[string]any{"id": id, "kind": kind, "name": name, "description": description, "public": isPublic, "created": unixTime(made), "updated": unixTime(changed)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, c := range collections {
		items := []map[string]any{}
		rows, err := db.QueryContext(ctx, "SELECT target, name, image, added FROM collection_items WHERE collection_id = ? ORDER BY added", c["id"])
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var target, name, image string
			var added int64
			if err := rows.Scan(&target, &name, &image, &added); err != nil {
				rows.Close()
				return nil, err
			}
			items = append(items, map[string]any{"target": target, "name": name, "image": image, "added": unixTime(added)})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		c["items"] = items
	}

	// Reports and removal requests this account sent. Copies of other people's
	// reported content stay out of the export.
	reports := []map[string]any{}
	rows, err = db.QueryContext(ctx, "SELECT kind, reason, details, status, created FROM reports WHERE reporter_id = ? ORDER BY id", a.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind, reason, details, state string
		var made int64
		if err := rows.Scan(&kind, &reason, &details, &state, &made); err != nil {
			rows.Close()
			return nil, err
		}
		reports = append(reports, map[string]any{"kind": kind, "reason": reason, "details": details, "status": state, "created": unixTime(made)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	requests := []map[string]any{}
	rows, err = db.QueryContext(ctx, "SELECT avatar_id, avatar_name, proof, verified, status, created FROM blacklist_requests WHERE account_id = ? ORDER BY id", a.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var avatar, name, proof, state string
		var verified bool
		var made int64
		if err := rows.Scan(&avatar, &name, &proof, &verified, &state, &made); err != nil {
			rows.Close()
			return nil, err
		}
		requests = append(requests, map[string]any{"avatarId": avatar, "avatarName": name, "proof": proof, "verifiedOwner": verified, "status": state, "created": unixTime(made)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return map[string]any{
		"format":   "vrcove-account-data-1",
		"exported": s.now().UTC().Format(time.RFC3339),
		"note":     "Everything VRCove stores about this account. Your password and recovery codes are stored only as hashes, so they can't be exported. Session tokens and API keys are left out for safety.",
		"account": map[string]any{"username": a.Username, "created": created, "bio": bio, "status": status, "publicProfile": public, "role": role,
			"recoveryCodesRemaining": recoveryCodes},
		"picture":          picture,
		"vrchatLinks":      links,
		"vrchatSessions":   sessions,
		"companionApiKeys": keys,
		"reviews":          reviews,
		"collections":      collections,
		"reportsFiled":     reports,
		"removalRequests":  requests,
	}, nil
}
