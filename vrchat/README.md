# VRChat Go SDK

Typed Go methods for the VRChat API. The SDK covers 327 JSON operations in 19
categories, plus file and image downloads. VRCove's server and desktop app make
all of their VRChat requests through it.

## Install

```sh
go get github.com/Sorganeil/vrcove/vrchat@main
```

Requires Go 1.26 or later. The module doesn't depend on the rest of VRCove. SDK
releases are tagged `vrchat/vX.Y.Z`, separately from app releases.

## Sign in and make requests

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

If VRChat asks for verification, call `VerifyTwoFactor(ctx, tokens, method, code)`
with `totp`, `otp` (a recovery code) or `emailOtp`. Apply the returned tokens,
check `result.Verified`, then call `CurrentUser`. `Logout` ends the session, after
which you should delete any saved tokens.

Every method returns its result, any rotated tokens and an error. Apply the
tokens after every call, even when decoding fails. The client doesn't store
credentials, use a cookie jar or follow redirects. Persistence, retries and
cooldowns are up to your application.

## Coverage

[coverage.json](coverage.json) lists every method with its HTTP verb and path,
along with the exclusions. It includes users, profiles, friends, avatars, worlds,
instances, favorites, groups, notifications, invites, moderation, file metadata,
inventory, economy, prints, props, jams and configuration.

The SDK skips 11 operations: image uploads, print uploads and edits, invites
with photos, the CSS and JavaScript endpoints, and calendar file exports.

Endpoints and models are generated from a pinned commit of the
[community VRChat specification](https://github.com/vrchatapi/specification/tree/3656fce60438134a94f9cb9fff98608aacb250c9).
VRChat doesn't publish or guarantee that specification. Tests run against local
fixtures, not live accounts.

## Optional fields and pagination

Parameters and patch fields are pointers, so leaving a field out is different
from sending `false`, `0` or an empty string. Wrap values with `vrchat.Ptr`. To
clear a list, send a pointer to an empty slice:

```go
profile, updates, err := client.UpdateProfile(ctx, tokens, userID, &vrchat.UpdateProfileRequest{
    Bio:      vrchat.Ptr(""),
    BioLinks: vrchat.Ptr([]string{}),
})
tokens.Apply(updates)
```

List methods take the `N`, `Offset` or cursor options their endpoint accepts.
The SDK never fetches more pages on its own. It checks required query
parameters and path values before sending. VRChat enforces everything else.

Avatar search can return a plain list or a result with metadata. Either way the
avatars end up in `SearchAvatarsResult.Avatars`, and the metadata form also sets
`TotalCount` and `HasNext`. Values whose shape varies stay as `json.RawMessage`.

## Raw JSON

Each method has a matching operation constructor. Pass it to `Client.Call` to
get the whole response, including fields the models don't cover yet:

```go
record, updates, err := client.Call(ctx, tokens, vrchat.GetAvatar(avatarID))
tokens.Apply(updates)
```

`Call` decodes into `map[string]any`, `[]any` and `json.Number`, and VRCove
uses it before applying its privacy filters. `Execute` returns the typed result
for an operation. `Do` sends requests to JSON paths that aren't in the
specification yet.

## Errors and limits

A failed HTTP request returns `*vrchat.APIError` with `StatusCode` and
`RetryAfter`. `apiErr.RetryDelay(time.Now(), 10*time.Minute)` turns the latter
into a wait time. Errors never include VRChat's response body. Be careful not to
log credentials, headers or user data when you handle them.

Requests time out after 20 seconds and responses are capped at 20 MB. Larger
responses fail with `vrchat.ErrResponseTooLarge`. `Config` can change
`Timeout`, `MaxResponseBytes`, `HTTPClient`, `BaseURL` and `PipelineURL`. The
SDK copies an `HTTPClient` you pass before it changes the redirect and cookie
settings.

## Files and images

`DownloadFileVersion(ctx, tokens, fileID, versionID, options)` downloads a file
version. `FetchImage(ctx, tokens, url, options)` loads an image URL that came
from VRChat, such as an API file path or a signed CDN link. Both return an
`AssetResponse` with the status, content type and body. `DownloadOptions` sets
the timeout and size limit, and zero values fall back to the client's settings.

The SDK sends cookies only to the configured API origin, and `client.IsAPIURL(u)`
tells you whether a URL is on it. Redirects come back in `RedirectURL` without
being followed, so your application decides whether to load the next host.
Errors are `ErrInvalidAssetURL`, `ErrInvalidRedirect`, `ErrResponseTooLarge` or
`*vrchat.APIError`. Check the content type before serving the bytes.

## Live events

```go
pipe, err := client.DialPipeline(ctx, tokens.Auth)
if err != nil {
    return err
}
defer pipe.Close()
message, err := pipe.Read(ctx)
```

`Read` returns one raw message of up to 1 MiB. Deadlines and reconnecting are up
to you. Dial errors leave out the URL, because it contains the auth token.

`DecodePipelineMessage` parses a message. VRChat sometimes sends `content` and
notification `details` as JSON inside a string, and `DecodeContent` and
`DecodePipelineContent` handle both forms. `NotificationEvent` and
`FriendEvent` have the fields VRCove uses. For other events, decode into your
own struct.

## Tests and regeneration

Run these from this directory. The parent module's `go test ./...` skips it.

```sh
go test -race ./...
go vet ./...
```

The generated methods and models are committed. To regenerate them from the
pinned specification:

```sh
python3 -m venv /tmp/vrchat-sdk-tools
/tmp/vrchat-sdk-tools/bin/pip install -r tools/requirements.txt
git clone https://github.com/vrchatapi/specification.git /tmp/vrchat-spec
git -C /tmp/vrchat-spec checkout 3656fce60438134a94f9cb9fff98608aacb250c9
/tmp/vrchat-sdk-tools/bin/python tools/generate.py --spec-dir /tmp/vrchat-spec
/tmp/vrchat-sdk-tools/bin/python -m unittest discover -s tools -p test_generate.py
```

The generator refuses any other revision and any edited specification file. The
specification's license is in [LICENSE.specification](LICENSE.specification).
