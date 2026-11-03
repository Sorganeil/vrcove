# VRChat Go client

Use this Go module to call the VRChat API and read its live WebSocket pipeline.
It handles JSON requests, login, two-factor verification, session cookie rotation,
HTTP errors, and Retry-After parsing. It has no dependency on VRCove's server,
database, frontend, or desktop app.

Module path: `github.com/Sorganeil/vrcove/vrchat` (Go 1.26+).

## Local use

Until this module has a release, another project can use a local checkout:

```sh
go mod edit -require=github.com/Sorganeil/vrcove/vrchat@v0.0.0
go mod edit -replace=github.com/Sorganeil/vrcove/vrchat=/absolute/path/to/vrcove/vrchat
go mod tidy
```

The VRCove server and companion use local replacements. Versioned releases use
`vrchat/vX.Y.Z` Git tags, independently of the application release tags.

## Requests and authentication

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/Sorganeil/vrcove/vrchat"
)

func main() {
    client, err := vrchat.New(vrchat.Config{
        UserAgent: "MyApp/1.0 (operator@example.com)",
    })
    if err != nil {
        log.Fatal(err)
    }
    ctx := context.Background()
    user, tokens, err := client.Login(ctx, os.Getenv("VRCHAT_USERNAME"), os.Getenv("VRCHAT_PASSWORD"))
    if err != nil {
        log.Fatal(err)
    }
    challenge, _ := user.(map[string]any)
    if challenge["requiresTwoFactorAuth"] != nil {
        // Obtain the required method and code from the user, then call
        // VerifyTwoFactor(ctx, tokens, "totp" / "otp" / "emailotp", code).
        // Apply its returned tokens and call CurrentUser before proceeding.
        fmt.Println("Two-factor verification required")
        return
    }
    friends, updates, err := client.Do(ctx, "/auth/user/friends?n=50", tokens, vrchat.Request{})
    tokens.Apply(updates)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(friends)
}
```

`Login` returns either the current user or a `requiresTwoFactorAuth` challenge.
`VerifyTwoFactor` accepts `totp`, `otp` (recovery code), and `emailotp`. VRChat's
challenge calls the email method `emailOtp`; pass `emailotp` to verification.
`CurrentUser` retrieves the authenticated user and `Logout` ends the session.
`BasicAuth` handles VRChat's credential encoding if you use `Do` for login.

`Do` accepts any API path beginning with a single `/`, an optional HTTP method,
JSON body, and headers. Decoded responses use `map[string]any`, `[]any`, and
`json.Number` so large numeric values keep their precision.
Build query strings with `net/url.Values`.

Tokens belong to the caller. Apply returned rotations after each call and store
them according to your application's needs. The client keeps credentials in
memory for each call. It copies a supplied HTTP client, disables redirects and
its cookie jar, and leaves retries to the caller.
It uses a 20-second API timeout and a 20 MB response limit by default; both are
configurable. `BaseURL`, `PipelineURL`, and `HTTPClient` can be overridden for tests.

## Errors and cooldowns

```go
var apiErr *vrchat.APIError
if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests {
    wait := apiErr.RetryDelay(time.Now(), 10*time.Minute)
    // Pause this account's requests for wait before making another call.
    _ = wait
}
```

HTTP errors expose the status and Retry-After header, without upstream response
bodies. `errors.Is(err, vrchat.ErrResponseTooLarge)` identifies oversized JSON.
Transport and decoding errors are returned to the caller; avoid logging request
headers, credentials, or user data. Cooldowns and session invalidation remain the
application's responsibility.

## Live events

```go
pipe, err := client.DialPipeline(ctx, tokens.Auth)
if err != nil {
    return err
}
defer pipe.Close()
message, err := pipe.Read(ctx)
```

`Read` returns raw event envelopes, limited to 1 MiB per message. The caller owns
event interpretation, connection deadlines, and reconnection. Pipeline dial
errors omit the URL containing the auth token.

## Development

Run from this directory:

```sh
go test -race ./...
go vet ./...
```

The tests use stub transports and a local WebSocket server; they never contact
VRChat. The parent module's `go test ./...` does not traverse this module.
