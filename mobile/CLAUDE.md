# Mobile (Android app + KMP `shared`)

Work here as a senior Android engineer shipping to production. Before writing code, think through lifecycle, threading,
every API level from minSdk 26 up, configuration changes, process death, slow or no network, and what each screen
shows on its first frame. Fix root causes; never paper over a symptom with delays, retries or flags.

## Done means verified
- Walk every UI state through its first frame and each transition: what is on screen between "started" and "loaded"?
  An empty list that isn't flagged as loading renders the empty state, so set loading flags before the first
  suspension point.
- Bug fix: write the failing test first. ViewModels use `androidApp/src/test/.../ui/Fakes.kt`; repositories use
  `shared/src/commonTest`.
- Run `:shared:testAndroidHostTest`, `:androidApp:testStagingDebugUnitTest` and `:androidApp:lintStagingDebug`
  (commands in the root CLAUDE.md). Add no new lint warnings.
- Report what you couldn't verify (device or emulator, real Firebase, FCM, ads). Check a claim (docs, source, a
  test) before stating it.

## Compose
- Read fast-changing state (animations, scroll position, drag offsets) only inside draw or layout lambdas
  (`drawBehind`, `Canvas`, `graphicsLayer { }`, `offset { }`). Never read `.value` in composition, which recomposes
  every frame.
- A parent that needs one field of a big state uses `derivedStateOf` or a narrower flow. `Shell` and `MainScreen`
  recompose everything below them.
- No expensive work in composition: no parsing, no formatter or `Regex` creation, no sorting or filtering. Use
  `remember(keys)`, cache per locale, or precompute in the ViewModel or repository.
- Lazy lists: give every item a stable `key` and a `contentType`. UI models must be stable; shared-module models
  are listed in `androidApp/compose_stability.conf`.
- Text lives in `strings.xml`. Colours, spacing and shapes come from the theme (`ChangeloomTheme`, `Spacing`,
  `Radius`). Follow the insets rule in the root CLAUDE.md.

## Platform
- Every API above minSdk 26 needs a `Build.VERSION.SDK_INT` guard and defined behaviour below that level. A
  permission that doesn't exist on an older release, such as `POST_NOTIFICATIONS` (API 33), always checks as denied
  there. Use the compat API instead (`NotificationManagerCompat.areNotificationsEnabled()`).
- The theme is the user's choice (`ThemePreferences`), not the system's. Anything drawn outside Compose (splash,
  window background, system bars) must follow it too.
- State survives rotation and process death: `rememberSaveable` and `SavedStateHandle`. Account-scoped ViewModels
  live in the session store (`SessionViewModels`).
- Nothing blocks the main thread: disk goes through `Dispatchers.IO` (StrictMode logs violations in debug builds),
  network goes through `ChangeloomApi`.

## Data & coroutines
- Start independent requests together. Never gate a screen on data it doesn't need to decide what to show.
- `viewModelScope` work dies with the ViewModel. Work that must outlive a screen goes in a longer scope (a session
  ViewModel, the app scope, or `NonCancellable` for a final sync).
- Every state flag is reset on every path: success, error, and a known outcome on cancellation.
- Rethrow `CancellationException`. Catch specific failures and show an actionable message through `Strings`. Log
  with `AppLog`, never PII.
- Optimistic updates roll back on failure (see `TimelineRepository.setRead`).

## Hygiene
- Delete code and resources your change makes unused. No commented-out code, no dead flags.
- Resource folder renames can leave a stale incremental merge. If AAPT reports a missing resource that exists,
  rerun `:androidApp:merge<Variant>Resources --rerun`.
