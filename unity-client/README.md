# SAO Unity Client

Unity 6.5 (6000.5) 2D runtime client for the existing Go pixel-world API. It is independent from `game-client`; the Go backend and `data/pixel_world.json` remain the source of truth.

## Open

1. Install Unity Hub and Unity `6.5 (6000.5.6f1)` with the Windows Build Support module.
2. In Unity Hub, use **Add > Add project from disk** and select this `unity-client` directory.
3. Open `Assets/Scenes/Bootstrap.unity` and press Play. `PixelWorldBootstrap` creates the camera and world runtime automatically.
4. Run `run-pixel-server.cmd` from the repository root and keep that terminal open. It starts the local-only pixel API at `http://127.0.0.1:8080`; it does not load PostgreSQL or AI configuration.

## Controls

- `WASD` or arrow keys: move one tile.
- `E`: interact with an adjacent object.
- `1`: toggle staff targeting, then left click an adjacent tile to break it.

## Current Scope

The runtime loads the backend-generated map PNG as its base layer. Colored blocks remain only as a fallback while that image is unavailable and as a translucent changed-tile debug overlay. Entities and the player still use placeholder sprites while the approved asset manifest is being built.

Generated candidates are not loaded by the runtime. An approved asset is registered in `data/pixel_assets/asset-manifest.json` with:

```powershell
go run -buildvcs=false ./cmd/generate_pixel_asset -object chest-amber
go run -buildvcs=false ./cmd/pixel_assets -approve object/chest-amber -kind object -file candidates/chest-amber-v01.png
```

The next Unity pass will map approved manifest entries to `SpriteRenderer` and `Animator` prefabs. Assets must not be generated during gameplay.
