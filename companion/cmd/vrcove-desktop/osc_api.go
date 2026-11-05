package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"vrcove/companion/internal/history"
	"vrcove/companion/internal/osc"
)

const (
	defaultOSCListen = 9001
	// maxOSCValues bounds what a noisy sender can make the app remember.
	maxOSCValues  = 2048
	afkAddress    = "/avatar/parameters/AFK"
	paramPrefix   = "/avatar/parameters/"
	avatarAddress = "/avatar/change"
)

// oscLink is the connection from VRChat to the app. It runs only while the
// player has it on, because another OSC app may need the same port, and it
// starts off each time the app opens.
type oscLink struct {
	mu     sync.Mutex
	stop   context.CancelFunc
	port   int
	avatar *osc.Avatar
	// values holds the latest parameter values VRChat reported.
	values   map[string]any
	afk      bool
	afkStart time.Time
	// parameter files are read from here. Tests replace folder.
	folder func() string
}

func (d *desktop) oscFolder() string {
	if d.osc.folder != nil {
		return d.osc.folder()
	}
	return osc.Folder(history.LogFolder(d.analyticsFolder()))
}

// afkSince reports when VRChat said the player went AFK.
func (l *oscLink) afkSince() (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.afkStart, l.afk
}

// receive stores one message from VRChat.
func (d *desktop) receive(m osc.Message) {
	l := &d.osc
	// A packet read just before stopOSC can still arrive here.
	l.mu.Lock()
	running := l.stop != nil
	l.mu.Unlock()
	if !running {
		return
	}
	if m.Address == avatarAddress && len(m.Args) == 1 {
		if id, ok := m.Args[0].(string); ok {
			d.switchAvatar(id)
		}
		return
	}
	name, ok := strings.CutPrefix(m.Address, paramPrefix)
	if !ok || len(m.Args) != 1 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.values == nil {
		return
	}
	if _, known := l.values[name]; known || len(l.values) < maxOSCValues {
		l.values[name] = m.Args[0]
	}
	if m.Address == afkAddress {
		afk, _ := m.Args[0].(bool)
		if afk && !l.afk {
			l.afkStart = time.Now()
		}
		l.afk = afk
	}
}

// switchAvatar loads the new avatar's parameters and forgets the old values.
// VRChat hasn't always written the file yet, so a failure leaves no avatar and
// the player can read it again.
func (d *desktop) switchAvatar(id string) {
	avatar, err := osc.LoadAvatar(d.oscFolder(), id)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Print("could not read avatar parameters: ", err)
	}
	l := &d.osc
	l.mu.Lock()
	defer l.mu.Unlock()
	l.avatar = avatar
	if l.values != nil {
		l.values = map[string]any{}
	}
}

func (d *desktop) serveOSC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			Action string          `json:"action"`
			Port   int             `json:"port"`
			Name   string          `json:"name"`
			Value  json.RawMessage `json:"value"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid OSC request."})
			return
		}
		var err error
		switch body.Action {
		case "listen":
			err = d.startOSC(body.Port)
		case "stop":
			d.stopOSC()
		case "reload":
			d.reloadAvatar()
		case "set":
			err = d.setParameter(body.Name, body.Value)
		default:
			err = errors.New("Unknown OSC action.")
		}
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, 200, d.oscStatus())
}

func (d *desktop) startOSC(port int) error {
	if port == 0 {
		port = defaultOSCListen
	}
	if port < 1024 || port > 65535 {
		return errors.New("Choose an OSC port between 1024 and 65535.")
	}
	l := &d.osc
	l.mu.Lock()
	running := l.stop != nil && l.port == port
	l.mu.Unlock()
	if running {
		return nil
	}
	d.stopOSC()
	parent := d.toolContext
	if parent == nil {
		parent = context.Background()
	}
	ctx, stop := context.WithCancel(parent)
	l.mu.Lock()
	l.values = map[string]any{}
	l.mu.Unlock()
	if err := osc.Listen(ctx, port, d.receive); err != nil {
		stop()
		return errors.New("Another app is using that port. Close it or choose another port.")
	}
	l.mu.Lock()
	l.stop, l.port = stop, port
	l.mu.Unlock()
	d.reloadAvatar()
	return nil
}

func (d *desktop) stopOSC() {
	l := &d.osc
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stop != nil {
		l.stop()
	}
	l.stop, l.port, l.afk, l.values = nil, 0, false, nil
}

// reloadAvatar reads the current avatar again, or the newest file if VRChat
// hasn't said which avatar this is.
func (d *desktop) reloadAvatar() {
	l := &d.osc
	l.mu.Lock()
	current := l.avatar
	l.mu.Unlock()
	folder := d.oscFolder()
	var avatar *osc.Avatar
	var err error
	if current != nil {
		avatar, err = osc.LoadAvatar(folder, current.ID)
	} else {
		avatar, err = osc.LatestAvatar(folder)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Print("could not read avatar parameters: ", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.avatar = avatar
}

// setParameter sends a value to a parameter of the current avatar. Only
// parameters VRChat listed as inputs can be set, at the address it gave.
func (d *desktop) setParameter(name string, raw json.RawMessage) error {
	l := &d.osc
	l.mu.Lock()
	var parameter *osc.Parameter
	if l.avatar != nil {
		for i := range l.avatar.Parameters {
			if l.avatar.Parameters[i].Name == name {
				parameter = &l.avatar.Parameters[i]
			}
		}
	}
	l.mu.Unlock()
	if parameter == nil || parameter.Input == "" {
		return errors.New("This avatar can't receive that parameter.")
	}
	value, err := parameterValue(parameter.Type, raw)
	if err != nil {
		return err
	}
	packet, err := osc.Encode(osc.Message{Address: parameter.Input, Args: []any{value}})
	if err != nil {
		return err
	}
	d.tools.mu.Lock()
	port, write := d.tools.prefs.Port, d.tools.sender.Write
	d.tools.mu.Unlock()
	if write == nil {
		write = osc.Send
	}
	if err := write(port, packet); err != nil {
		return errors.New("Could not send to VRChat. Check that VRChat and OSC are running.")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.values != nil {
		l.values[name] = value
	}
	return nil
}

// parameterValue checks a value against VRChat's range for the type.
func parameterValue(kind string, raw json.RawMessage) (any, error) {
	switch kind {
	case "Bool":
		var v bool
		if json.Unmarshal(raw, &v) == nil {
			return v, nil
		}
	case "Int":
		var v int32
		if json.Unmarshal(raw, &v) == nil && v >= 0 && v <= 255 {
			return v, nil
		}
	case "Float":
		var v float32
		if json.Unmarshal(raw, &v) == nil && v >= -1 && v <= 1 {
			return v, nil
		}
	}
	return nil, errors.New("Use on or off, a whole number from 0 to 255, or a decimal from -1 to 1.")
}

func (d *desktop) oscStatus() map[string]any {
	l := &d.osc
	l.mu.Lock()
	defer l.mu.Unlock()
	parameters := []map[string]any{}
	status := map[string]any{"listening": l.stop != nil, "port": l.port, "defaultPort": defaultOSCListen, "afk": l.afk, "avatarId": "", "avatarName": "", "parameters": parameters}
	if l.avatar == nil {
		return status
	}
	for _, p := range l.avatar.Parameters {
		parameter := map[string]any{"name": p.Name, "type": p.Type, "writable": p.Input != ""}
		if value, ok := l.values[p.Name]; ok {
			parameter["value"] = value
		}
		parameters = append(parameters, parameter)
	}
	status["avatarId"], status["avatarName"], status["parameters"] = l.avatar.ID, l.avatar.Name, parameters
	return status
}
