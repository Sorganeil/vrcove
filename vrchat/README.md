# VRChat Go SDK

Call VRChat through named Go methods with request models, response models, and
query options. The SDK covers 327 JSON operations across 19 API categories.
VRCove uses the same operations for its web server and desktop app.

## Install

```sh
go get github.com/Sorganeil/vrcove/vrchat@main
```

Requires Go 1.26+. The module has no dependency on VRCove's server, database, or
frontend. Applications in this repository use local module replacements.
Versioned SDK releases use `vrchat/vX.Y.Z` tags independently of app releases.

## Authentication and requests

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
    if user.RequiresTwoFactorAuth != nil {
        fmt.Println("Verification methods:", *user.RequiresTwoFactorAuth)
        return // Ask the user for a code and complete verification first.
    }

    worlds, updates, err := client.SearchWorlds(ctx, tokens, vrchat.SearchWorldsParams{
        Search:   vrchat.Ptr("cafe"),
        Platform: vrchat.Ptr("android"),
        N:        vrchat.Ptr(50),
        Offset:   vrchat.Ptr(0),
    })
    tokens.Apply(updates)
    if err != nil {
        log.Fatal(err)
    }
    for _, world := range worlds {
        fmt.Println(world.ID, world.Name)
    }
}
```

`Login` returns a typed `LoginResult`, the session tokens, and an error. When
verification is required, call `VerifyTwoFactor(ctx, tokens, method, code)`, apply
the returned tokens, check `result.Verified`, and call `CurrentUser`. Supported
methods are `totp`, `otp` (a recovery code), and `emailOtp` or `emailotp`.
`Logout` ends the VRChat session; discard your saved tokens afterwards.

Every endpoint method returns its typed result, rotated tokens, and an error.
Keep the tokens and apply rotations after each call, including decoding errors.
The client keeps credentials in memory for each request, disables cookie jars
and redirects, and leaves persistence, retries, and cooldowns to your application.

## Endpoint coverage

The complete list of methods, HTTP verbs, paths, and exclusions is in
[coverage.json](coverage.json). Common operations include:

- Users and profiles: `GetUser`, `SearchUsers`, `GetPublicProfile`, `UpdateUser`,
  `UpdateProfile`, user notes, mutuals, groups, account settings, and verification.
- Friends: `GetFriends`, `GetFriendStatus`, `Friend`, `DeleteFriendRequest`,
  `Unfriend`, and `Boop`.
- Avatars: search, favorites, upload metadata, create/update/delete, selection,
  fallback selection, and impostor operations.
- Worlds and instances: search, favorites, recent/active worlds, publishing,
  instance creation, discovery, details, close, and short links.
- Favorites: list/add/remove, favorite groups, group contents, limits, updates,
  and clearing groups.
- Groups: membership, invites, requests, roles, permissions, bans, galleries,
  posts, announcements, audit logs, ownership transfer, and calendars.
- Notifications and invites: both notification APIs, accepting/reading/hiding,
  sending/responding to invites, self-invites, and invite message slots.
- Player moderation, file metadata and upload negotiation, inventory, economy,
  prints metadata, props, jams, configuration, and other JSON endpoints.

The 12 operations with binary, multipart, text, or calendar-file bodies are
listed as excluded. The SDK does not currently transfer file bytes or photo
uploads. JSON file creation, versioning, upload URLs, completion, and status
operations are included.

The endpoint definitions and models come from the
[community VRChat specification](https://github.com/vrchatapi/specification/tree/3656fce60438134a94f9cb9fff98608aacb250c9).
This is a pinned community contract, not an official or live guarantee from
VRChat. Tests check that contract using local fixtures; they do not exercise
hundreds of live account actions.

## Optional fields and pagination

Parameter structs use pointers to distinguish an omitted value from explicit
`false`, `0`, or an empty string. Use `vrchat.Ptr(value)`. Patch models also use
pointers for optional arrays so an empty list can clear an existing value:

```go
profile, updates, err := client.UpdateProfile(ctx, tokens, userID, &vrchat.UpdateProfileRequest{
    Bio:      vrchat.Ptr(""),
    BioLinks: vrchat.Ptr([]string{}),
})
tokens.Apply(updates)
```

List methods expose the documented `N`, `Offset`, or cursor options for that
endpoint. Fetch subsequent pages explicitly so your application controls request
volume. Required query parameters and empty or unsafe path components are
checked locally. Other field constraints and permissions are enforced upstream.

Avatar search accepts either an avatar array or a metadata result. Both decode
into `SearchAvatarsResult.Avatars`; metadata responses also set `TotalCount` and
`HasNext`. Discriminated object responses expose a combined set of fields.
Heterogeneous values that cannot share a Go scalar or struct use
`json.RawMessage` to preserve their JSON.

## Complete response access

Each endpoint has a matching operation constructor. Use `Client.Call` when you
need the full JSON, including new fields that the current model does not know:

```go
record, updates, err := client.Call(ctx, tokens, vrchat.GetAvatar(avatarID))
tokens.Apply(updates)
```

`Call` uses `map[string]any`, `[]any`, and `json.Number`. VRCove uses this form
before applying its own privacy filters. `Execute(ctx, client, tokens, operation)`
returns the constructor's typed result. `Do` remains available for JSON paths
not yet present in the pinned specification.

## Errors and transport settings

HTTP failures return `*vrchat.APIError` with `StatusCode` and `RetryAfter`.
`apiErr.RetryDelay(time.Now(), 10*time.Minute)` parses the cooldown.
Upstream error bodies are excluded from errors. Transport and decoding errors
are returned to the caller; avoid logging credentials, headers, or user data.

The API request timeout defaults to 20 seconds and the response limit to 20 MB.
`Config.Timeout`, `MaxResponseBytes`, `HTTPClient`, `BaseURL`, and `PipelineURL`
can override these settings. Use `errors.Is(err, vrchat.ErrResponseTooLarge)` to
identify oversized responses. Supplied HTTP clients are copied before redirect
and cookie-jar settings are changed.

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
event interpretation, connection deadlines, and reconnection. Dial errors omit
the URL containing the auth token.

## Tests and regeneration

From this directory:

```sh
go test -race ./...
go vet ./...
```

Tests use stub transports and a local WebSocket server. The parent module's
`go test ./...` does not traverse this module.

Generated methods and models are checked in. To reproduce them, use the pinned
specification and the Python development dependencies:

```sh
python3 -m venv /tmp/vrchat-sdk-tools
/tmp/vrchat-sdk-tools/bin/pip install -r tools/requirements.txt
git clone https://github.com/vrchatapi/specification.git /tmp/vrchat-spec
git -C /tmp/vrchat-spec checkout 3656fce60438134a94f9cb9fff98608aacb250c9
/tmp/vrchat-sdk-tools/bin/python tools/generate.py --spec-dir /tmp/vrchat-spec
/tmp/vrchat-sdk-tools/bin/python -m unittest discover -s tools -p test_generate.py
```

The generator rejects another revision or modified specification files. Upstream
schema attribution is retained in [LICENSE.specification](LICENSE.specification).
