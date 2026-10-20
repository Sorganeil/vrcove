package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"
	"vrcove/companion/internal/buildinfo"
	"vrcove/companion/internal/vrchat"
)

// verifier lets the upload pipeline use the desktop's VRChat login.
type verifier struct{ c *vrchat.Client }

func (v verifier) Ready() bool { return v.c.Ready() }
func (v verifier) Check(ctx context.Context, id string) (bool, error) {
	verdict, err := v.c.Check(ctx, id)
	return verdict == vrchat.Public, err
}

// async runs a login operation off the GUI thread and reports its error.
func (g *gui) async(op func() error) {
	g.loginMsg = ""
	go func() {
		msg := ""
		if err := op(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, vrchat.ErrChanged) {
			msg = err.Error()
		}
		select {
		case <-g.loginReply:
		default:
		}
		select {
		case g.loginReply <- msg:
		default:
		}
		g.invalidate()
	}()
}

func (g *gui) loginActions(gtx layout.Context) {
	if g.manage.Clicked(gtx) {
		g.login, g.loginMsg = true, ""
		g.list.Position = layout.Position{}
	}
	if g.back.Clicked(gtx) {
		g.login, g.loginMsg = false, ""
		g.password.SetText("")
		g.code.SetText("")
		g.list.Position = layout.Position{}
	}
	info := g.vrc.Info()
	if g.signIn.Clicked(gtx) && !info.Busy {
		username, password := g.username.Text(), g.password.Text()
		// The password goes to VRChat once and is not kept.
		g.password.SetText("")
		g.async(func() error { return g.vrc.Login(username, password) })
	}
	if g.verify.Clicked(gtx) && !info.Busy {
		method, code := g.method.Value, g.code.Text()
		g.code.SetText("")
		g.async(func() error { return g.vrc.Verify(method, code) })
	}
	if g.signOut.Clicked(gtx) {
		g.password.SetText("")
		g.code.SetText("")
		g.async(g.vrc.Logout)
	}
}

func loginStatus(info vrchat.Info) string {
	switch info.State {
	case vrchat.Loading:
		return "Reading the saved login…"
	case vrchat.NeedsCode:
		return "Waiting for a verification code"
	case vrchat.Ready:
		return "Signed in as " + info.Name
	}
	if info.Busy {
		return "Signing in…"
	}
	return "Not signed in"
}

func (g *gui) loginRow(gtx layout.Context) layout.Dimensions {
	info := g.vrc.Info()
	title := "Sign in"
	if info.State == vrchat.Ready || info.State == vrchat.NeedsCode {
		title = "Manage"
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			children := []layout.FlexChild{layout.Rigid(g.label("VRChat login", 13, fg, false)), gap(2), layout.Rigid(g.label(loginStatus(info), 12, muted, false))}
			if info.Message != "" {
				children = append(children, gap(2), layout.Rigid(g.label(info.Message, 12, warn, false)))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		}), hgap(10), layout.Rigid(g.button(&g.manage, title, false)))
}

func (g *gui) loginView(gtx layout.Context) layout.Dimensions {
	info := g.vrc.Info()
	items := []layout.Widget{
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, g.label("VRChat login", 17, fg, true)), layout.Rigid(g.label(buildinfo.Version, 12, muted, false)))
		},
		space(4),
		g.label("Used to check avatars on this PC before upload. Your password goes only to VRChat and isn’t saved. The session is kept in Windows Credential Manager or your system keyring.", 12, muted, false),
		space(12),
	}
	if g.preview {
		items = append(items, g.label("Preview: any login works and nothing is sent to VRChat.", 12, accent, false), space(10))
	}
	busy := func(title string) string {
		if info.Busy {
			return "Contacting VRChat…"
		}
		return title
	}
	switch info.State {
	case vrchat.Loading:
		items = append(items, g.label("Reading the saved login. Your keyring may ask you to unlock it.", 13, fg, false))
	case vrchat.SignedOut:
		items = append(items,
			g.field("Username or email", "VRChat username", &g.username), space(10),
			g.field("Password", "VRChat password", &g.password), space(10),
			func(gtx layout.Context) layout.Dimensions {
				children := []layout.FlexChild{layout.Rigid(g.button(&g.signIn, busy("Sign in to VRChat"), true))}
				if info.Busy {
					children = append(children, hgap(10), layout.Rigid(g.button(&g.signOut, "Cancel", false)))
				}
				return layout.Flex{}.Layout(gtx, children...)
			})
	case vrchat.NeedsCode:
		// VRChat says which codes it accepts: email-code accounts get emailOtp,
		// authenticator accounts get totp plus recovery codes (otp).
		if len(info.Methods) > 0 && !slices.Contains(info.Methods, g.method.Value) {
			g.method.Value = info.Methods[0]
			if slices.Contains(info.Methods, vrchat.Authenticator) {
				g.method.Value = vrchat.Authenticator
			}
		}
		other := ""
		switch {
		case g.method.Value == vrchat.Authenticator && slices.Contains(info.Methods, vrchat.RecoveryCode):
			other = vrchat.RecoveryCode
		case g.method.Value == vrchat.RecoveryCode && slices.Contains(info.Methods, vrchat.Authenticator):
			other = vrchat.Authenticator
		}
		if other != "" && g.methods[0].Clicked(gtx) {
			g.method.Value, other = other, g.method.Value
			g.code.SetText("")
		}
		title, hint := "Code from your authenticator app", "6-digit code"
		switch g.method.Value {
		case vrchat.EmailCode:
			title = "Code VRChat emailed you"
		case vrchat.RecoveryCode:
			title, hint = "Recovery code", "For example abcd-1234"
		}
		items = append(items, g.field(title, hint, &g.code), space(10),
			func(gtx layout.Context) layout.Dimensions {
				children := []layout.FlexChild{layout.Rigid(g.button(&g.verify, busy("Verify"), true)), hgap(8), layout.Rigid(g.button(&g.signOut, "Start over", false))}
				if other != "" {
					switch_ := "Use a recovery code instead"
					if other == vrchat.Authenticator {
						switch_ = "Use your authenticator app instead"
					}
					children = append(children, layout.Flexed(1, layout.Spacer{}.Layout), layout.Rigid(g.link(&g.methods[0], switch_)))
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
			})
	case vrchat.Ready:
		items = append(items, g.label(loginStatus(info), 14, fg, true), space(4),
			g.label("Signing out removes the saved session from this PC and ends it at VRChat. Your queue stays saved.", 12, muted, false), space(10),
			func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{}.Layout(gtx, layout.Rigid(g.button(&g.signOut, "Sign out", false)))
			})
	}
	for _, msg := range []string{info.Message, g.loginMsg} {
		if msg != "" {
			items = append(items, space(10), g.label(msg, 12, warn, false))
		}
	}
	items = append(items, space(14), func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{}.Layout(gtx, layout.Rigid(g.button(&g.back, "Back", false)))
	})
	return g.list.Layout(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions { return items[i](gtx) })
}

// previewVRChat answers the login flow locally, with no network or keyring.
func previewVRChat(onChange func()) *vrchat.Client {
	return vrchat.NewWith("https://vrchat.preview.invalid/api/1", &http.Client{Transport: previewTransport{}}, &previewStore{}, onChange)
}

type previewStore struct {
	mu    sync.Mutex
	value string
}

func (s *previewStore) Get() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.value == "" {
		return "", vrchat.ErrNoLogin
	}
	return s.value, nil
}
func (s *previewStore) Set(v string) error { s.mu.Lock(); defer s.mu.Unlock(); s.value = v; return nil }
func (s *previewStore) Delete() error      { return s.Set("") }

type previewTransport struct{}

func (previewTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		_ = r.Body.Close()
	}
	select {
	case <-time.After(400 * time.Millisecond):
	case <-r.Context().Done():
		return nil, r.Context().Err()
	}
	h := http.Header{"Content-Type": {"application/json"}}
	body := `{}`
	switch {
	case strings.HasSuffix(r.URL.Path, "/auth/user") && r.Header.Get("Authorization") != "":
		h.Add("Set-Cookie", "auth=preview-session; Path=/")
		body = `{"requiresTwoFactorAuth":["totp","otp"]}`
	case strings.HasSuffix(r.URL.Path, "/verify"):
		h.Add("Set-Cookie", "twoFactorAuth=preview-2fa; Path=/")
		body = `{"verified":true}`
	case strings.HasSuffix(r.URL.Path, "/auth/user"):
		body = `{"id":"usr_00000000-0000-0000-0000-000000000000","displayName":"Preview user"}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

// link is a borderless text button for secondary choices.
func (g *gui) link(click *widget.Clickable, title string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(4).Layout(gtx, g.label(title, 12, accent, false))
		})
	}
}
