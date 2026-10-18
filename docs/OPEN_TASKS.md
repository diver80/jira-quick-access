# Open tasks

Planned work that is deliberately deferred. Remove an entry when it ships.

## Waiting for an Apple Developer ID

### Code signing and notarization
Sign the app with a Developer ID certificate and notarize it in `./build.sh release`.
This removes the `xattr -dr com.apple.quarantine` step from the Homebrew cask caveats
and gives the app a stable identity across updates (needed for the Keychain task below).

### API tokens in the macOS Keychain
Store each instance's API token in the Keychain instead of `~/.jira-quick-access/config.json`
(service `com.avono.jira-quick-access`, account = instance ID) and migrate existing configs.
If the Keychain is unavailable or access is denied, keep the token in the file and show an error.

Why it waits: builds are only ad-hoc signed, so macOS treats every release as a different
app. With the default "this app only" access rule, each `brew upgrade` would prompt for the
login password once. The alternative ("any app may read") avoids the prompt but offers little
over the current `0600` file. With a Developer ID the access rule stays valid across updates.

## Review findings not yet fixed

- `IsMouseInside` reads the window frame off the main thread.
- The `NSWindow (AllowKeyWindow)` category overrides key handling for every window in the
  process; a subclass or a check for the HUD window would be safer.
- The window drag loop can stay active after very quick clicks.
- Multi-monitor: displays that are added or removed while running are not handled, and
  auto-hide can tuck onto the inner edge between two displays.
- `copyToClipboard` is called while the view lock is held.
- `SaveConfig` does not `fsync` before the atomic rename.
- `main.go` re-reads the config file on every status poll tick.
- `GOOS=windows go vet ./pkg/window` fails: `manager_test.go` uses the darwin-only `collapseMu`.

## Ideas

- Evaluate [mygo](https://github.com/egoist/mygo) as an alternative UI stack (spike).
- Comment notifications (needs one extra request per changed ticket).
- Configurable global hotkey (currently fixed to ⌥⌘J).
