using System;
using System;
using System.Collections.Generic;
using Newtonsoft.Json;
using UnityEngine;

namespace Sao.UnityClient
{
    public sealed class PixelWorldRuntime : MonoBehaviour
    {
        private const float TileSize = 1f;
        private readonly Dictionary<string, SpriteRenderer> tiles = new Dictionary<string, SpriteRenderer>();
        private readonly Dictionary<string, GameObject> entities = new Dictionary<string, GameObject>();
        private readonly Dictionary<string, string> entityAssetSignatures = new Dictionary<string, string>();
        private PixelWorldApi api;
        private PixelWorldSnapshot world;
        private Transform mapLayer;
        private Transform tileOverlayLayer;
        private Transform entityLayer;
        private Transform actorLayer;
        private SpriteRenderer generatedMap;
        private GameObject player;
        private Camera worldCamera;
        private bool requestInFlight;
        private bool staffTargeting;
        private bool mapLoading;
        private string renderedTileSignature = "";

        private void Awake()
        {
            api = GetComponent<PixelWorldApi>();
            CreateCamera();
            CreateLayers();
        }

        private void Start()
        {
            RefreshWorld();
        }

        private void Update()
        {
            if (world == null || requestInFlight) return;
            if (Input.GetKeyDown(KeyCode.Alpha1)) staffTargeting = !staffTargeting;
            if (Input.GetKeyDown(KeyCode.E)) InteractNearest();
            if (Input.GetMouseButtonDown(0)) HandleClick();

            var direction = Vector2Int.zero;
            if (Input.GetKeyDown(KeyCode.W) || Input.GetKeyDown(KeyCode.UpArrow)) direction = Vector2Int.up;
            if (Input.GetKeyDown(KeyCode.S) || Input.GetKeyDown(KeyCode.DownArrow)) direction = Vector2Int.down;
            if (Input.GetKeyDown(KeyCode.A) || Input.GetKeyDown(KeyCode.LeftArrow)) direction = Vector2Int.left;
            if (Input.GetKeyDown(KeyCode.D) || Input.GetKeyDown(KeyCode.RightArrow)) direction = Vector2Int.right;
            if (direction != Vector2Int.zero) Move(direction);
        }

        private void RefreshWorld()
        {
            requestInFlight = true;
            api.GetWorld(SyncWorld, ReportFailure);
        }

        private void Move(Vector2Int direction)
        {
            var playerState = world.players["p1"];
            SendWorldAction(done => api.Move(new GridPosition(playerState.x + direction.x, playerState.y + direction.y), done, ReportFailure));
        }

        private void HandleClick()
        {
            var point = worldCamera.ScreenToWorldPoint(Input.mousePosition);
            var target = new GridPosition(Mathf.FloorToInt(point.x / TileSize), Mathf.FloorToInt(point.y / TileSize));
            if (staffTargeting)
            {
                SendWorldAction(done => api.UseItem("staff-rift", target, done, ReportFailure));
                staffTargeting = false;
                return;
            }
            var playerState = world.players["p1"];
            var direction = new Vector2Int(target.x - playerState.x, target.y - playerState.y);
            if (Mathf.Abs(direction.x) + Mathf.Abs(direction.y) == 1) Move(direction);
        }

        private void InteractNearest()
        {
            var playerState = world.players["p1"];
            foreach (var entity in world.entities)
            {
                if (!entity.IsDestroyed && Math.Abs(entity.position.x - playerState.x) + Math.Abs(entity.position.y - playerState.y) <= 1)
                {
                    SendWorldAction(done => api.Interact(entity.id, done, ReportFailure));
                    return;
                }
            }
        }

        private void SendWorldAction(Action<Action<PixelWorldSnapshot>> send)
        {
            requestInFlight = true;
            send(SyncWorld);
        }

        private void SyncWorld(PixelWorldSnapshot next)
        {
            world = next;
            requestInFlight = false;
            SyncTiles();
            SyncEntities();
            SyncPlayer();
            RefreshGeneratedMap();
        }

        private void SyncTiles()
        {
            for (var y = 0; y < world.size.y; y++)
            for (var x = 0; x < world.size.x; x++)
            {
                var key = x + "," + y;
                var kind = TileKind(x, y);
                if (!tiles.TryGetValue(key, out var renderer))
                {
                    renderer = CreateBlock("Changed Tile " + key, new Vector2(x + .5f, y + .5f), -10, new Vector2(1, 1), tileOverlayLayer);
                    tiles[key] = renderer;
                }
                renderer.color = TileColor(kind);
                renderer.enabled = generatedMap == null || kind.StartsWith("rubble");
                if (kind.StartsWith("rubble")) renderer.color = new Color(.12f, .10f, .12f, .38f);
            }
        }

        private void SyncEntities()
        {
            var live = new HashSet<string>();
            foreach (var entity in world.entities)
            {
                if (entity.IsDestroyed) continue;
                live.Add(entity.id);
                if (!entities.TryGetValue(entity.id, out var view))
                {
                    view = new GameObject("Entity " + entity.id);
                    view.transform.SetParent(entityLayer, false);
                    entities[entity.id] = view;
                    CreateBlock("Shadow", Vector2.zero, -1, new Vector2(.7f, .18f), view.transform).color = new Color(0f, 0f, 0f, .35f);
                    CreateBlock("Body", Vector2.zero, 1, EntityScale(entity.kind), view.transform);
                }
                view.transform.position = new Vector3(entity.position.x + .5f, entity.position.y + .5f, 0);
                view.transform.Find("Body").GetComponent<SpriteRenderer>().color = EntityColor(entity);
                RequestEntityAsset(entity, view.transform.Find("Body").GetComponent<SpriteRenderer>());
            }
            foreach (var pair in new List<KeyValuePair<string, GameObject>>(entities))
            {
                if (live.Contains(pair.Key)) continue;
                Destroy(pair.Value);
                entities.Remove(pair.Key);
                entityAssetSignatures.Remove(pair.Key);
            }
        }

        private void SyncPlayer()
        {
            var data = world.players["p1"];
            if (player == null)
            {
                player = new GameObject("Player p1");
                player.transform.SetParent(actorLayer, false);
                CreateBlock("Body", Vector2.zero, 5, new Vector2(.52f, .78f), player.transform).color = new Color(.94f, .78f, .43f);
                CreateBlock("Staff", new Vector2(.3f, .05f), 6, new Vector2(.1f, .72f), player.transform).color = new Color(.49f, .23f, .72f);
            }
            player.transform.position = new Vector3(data.x + .5f, data.y + .5f, 0);
            worldCamera.transform.position = new Vector3(player.transform.position.x, player.transform.position.y, -10);
        }

        private string TileKind(int x, int y)
        {
            var key = x + "," + y;
            if (world.changed_tiles != null && world.changed_tiles.TryGetValue(key, out var changed)) return changed;
            if ((x >= 11 && x <= 12) || (x >= 19 && x <= 20 && y > 11)) return "water";
            if ((x < 2 || x > 27) && y % 3 != 1) return "tree";
            if ((y < 2 || y > 18) && x % 4 != 2) return "tree";
            if ((x == 14 || x == 15) && y > 2 && y < 18) return "path";
            if (y == 10 && x > 4 && x < 25) return "path";
            if (x > 18 && y > 3 && y < 8) return "path";
            if (x < 10 && y > 13) return "path";
            return "grass";
        }

        private static Color TileColor(string kind)
        {
            if (kind.StartsWith("rubble")) return new Color(.25f, .23f, .25f);
            switch (kind) { case "water": return new Color(.14f, .45f, .62f); case "tree": return new Color(.12f, .31f, .16f); case "path": return new Color(.55f, .43f, .27f); default: return new Color(.28f, .51f, .25f); }
        }

        private static Color EntityColor(PixelEntity entity)
        {
            if (entity.kind == "chest") return entity.StateIs("opened", true) ? new Color(.76f, .63f, .34f) : new Color(.65f, .38f, .15f);
            if (entity.kind == "gate") return new Color(.43f, .44f, .48f);
            if (entity.kind == "npc") return new Color(.86f, .41f, .45f);
            return entity.StateIs("active", true) ? new Color(.28f, .9f, .95f) : new Color(.2f, .38f, .42f);
        }

        private static Vector2 EntityScale(string kind)
        {
            if (kind == "gate") return new Vector2(.94f, .94f);
            if (kind == "npc") return new Vector2(.48f, .72f);
            return new Vector2(.66f, .66f);
        }

        private void RequestEntityAsset(PixelEntity entity, SpriteRenderer renderer)
        {
            var signature = entity.kind + ":" + JsonConvert.SerializeObject(entity.state);
            if (entityAssetSignatures.TryGetValue(entity.id, out var existing) && existing == signature) return;
            entityAssetSignatures[entity.id] = signature;
            api.GetObjectTexture(entity.id, (texture, generating) =>
            {
                if (!entityAssetSignatures.TryGetValue(entity.id, out var current) || current != signature || renderer == null) return;
                ApplyEntityTexture(renderer, texture);
                if (generating) StartCoroutine(RefreshEntityAsset(entity.id, signature));
            }, error => Debug.LogWarning("Object asset unavailable for " + entity.id + ": " + error));
        }

        private IEnumerator RefreshEntityAsset(string entityId, string signature)
        {
            yield return new WaitForSeconds(2f);
            if (!entityAssetSignatures.TryGetValue(entityId, out var current) || current != signature) yield break;
            if (!entities.TryGetValue(entityId, out var view)) yield break;
            var renderer = view.transform.Find("Body").GetComponent<SpriteRenderer>();
            entityAssetSignatures.Remove(entityId);
            foreach (var entity in world.entities)
            {
                if (entity.id == entityId && !entity.IsDestroyed) RequestEntityAsset(entity, renderer);
            }
        }

        private static void ApplyEntityTexture(SpriteRenderer renderer, Texture2D texture)
        {
            if (renderer.sprite != null && renderer.sprite != RuntimeSprite.Shared) Destroy(renderer.sprite);
            var pixelsPerUnit = Mathf.Max(texture.width, texture.height);
            renderer.sprite = Sprite.Create(texture, new Rect(0, 0, texture.width, texture.height), new Vector2(.5f, .5f), pixelsPerUnit);
            renderer.color = Color.white;
        }

        private void CreateCamera()
        {
            worldCamera = Camera.main;
            if (worldCamera == null)
            {
                var cameraObject = new GameObject("Pixel Camera");
                worldCamera = cameraObject.AddComponent<Camera>();
                cameraObject.tag = "MainCamera";
            }
            worldCamera.orthographic = true;
            worldCamera.orthographicSize = 8.5f;
            worldCamera.backgroundColor = new Color(.04f, .10f, .08f);
            worldCamera.clearFlags = CameraClearFlags.SolidColor;
        }

        private void CreateLayers()
        {
            mapLayer = new GameObject("Map Layer").transform;
            tileOverlayLayer = new GameObject("Changed Tile Overlay").transform;
            entityLayer = new GameObject("Entity Layer").transform;
            actorLayer = new GameObject("Actor Layer").transform;
        }

        private void RefreshGeneratedMap()
        {
            var signature = ChangedTileSignature();
            if (mapLoading || signature == renderedTileSignature) return;
            mapLoading = true;
            api.GetMapTexture(world.revision, texture =>
            {
                mapLoading = false;
                renderedTileSignature = signature;
                ApplyMapTexture(texture);
                SyncTiles();
            }, error =>
            {
                mapLoading = false;
                Debug.LogWarning("Generated map texture unavailable; retaining tile fallback: " + error);
            });
        }

        private string ChangedTileSignature()
        {
            if (world.changed_tiles == null || world.changed_tiles.Count == 0) return "empty";
            var keys = new List<string>(world.changed_tiles.Keys);
            keys.Sort(StringComparer.Ordinal);
            return string.Join("|", keys);
        }

        private void ApplyMapTexture(Texture2D texture)
        {
            if (generatedMap == null)
            {
                var mapObject = new GameObject("Generated Map");
                mapObject.transform.SetParent(mapLayer, false);
                generatedMap = mapObject.AddComponent<SpriteRenderer>();
                generatedMap.sortingOrder = -100;
                generatedMap.flipY = true;
            }
            if (generatedMap.sprite != null) Destroy(generatedMap.sprite);
            var pixelsPerUnit = texture.width / (float)world.size.x;
            generatedMap.sprite = Sprite.Create(texture, new Rect(0, 0, texture.width, texture.height), new Vector2(.5f, .5f), pixelsPerUnit);
            generatedMap.transform.localPosition = new Vector3(world.size.x * .5f, world.size.y * .5f, 0);
        }

        private static SpriteRenderer CreateBlock(string name, Vector2 position, int order, Vector2 scale, Transform parent = null)
        {
            var block = new GameObject(name);
            if (parent != null) block.transform.SetParent(parent, false);
            block.transform.localPosition = position;
            var renderer = block.AddComponent<SpriteRenderer>();
            renderer.sprite = RuntimeSprite.Shared;
            renderer.sortingOrder = order;
            block.transform.localScale = scale;
            return renderer;
        }

        private void ReportFailure(string error)
        {
            requestInFlight = false;
            Debug.LogWarning("Pixel world request failed: " + error);
        }
    }

    internal static class RuntimeSprite
    {
        private static Sprite shared;
        public static Sprite Shared
        {
            get
            {
                if (shared != null) return shared;
                var texture = new Texture2D(1, 1, TextureFormat.RGBA32, false);
                texture.SetPixel(0, 0, Color.white);
                texture.filterMode = FilterMode.Point;
                texture.Apply();
                shared = Sprite.Create(texture, new Rect(0, 0, 1, 1), new Vector2(.5f, .5f), 1f);
                return shared;
            }
        }
    }
}
