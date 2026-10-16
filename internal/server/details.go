package server

import (
	"fmt"
	"regexp"
	"strconv"
)

// Explicit field mappers for VRChat detail pages. Only listed fields reach
// the browser; a field VRChat omits stays omitted, while null passes through.

var (
	worldID  = regexp.MustCompile(`^wrld_[a-f0-9-]{36}$`)
	location = regexp.MustCompile(`^wrld_[a-f0-9-]{36}:[A-Za-z0-9~()_.-]{1,200}$`)
)

type object = map[string]any

func asObject(v any) object {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) ([]any, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected VRChat list")
	}
	return list, nil
}

// pick copies the named fields that are present in src.
func pick(dst, src object, keys ...string) object {
	for _, k := range keys {
		if v, ok := src[k]; ok {
			dst[k] = v
		}
	}
	return dst
}

// rename copies src[from] to dst[to] when present.
func rename(dst, src object, pairs ...string) object {
	for i := 0; i+1 < len(pairs); i += 2 {
		if v, ok := src[pairs[i+1]]; ok {
			dst[pairs[i]] = v
		}
	}
	return dst
}

// truthy follows JavaScript truthiness for decoded JSON values.
func truthy(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case interface{ String() string }: // json.Number
		f, _ := strconv.ParseFloat(v.String(), 64)
		return f != 0 && f == f
	}
	return true
}

// firstTruthy mirrors a || b chains; the result is omitted when all are falsy.
func firstTruthy(dst object, key string, values ...any) {
	var last any
	present := false
	for _, v := range values {
		if v != nil {
			last, present = v, true
		}
		if truthy(v) {
			dst[key] = v
			return
		}
	}
	if present {
		dst[key] = last
	}
}

func isLocation(v any) bool {
	s, ok := v.(string)
	return ok && location.MatchString(s)
}

func publicWorld(data any) (any, error) {
	w := asObject(data)
	out := pick(object{}, w, "id", "name", "description", "authorId", "authorName", "imageUrl", "thumbnailImageUrl",
		"capacity", "recommendedCapacity", "occupants", "publicOccupants", "privateOccupants", "visits", "favorites",
		"heat", "popularity", "releaseStatus", "publicationDate", "tags")
	rename(out, w, "created", "created_at", "updated", "updated_at")
	platforms := []string{}
	seen := map[string]bool{}
	packages, _ := w["unityPackages"].([]any)
	for _, p := range packages {
		if name, ok := asObject(p)["platform"].(string); ok && !seen[name] {
			seen[name] = true
			platforms = append(platforms, name)
		}
	}
	out["platforms"] = platforms
	instances := []object{}
	list, _ := w["instances"].([]any)
	id, _ := w["id"].(string)
	for _, inst := range list {
		pair, ok := inst.([]any)
		if !ok || len(pair) == 0 {
			continue
		}
		name, ok := pair[0].(string)
		if !ok || !location.MatchString(id+":"+name) {
			continue
		}
		users := 0.0
		if len(pair) > 1 {
			if n, ok := pair[1].(interface{ String() string }); ok {
				if f, err := strconv.ParseFloat(n.String(), 64); err == nil {
					users = f
				}
			}
		}
		instances = append(instances, object{"id": name, "users": users})
	}
	out["instances"] = instances
	return out, nil
}

func publicProfile(data any) (any, error) {
	u := asObject(data)
	out := publicUser(data)
	delete(out, "location")
	pick(out, u, "pronouns", "bioLinks", "worldId", "instanceId")
	rename(out, u, "dateJoined", "date_joined", "lastLogin", "last_login", "lastPlatform", "last_platform")
	out["isFriend"] = truthy(u["isFriend"])
	if loc, _ := u["location"].(string); isLocation(loc) || loc == "private" || loc == "offline" || loc == "traveling" {
		out["location"] = loc
	}
	badges := []object{}
	list, _ := u["badges"].([]any)
	for _, b := range list {
		badge := asObject(b)
		if name, ok := badge["badgeName"].(string); !ok || name == "" || badge["showcased"] == false {
			continue
		}
		badges = append(badges, rename(object{}, badge, "id", "badgeId", "name", "badgeName", "description", "badgeDescription", "imageUrl", "badgeImageUrl"))
	}
	out["badges"] = badges
	return out, nil
}

func mapList(data any, each func(object) (object, bool)) (any, error) {
	list, err := asList(data)
	if err != nil {
		return nil, err
	}
	out := []object{}
	for _, item := range list {
		if mapped, ok := each(asObject(item)); ok {
			out = append(out, mapped)
		}
	}
	return out, nil
}

func worldSummaries(data any) (any, error) {
	return mapList(data, func(w object) (object, bool) {
		return pick(object{}, w, "id", "name", "authorId", "authorName", "thumbnailImageUrl", "occupants", "favorites"), true
	})
}

func userGroups(data any) (any, error) {
	return mapList(data, func(g object) (object, bool) {
		out := pick(object{}, g, "name", "shortCode", "discriminator", "iconUrl", "bannerUrl", "memberCount")
		firstTruthy(out, "id", g["groupId"], g["id"])
		out["isRepresenting"] = truthy(g["isRepresenting"])
		return out, true
	})
}

func publicGroup(data any) (any, error) {
	g := asObject(data)
	out := pick(object{}, g, "id", "name", "shortCode", "discriminator", "description", "iconUrl", "bannerUrl",
		"memberCount", "onlineMemberCount", "ownerId", "createdAt", "joinState", "privacy", "rules", "links",
		"languages", "tags", "membershipStatus")
	out["isVerified"] = truthy(g["isVerified"])
	return out, nil
}

func groupPosts(data any) (any, error) {
	posts, ok := asObject(data)["posts"]
	if !ok {
		return []object{}, nil
	}
	return mapList(posts, func(p object) (object, bool) {
		return pick(object{}, p, "id", "title", "text", "imageUrl", "authorId", "createdAt"), true
	})
}

func groupMembers(data any) (any, error) {
	return mapList(data, func(m object) (object, bool) {
		user := asObject(m["user"])
		out := pick(object{}, m, "joinedAt")
		firstTruthy(out, "id", m["userId"], user["id"])
		rename(out, user, "displayName", "displayName", "profilePicOverride", "profilePicOverride")
		firstTruthy(out, "userIcon", user["iconUrl"], user["userIcon"])
		firstTruthy(out, "currentAvatarThumbnailImageUrl", user["thumbnailUrl"], user["currentAvatarThumbnailImageUrl"])
		return out, true
	})
}

func groupInstances(data any) (any, error) {
	return mapList(data, func(inst object) (object, bool) {
		world := asObject(inst["world"])
		loc, ok := inst["location"].(string)
		if !ok || loc == "" {
			wid, _ := world["id"].(string)
			iid, _ := inst["instanceId"].(string)
			loc = wid + ":" + iid
		}
		if !location.MatchString(loc) {
			return nil, false
		}
		out := object{"location": loc}
		if users, ok := inst["memberCount"]; ok && users != nil {
			out["users"] = users
		} else if users, ok := inst["n_users"]; ok {
			out["users"] = users
		}
		rename(out, world, "worldName", "name", "worldImage", "thumbnailImageUrl", "capacity", "capacity")
		return out, true
	})
}
