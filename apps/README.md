# Native apps

One folder per platform, each a self-contained project with its own build. Every app drives the same Go core
(`cli/mobile`, the protocol the CLI uses) and draws its own native UI; no UI code is shared between the native platforms (the desktop app reuses the web app's React components).

| Folder | Platform | UI | Go core via |
|---|---|---|---|
| [android/](android/README.md) | Android 10+ | Kotlin, Jetpack Compose, Material 3 Expressive | gomobile `.aar` |
| [desktop/](desktop/README.md) | Linux, Windows | [Wails 3](https://v3.wails.io), React; shares the web app's components | plain Go import of `cli/mobile` |
