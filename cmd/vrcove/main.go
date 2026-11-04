// Command vrcove serves the VRCove frontend and API from one binary.
//
// Read configuration from the environment as described in README.md.
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vrcove/internal/server"
	"vrcove/web"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// listen opens LISTEN_ADDR: "unix:/path/to.sock" or a TCP host:port. When
// tunnelOnly is set, a TCP address must be loopback so the port is never
// reachable from the internet.
func listen(addr string, tunnelOnly bool) (net.Listener, error) {
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			os.Remove(path) // stale socket from an unclean exit
		}
		ln, err := net.Listen("unix", path)
		if err != nil {
			return nil, err
		}
		// Allow only the owner and group. Run cloudflared in the service's group.
		if err := os.Chmod(path, 0o660); err != nil {
			ln.Close()
			return nil, err
		}
		return ln, nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); tunnelOnly && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("refusing to listen on %s: with an https PUBLIC_ORIGIN the server only accepts tunnel traffic, so LISTEN_ADDR must be loopback or unix:/path", addr)
	}
	return net.Listen("tcp", addr)
}

func main() {
	log.SetFlags(0)
	// vrcove set-role <username> <user|admin|owner> changes a role, using the
	// same ACCOUNTS_PATH as the server. It is the only way to make an owner.
	if len(os.Args) > 1 && os.Args[1] == "set-role" {
		if len(os.Args) != 4 {
			log.Fatal("usage: vrcove set-role <username> <user|admin|owner>")
		}
		accounts, err := server.OpenAccounts(env("ACCOUNTS_PATH", ".data/accounts.sqlite"))
		if err != nil {
			log.Fatal(err)
		}
		defer accounts.Close()
		if err := server.SetRole(context.Background(), accounts, os.Args[2], os.Args[3]); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s is now %s\n", os.Args[2], os.Args[3])
		return
	}
	// vrcove bot-login signs the logger's bot account in to VRChat, asking for
	// the email or authenticator code, and stores its session for the server.
	if len(os.Args) > 1 && os.Args[1] == "bot-login" {
		botLogin()
		return
	}
	addr := env("LISTEN_ADDR", "127.0.0.1:8080")
	cfg := server.Config{
		PublicOrigin: env("PUBLIC_ORIGIN", "http://localhost:5173"),
		Contact:      os.Getenv("VRC_CONTACT"),
		VRChatWorker: os.Getenv("VRCHAT_WORKER"),
	}
	if site, secret := os.Getenv("TURNSTILE_SITE_KEY"), os.Getenv("TURNSTILE_SECRET_KEY"); site != "" && secret != "" {
		cfg.TurnstileSiteKey, cfg.TurnstileSecret = site, secret
	} else if site != "" || secret != "" {
		log.Fatal("TURNSTILE_SITE_KEY and TURNSTILE_SECRET_KEY must be set together")
	}
	// Production (an https origin) only serves Cloudflare Tunnel traffic.
	// TUNNEL_ONLY=false disables that, e.g. for debugging on the box itself.
	origin, _ := url.Parse(cfg.PublicOrigin)
	tunnelOnly, err := strconv.ParseBool(env("TUNNEL_ONLY", fmt.Sprint(origin != nil && origin.Scheme == "https")))
	if err != nil {
		log.Fatal("TUNNEL_ONLY must be true or false")
	}
	cfg.TunnelOnly = tunnelOnly
	if key := os.Getenv("SESSION_KEY"); key != "" {
		decoded, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			log.Fatal("SESSION_KEY must be base64")
		}
		cfg.SessionKey = decoded
	}

	if key := os.Getenv("ACCOUNT_KEY"); key != "" {
		decoded, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			log.Fatal("ACCOUNT_KEY must be base64")
		}
		cfg.AccountKey = decoded
		accounts, err := server.OpenAccounts(env("ACCOUNTS_PATH", ".data/accounts.sqlite"))
		if err != nil {
			log.Fatalf("open accounts: %v", err)
		}
		defer accounts.Close()
		cfg.Accounts = accounts
	}

	db, err := server.OpenCatalog(env("DATABASE_PATH", ".data/catalog.sqlite"))
	if err != nil {
		log.Fatalf("open catalog: %v", err)
	}
	defer db.Close()
	if os.Getenv("LOGGER_ENABLED") == "true" {
		writer, err := server.OpenCatalogWriter(env("DATABASE_PATH", ".data/catalog.sqlite"))
		if err != nil {
			log.Fatal("could not open the logger catalog writer")
		}
		defer writer.Close()
		cfg.Logger = &server.LoggerConfig{Catalog: writer, Bot: botConfig()}
	}

	srv, err := server.New(cfg, db, web.Dist())
	if err != nil {
		log.Fatal(err)
	}
	ln, err := listen(addr, cfg.TunnelOnly)
	if err != nil {
		log.Fatal(err)
	}
	httpServer := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdown)
	}()

	login := "disabled (set SESSION_KEY and VRC_CONTACT)"
	if cfg.SessionKey != nil && cfg.Contact != "" {
		login = "enabled"
	}
	if cfg.Accounts == nil {
		login += "; VRCove accounts disabled (set ACCOUNT_KEY)"
	} else {
		login += "; VRCove accounts enabled"
	}
	mode := "direct access allowed"
	if cfg.TunnelOnly {
		mode = "tunnel-only"
	}
	log.Printf("VRCove listening on %s for %s (%s); account login %s", addr, cfg.PublicOrigin, mode, login)
	if err := httpServer.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func botConfig() server.BotConfig {
	bot := server.BotConfig{Username: os.Getenv("BOT_USERNAME"), Password: os.Getenv("BOT_PASSWORD")}
	if bot.Username == "" || bot.Password == "" {
		log.Fatal("the logger needs BOT_USERNAME and BOT_PASSWORD for its VRChat bot account")
	}
	return bot
}

// botLogin runs vrcove bot-login with the server's own environment.
func botLogin() {
	key, err := base64.StdEncoding.DecodeString(os.Getenv("ACCOUNT_KEY"))
	if err != nil || len(key) == 0 {
		log.Fatal("bot-login stores the bot session with VRCove accounts: set ACCOUNT_KEY")
	}
	accounts, err := server.OpenAccounts(env("ACCOUNTS_PATH", ".data/accounts.sqlite"))
	if err != nil {
		log.Fatal(err)
	}
	defer accounts.Close()
	srv, err := server.New(server.Config{
		PublicOrigin: env("PUBLIC_ORIGIN", "http://localhost:5173"),
		Contact:      os.Getenv("VRC_CONTACT"),
		Accounts:     accounts,
		AccountKey:   key,
	}, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	bot := botConfig()
	input := bufio.NewReader(os.Stdin)
	name, err := srv.BotLogin(context.Background(), bot.Username, bot.Password, func(method string) (string, error) {
		if method == "emailOtp" {
			fmt.Print("VRChat emailed a code to the bot account. Code: ")
		} else {
			fmt.Print("Code from the bot account's authenticator app: ")
		}
		line, err := input.ReadString('\n')
		return strings.TrimSpace(line), err
	})
	if err != nil {
		log.Fatalf("bot sign-in failed: %v", err)
	}
	fmt.Printf("Signed in as %s. The server uses this session for logger checks.\n", name)
}
