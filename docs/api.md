# API Reference

Base URL: `http://<host>:8080`

SSE endpoint:
- `GET /events?player_id=<id>`

All other endpoints are `POST` and return JSON.

For action endpoints (`/action`, `/nl-action`, `/interact`, `/observe`, `/pick-drop`):
- The server subscribes to player events, enqueues action, and waits up to 40s for the matched `action_resolved` event.
- Response includes synchronous `outcome` and `event`.
- Internal matching uses `payload.request_id` to pair request/response.
- If request includes `dice_expr`, outcome payload may include `roll` breakdown:
  - `base_total`
  - `final_total`
  - `attribute` / `attribute_score` / `attribute_modifier`
  - `inventory_modifier`
  - `reasonableness_modifier` / `reasonableness_reason`

## SSE) `/events`

Server-Sent Events stream for player-scoped scene/action updates.

Request:
```
GET /events?player_id=p1
Accept: text/event-stream
```

Event examples:
```text
event: connected
data: {"player_id":"p1"}

event: action_resolved
data: {"type":"action_resolved","scene_id":"scene-城镇","player_id":"p1","payload":{"ok":true}}
```

Notes:
- Keep connection open with `EventSource`.
- Heartbeat comment `: ping` is sent every 20s.

## 0) `/auth/login`

Login or create player in a scene (default `scene-城镇`).
`device_id` is required. One account can only stay bound to one device at a time.

Request:
```json
{
  "player_id": "p1",
  "device_id": "device-a",
  "name": "爱丽丝",
  "scene_id": "scene-城镇",
  "attributes": {
    "strength": 12,
    "dexterity": 11
  }
}
```

Response:
```json
{
  "status": "ok",
  "scene_id": "scene-城镇",
  "player": {
    "id": "p1",
    "name": "爱丽丝",
    "scene_id": "scene-城镇",
    "layer_stack": ["scene-城镇"],
    "inventory_obj_ids": [],
    "device_id": "device-a",
    "device_locked": true,
    "attributes": {
      "strength": 12,
      "dexterity": 11,
      "constitution": 10,
      "intelligence": 10,
      "wisdom": 10,
      "charisma": 10,
      "luck": 10
    }
  }
}
```

## 0.1) `/auth/logout`

Logout current device and release account lock.

Request:
```json
{
  "player_id": "p1",
  "device_id": "device-a"
}
```

Response:
```json
{
  "status": "ok",
  "player": {
    "id": "p1",
    "scene_id": "scene-城镇",
    "device_id": "",
    "device_locked": false
  }
}
```

## 0.2) `/auth/accounts`

Get available accounts (unlocked accounts only). Optional `scene_id` filter supported.

Request:
```json
{
  "scene_id": "scene-城镇"
}
```

Response:
```json
{
  "status": "ok",
  "accounts": [
    {
      "player_id": "user01",
      "name": "账号01",
      "scene_id": "scene-城镇",
      "device_locked": true,
      "is_available": true
    }
  ]
}
```

## 1) `/action`

Submit a fully specified action envelope.

Request:
```json
{
  "player_id": "p1",
  "action": {
    "type": "interact",
    "interaction": "open_container",
    "target_object_id": "obj-木箱-1",
    "target_query": "箱子",
    "layer_id": "scene-测试区",
    "dice_expr": "1d20",
    "patch": {},
    "payload": {"raw_input": "打开箱子"}
  }
}
```

Response:
```json
{
  "status": "ok",
  "action": {
    "type": "interact",
    "interaction": "open_container",
    "target_object_id": "obj-木箱-1",
    "target_query": "箱子",
    "layer_id": "scene-测试区",
    "dice_expr": "1d20",
    "patch": {},
    "payload": {
      "raw_input": "打开箱子",
      "request_id": "p1-1740210000000000000-1"
    }
  },
  "outcome": {
    "ok": true,
    "narration": "你打开了箱子，发现了一把测试钥匙。",
    "spawned": [],
    "request_id": "p1-1740210000000000000-1"
  },
  "event": {
    "type": "action_resolved",
    "scene_id": "scene-城镇",
    "player_id": "p1",
    "object_id": "obj-木箱-1",
    "payload": {
      "ok": true,
      "narration": "你打开了箱子，发现了一把测试钥匙。",
      "spawned": [],
      "request_id": "p1-1740210000000000000-1"
    }
  }
}
```

Failure example:
```json
{
  "status": "failed",
  "error": "target object obj-x not found",
  "action": {
    "type": "interact",
    "interaction": "open_container",
    "target_object_id": "obj-x"
  },
  "outcome": {
    "error": "target object obj-x not found",
    "request_id": "p1-1740210000000000000-2"
  },
  "event": {
    "type": "action_resolved",
    "scene_id": "scene-城镇",
    "player_id": "p1",
    "payload": {
      "error": "target object obj-x not found",
      "request_id": "p1-1740210000000000000-2"
    }
  }
}
```

## 2) `/nl-action`

Submit natural language and let the referee parse.

Request:
```json
{
  "player_id": "p1",
  "input": "打开箱子"
}
```

Response:
```json
{
  "status": "ok",
  "parsed_action": {
    "type": "interact",
    "interaction": "open_container",
    "target_query": "箱子",
    "dice_expr": "1d20",
    "payload": {
      "raw_input":"打开箱子",
      "request_id":"p1-1740210000000000000-3"
    }
  },
  "outcome": {
    "ok": true,
    "narration": "箱子已经被打开，里面空空如也。",
    "spawned": [],
    "request_id":"p1-1740210000000000000-3"
  },
  "event": {
    "type": "action_resolved",
    "scene_id": "scene-城镇",
    "player_id": "p1",
    "object_id": "obj-木箱-1",
    "payload": {
      "ok": true,
      "narration": "箱子已经被打开，里面空空如也。",
      "spawned": [],
      "request_id":"p1-1740210000000000000-3"
    }
  }
}
```

## 3) `/interact`

Direct interact call.

Request:
```json
{
  "player_id": "p1",
  "interaction": "open_container",
  "target_query": "箱子",
  "payload": {"raw_input": "打开箱子"}
}
```

Response:
```json
{
  "status": "ok",
  "action": {
    "type": "interact",
    "interaction": "open_container",
    "target_query": "箱子",
    "payload": {"request_id":"p1-1740210000000000000-4"}
  },
  "outcome": {
    "ok": true,
    "narration": "你打开了箱子。",
    "spawned": [],
    "request_id":"p1-1740210000000000000-4"
  },
  "event": {
    "type": "action_resolved",
    "scene_id": "scene-城镇",
    "player_id": "p1",
    "payload": {
      "ok": true,
      "narration": "你打开了箱子。",
      "spawned": [],
      "request_id":"p1-1740210000000000000-4"
    }
  }
}
```

## 4) `/observe`

Observe a target object or layer. If only `target_query` is given, server resolves object first; if not found, resolves layer.

Request:
```json
{
  "player_id": "p1",
  "target_query": "大厅",
  "payload": {"raw_input": "观察大厅"}
}
```

Response:
```json
{
  "status": "ok",
  "action": {
    "type": "observe",
    "target_query": "大厅",
    "payload": {"request_id":"p1-1740210000000000000-5"}
  },
  "outcome": {
    "ok": true,
    "narration": "你观察了大厅，四周安静。",
    "spawned": [],
    "request_id":"p1-1740210000000000000-5"
  },
  "event": {
    "type": "action_resolved",
    "scene_id": "scene-城镇",
    "player_id": "p1",
    "payload": {
      "ok": true,
      "narration": "你观察了大厅，四周安静。",
      "spawned": [],
      "request_id":"p1-1740210000000000000-5"
    }
  }
}
```

## 5) `/pick-drop`

Pick up or drop an item.

Request:
```json
{
  "player_id": "p1",
  "action": "pick",
  "target_query": "测试钥匙",
  "payload": {"raw_input": "捡起测试钥匙"}
}
```

Response:
```json
{
  "status": "ok",
  "action": {
    "type": "interact",
    "interaction": "pickup_item",
    "target_query": "测试钥匙",
    "payload": {"request_id":"p1-1740210000000000000-6"}
  },
  "outcome": {
    "ok": true,
    "narration": "你拾取了测试钥匙，已放入背包。",
    "request_id":"p1-1740210000000000000-6"
  },
  "event": {
    "type": "action_resolved",
    "scene_id": "scene-城镇",
    "player_id": "p1",
    "object_id": "obj-测试钥匙",
    "payload": {
      "ok": true,
      "narration": "你拾取了测试钥匙，已放入背包。",
      "request_id":"p1-1740210000000000000-6"
    }
  }
}
```

## 6) `/scene/layer`

Create a layer and attach to a parent.

Request:
```json
{
  "player_id": "p1",
  "layer_id": "scene-大厅",
  "relation": "child",
  "parent_layer_id": "scene-城镇"
}
```

Response:
```json
{
  "status": "ok",
  "layer_id": "scene-大厅",
  "outcome": {
    "ok": true,
    "created_layer_id": "scene-大厅",
    "relation": "child",
    "parent_layer_id": "scene-城镇",
    "requested_layer_id": "scene-大厅"
  }
}
```

## 7) `/scene/nearby`

Get nearby layers and their objects (current layer + parent + child layers).

Request:
```json
{
  "player_id": "p1"
}
```

Response:
```json
{
  "current_layer": "scene-测试区",
  "parent_layer": "scene-城镇",
  "child_layers": ["scene-测试区子层"],
  "layers": ["scene-城镇", "scene-测试区", "scene-测试区子层"],
  "layer_objects": {
    "scene-测试区": [
      {"id":"obj-测试钥匙","name":"测试钥匙","layer":"测试区","description":"地上有一把测试钥匙。"}
    ]
  }
}
```

## 8) `/player/panel/get`

Get player attribute panel and computed modifiers.

Request:
```json
{
  "player_id": "p1"
}
```

Response:
```json
{
  "status": "ok",
  "player_id": "p1",
  "attributes": {
    "strength": 12,
    "dexterity": 11,
    "constitution": 10,
    "intelligence": 14,
    "wisdom": 13,
    "charisma": 9,
    "luck": 10
  },
  "attr_modifiers": {
    "strength": 1,
    "dexterity": 0,
    "constitution": 0,
    "intelligence": 2,
    "wisdom": 1,
    "charisma": -1,
    "luck": 0
  }
}
```

## 9) `/player/panel/set`

Update player attribute panel (partial update supported).

Request:
```json
{
  "player_id": "p1",
  "attributes": {
    "strength": 14,
    "wisdom": 13,
    "luck": 12
  }
}
```

## 10) `/player/inventory/get`

Get items in player's inventory.

Request:
```json
{
  "player_id": "p1"
}
```

Response:
```json
{
  "status": "ok",
  "player_id": "p1",
  "items": [
    {
      "id": "obj-测试钥匙",
      "name": "测试钥匙",
      "layer": "背包",
      "tags": ["tool", "key"],
      "description": "地上有一把测试钥匙。",
      "state": {
        "layer": "背包",
        "description": "地上有一把测试钥匙。"
      }
    }
  ]
}
```

## Common Errors

- `400` invalid json / missing fields
- `405` method not allowed
- `504` action accepted but timed out waiting for matched outcome event (action endpoints only)

## Notes

- Action endpoints now return synchronous outcome/event payloads; `GET /events` can still be used for streaming updates.
- Matching uses database embeddings (pgvector) when enabled, otherwise falls back to local semantic matcher.




