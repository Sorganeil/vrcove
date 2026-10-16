package server

import (
	"container/list"
	"context"
	"sync"
	"time"
)

const (
	// imageCacheBytes bounds the memory held by cached images.
	imageCacheBytes = 128 << 20
	// imageCacheItemBytes keeps one large image from evicting many small ones.
	imageCacheItemBytes = 2 << 20
	// VRChat file URLs carry a version, so public images can be kept a day.
	// Images fetched with a viewer's cookie expire sooner in case access is
	// revoked.
	publicImageTTL  = 24 * time.Hour
	privateImageTTL = time.Hour
)

// imageCache is a size-bounded LRU of proxied images that also merges
// concurrent fetches of the same image. Images fetched without credentials are
// shared by everyone; images fetched with a viewer's cookie are keyed by that
// VRChat user and never served to anyone else.
type imageCache struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   list.List
	size    int
	flights map[string]*imageFlight
}

type imageEntry struct {
	key     string
	img     imageResult
	expires time.Time
}

type imageFlight struct {
	done chan struct{}
	img  imageResult
	err  error
}

func publicImageKey(raw string) string        { return "p\x00" + raw }
func privateImageKey(user, raw string) string { return "u:" + user + "\x00" + raw }

// get returns a cached image for raw: the viewer's own copy first, then a
// public one.
func (c *imageCache) get(raw, user string, now time.Time) (imageResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if user != "" {
		if img, ok := c.lookup(privateImageKey(user, raw), now); ok {
			return img, true
		}
	}
	return c.lookup(publicImageKey(raw), now)
}

// lookup returns a live entry and marks it recently used. Callers hold mu.
func (c *imageCache) lookup(key string, now time.Time) (imageResult, bool) {
	el := c.entries[key]
	if el == nil {
		return imageResult{}, false
	}
	e := el.Value.(*imageEntry)
	if !now.Before(e.expires) {
		c.remove(el)
		return imageResult{}, false
	}
	c.order.MoveToFront(el)
	return e.img, true
}

// put stores a successful image, evicting the least recently used entries
// to stay within imageCacheBytes.
func (c *imageCache) put(raw, user string, img imageResult, now time.Time) {
	if len(img.body) > imageCacheItemBytes {
		return
	}
	key, ttl := publicImageKey(raw), publicImageTTL
	if img.private {
		if user == "" {
			return
		}
		key, ttl = privateImageKey(user, raw), privateImageTTL
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]*list.Element{}
	}
	if el := c.entries[key]; el != nil {
		c.remove(el)
	}
	e := &imageEntry{key: key, img: img, expires: now.Add(ttl)}
	c.entries[key] = c.order.PushFront(e)
	c.size += entrySize(e)
	for c.size > imageCacheBytes {
		c.remove(c.order.Back())
	}
}

// remove drops one entry. Callers hold mu.
func (c *imageCache) remove(el *list.Element) {
	e := c.order.Remove(el).(*imageEntry)
	delete(c.entries, e.key)
	c.size -= entrySize(e)
}

func entrySize(e *imageEntry) int { return len(e.key) + len(e.img.body) }

// do runs fetch once per key at a time; concurrent callers wait for and share
// the first caller's result. A waiter whose request ends stops waiting.
func (c *imageCache) do(ctx context.Context, key string, fetch func() (imageResult, error)) (imageResult, error) {
	c.mu.Lock()
	if f := c.flights[key]; f != nil {
		c.mu.Unlock()
		select {
		case <-f.done:
			return f.img, f.err
		case <-ctx.Done():
			return imageResult{}, ctx.Err()
		}
	}
	if c.flights == nil {
		c.flights = map[string]*imageFlight{}
	}
	f := &imageFlight{done: make(chan struct{})}
	c.flights[key] = f
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.flights, key)
		c.mu.Unlock()
		close(f.done)
	}()
	f.img, f.err = fetch()
	return f.img, f.err
}
