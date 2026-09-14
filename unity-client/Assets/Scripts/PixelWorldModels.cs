using System;
using System.Collections.Generic;
using Newtonsoft.Json.Linq;

namespace Sao.UnityClient
{
    [Serializable]
    public sealed class PixelWorldEnvelope
    {
        public PixelWorldSnapshot world;
    }

    [Serializable]
    public sealed class PixelActionEnvelope
    {
        public PixelWorldSnapshot world;
        public PixelWorldEvent @event;
    }

    [Serializable]
    public sealed class PixelWorldSnapshot
    {
        public int revision;
        public string scene_id;
        public GridPosition size;
        public Dictionary<string, PixelPlayer> players = new Dictionary<string, PixelPlayer>();
        public List<PixelEntity> entities = new List<PixelEntity>();
        public Dictionary<string, string> changed_tiles = new Dictionary<string, string>();
        public List<PixelWorldEvent> event_log = new List<PixelWorldEvent>();
    }

    [Serializable]
    public class GridPosition
    {
        public int x;
        public int y;

        public GridPosition() { }
        public GridPosition(int x, int y) { this.x = x; this.y = y; }
    }

    [Serializable]
    public sealed class PixelPlayer : GridPosition
    {
        public string id;
        public string name;
        public List<PixelItem> inventory = new List<PixelItem>();
    }

    [Serializable]
    public sealed class PixelItem
    {
        public string id;
        public string name;
        public string kind;
        public int quantity;
        public Dictionary<string, JToken> state = new Dictionary<string, JToken>();
    }

    [Serializable]
    public sealed class PixelEntity
    {
        public string id;
        public string kind;
        public string name;
        public GridPosition position;
        public Dictionary<string, JToken> state = new Dictionary<string, JToken>();

        public bool IsDestroyed => StateIs("destroyed", true);

        public bool StateIs(string key, bool expected)
        {
            return state != null && state.TryGetValue(key, out var value) && value.Value<bool>() == expected;
        }
    }

    [Serializable]
    public sealed class PixelWorldEvent
    {
        public string id;
        public string type;
        public string title;
        public string detail;
        public int revision;
    }
}
