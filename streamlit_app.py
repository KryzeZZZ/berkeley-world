import json
import queue
import threading
import time
import uuid
from collections import deque
from typing import Any

import requests
import streamlit as st


# The console only talks to the local Go API. Do not inherit ALL_PROXY or
# other system proxy settings for these loopback HTTP/SSE connections.
LOCAL_API_SESSION = requests.Session()
LOCAL_API_SESSION.trust_env = False


st.set_page_config(
    page_title="SAO TRPG Console",
    page_icon="🜂",
    layout="wide",
    initial_sidebar_state="expanded",
)


st.markdown(
    """
    <style>
    :root {
      --paper: #f5efe2;
      --ink: #1b1a17;
      --muted: #6a6257;
      --panel: rgba(255,255,255,0.72);
      --line: rgba(79, 64, 43, 0.18);
      --accent: #b85c38;
      --accent-2: #234e52;
      --glow: rgba(184, 92, 56, 0.18);
    }
    .stApp {
      background:
        radial-gradient(circle at top left, rgba(184, 92, 56, 0.12), transparent 28%),
        radial-gradient(circle at top right, rgba(35, 78, 82, 0.12), transparent 24%),
        linear-gradient(180deg, #f8f4ea 0%, #efe4ce 100%);
      color: var(--ink);
      font-family: "IBM Plex Sans", "Avenir Next", "Segoe UI", sans-serif;
    }
    h1, h2, h3 {
      font-family: "Iowan Old Style", "Palatino Linotype", "Book Antiqua", serif;
      letter-spacing: 0.02em;
    }
    [data-testid="stSidebar"] {
      background: linear-gradient(180deg, rgba(255,255,255,0.86), rgba(247,240,227,0.94));
      border-right: 1px solid var(--line);
    }
    [data-testid="stMetricValue"] {
      color: var(--accent-2);
    }
    div[data-baseweb="tab-list"] {
      gap: 0.5rem;
    }
    button[kind="secondary"], button[kind="primary"] {
      border-radius: 999px !important;
    }
    .sao-card {
      background: var(--panel);
      border: 1px solid var(--line);
      box-shadow: 0 16px 40px rgba(72, 52, 32, 0.08);
      border-radius: 20px;
      padding: 1rem 1.2rem;
      backdrop-filter: blur(8px);
    }
    .sao-caption {
      color: var(--muted);
      font-size: 0.92rem;
      margin-top: -0.2rem;
      margin-bottom: 0.6rem;
    }
    .sao-pill {
      display: inline-block;
      padding: 0.18rem 0.55rem;
      border-radius: 999px;
      border: 1px solid var(--line);
      margin-right: 0.35rem;
      margin-bottom: 0.35rem;
      background: rgba(255,255,255,0.58);
      font-size: 0.82rem;
    }
    .map-wrap {
      border: 1px solid rgba(44, 62, 80, 0.18);
      background: rgba(255,255,255,0.62);
      border-radius: 8px;
      padding: 16px;
      min-height: 520px;
    }
    .map-grid {
      display: grid;
      grid-template-columns: minmax(180px, 0.9fr) minmax(260px, 1.3fr) minmax(220px, 1fr);
      gap: 14px;
      align-items: stretch;
    }
    .map-lane {
      display: flex;
      flex-direction: column;
      gap: 12px;
    }
    .map-node {
      border: 1px solid rgba(35,78,82,0.22);
      background: rgba(255,255,255,0.78);
      border-radius: 8px;
      padding: 12px;
      min-height: 92px;
      box-shadow: 0 10px 24px rgba(35, 78, 82, 0.08);
    }
    .map-node.current {
      border-color: rgba(184,92,56,0.72);
      box-shadow: 0 0 0 2px rgba(184,92,56,0.15), 0 14px 28px rgba(184,92,56,0.12);
    }
    .map-node.parent {
      background: rgba(245, 239, 226, 0.9);
    }
    .map-node.child {
      background: rgba(236, 246, 242, 0.9);
    }
    .map-node.event {
      background: rgba(255, 246, 232, 0.92);
    }
    .map-node-title {
      font-weight: 700;
      font-size: 0.98rem;
      margin-bottom: 4px;
      word-break: break-word;
    }
    .map-node-meta {
      color: var(--muted);
      font-size: 0.78rem;
      line-height: 1.35;
      word-break: break-word;
    }
    .map-object-list {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
      gap: 8px;
      margin-top: 10px;
    }
    .map-object {
      border: 1px solid rgba(79, 64, 43, 0.16);
      border-radius: 8px;
      background: rgba(255,255,255,0.72);
      padding: 8px;
      font-size: 0.82rem;
      min-height: 62px;
    }
    .feedback-row {
      border-left: 3px solid rgba(35,78,82,0.55);
      background: rgba(255,255,255,0.68);
      padding: 0.7rem 0.85rem;
      border-radius: 0 8px 8px 0;
      margin-bottom: 0.5rem;
    }
    .feedback-row.error {
      border-left-color: rgba(184,92,56,0.85);
    }
    .feedback-title {
      font-weight: 700;
      margin-bottom: 0.16rem;
    }
    .feedback-meta {
      color: var(--muted);
      font-size: 0.78rem;
    }
    @media (max-width: 900px) {
      .map-grid {
        grid-template-columns: 1fr;
      }
    }
    </style>
    """,
    unsafe_allow_html=True,
)


ATTRIBUTES = [
    "strength",
    "dexterity",
    "constitution",
    "intelligence",
    "wisdom",
    "charisma",
    "luck",
]


STREAM_REGISTRY: dict[str, dict[str, Any]] = {}


def ensure_state() -> None:
    st.session_state.setdefault("base_url", "http://127.0.0.1:8080")
    st.session_state.setdefault("player_id", "p1")
    st.session_state.setdefault("device_id", f"streamlit-{uuid.uuid4().hex[:8]}")
    st.session_state.setdefault("scene_id", "scene-城镇")
    st.session_state.setdefault("player_name", "爱丽丝")
    st.session_state.setdefault("event_stream_key", uuid.uuid4().hex)
    st.session_state.setdefault("last_response", None)
    st.session_state.setdefault("last_error", None)
    st.session_state.setdefault(
        "attribute_editor",
        {key: 10 for key in ATTRIBUTES},
    )
    st.session_state.setdefault("live_auto_start", True)
    st.session_state.setdefault("live_auto_refresh", True)
    st.session_state.setdefault("live_refresh_ms", 2500)
    st.session_state.setdefault("map_snapshot", None)
    st.session_state.setdefault("map_last_refresh", None)
    st.session_state.setdefault("map_last_poll_ts", 0.0)
    st.session_state.setdefault("inventory_snapshot", None)
    st.session_state.setdefault("panel_snapshot", None)
    if "feedback_log" not in st.session_state:
        st.session_state.feedback_log = deque(maxlen=100)


def normalize_base_url(url: str) -> str:
    return url.rstrip("/")


def short_text(value: Any, limit: int = 220) -> str:
    if value is None:
        return ""
    if isinstance(value, (dict, list)):
        value = json.dumps(value, ensure_ascii=False)
    text = str(value).strip()
    if len(text) <= limit:
        return text
    return text[: limit - 1].rstrip() + "..."


def event_payload(data: Any) -> dict[str, Any]:
    if isinstance(data, dict):
        payload = data.get("payload")
        if isinstance(payload, dict):
            return payload
    return {}


def record_feedback(item: dict[str, Any]) -> None:
    payload = item.get("payload")
    request_id = ""
    if isinstance(payload, dict):
        request_id = str(payload.get("request_id") or "")
    key = json.dumps(
        {
            "source": item.get("source"),
            "path": item.get("path"),
            "title": item.get("title"),
            "scene_id": item.get("scene_id"),
            "layer_id": item.get("layer_id"),
            "request_id": request_id,
        },
        ensure_ascii=False,
        sort_keys=True,
    )
    recent = st.session_state.feedback_log
    for existing in list(recent)[:8]:
        if existing.get("_key") == key:
            return
    item["_key"] = key
    recent.appendleft(item)


def feedback_from_action_response(path: str, data: dict[str, Any]) -> None:
    outcome = data.get("outcome")
    event = data.get("event")
    if not isinstance(outcome, dict) and not isinstance(event, dict):
        return
    payload = outcome if isinstance(outcome, dict) else event_payload(event)
    status = str(data.get("status", "ok"))
    title = payload.get("narration") or payload.get("error") or data.get("error") or path
    scene_id = ""
    if isinstance(event, dict):
        scene_id = str(event.get("scene_id") or "")
    layer_id = payload.get("scene_layer") or payload.get("layer_id") or payload.get("current_layer")
    record_feedback(
        {
            "ts": time.strftime("%H:%M:%S"),
            "source": "api",
            "path": path,
            "status": status,
            "title": short_text(title, 160),
            "scene_id": scene_id,
            "layer_id": str(layer_id or ""),
            "payload": payload,
        }
    )


def feedback_from_stream_event(item: dict[str, Any]) -> None:
    data = item.get("data")
    if not isinstance(data, dict):
        return
    payload = event_payload(data)
    event_type = str(data.get("type") or item.get("event") or "event")
    title = payload.get("narration") or payload.get("error") or event_type
    record_feedback(
        {
            "ts": item.get("ts", time.strftime("%H:%M:%S")),
            "source": "sse",
            "path": event_type,
            "status": "failed" if payload.get("error") else "ok",
            "title": short_text(title, 160),
            "scene_id": str(data.get("scene_id") or ""),
            "layer_id": str(payload.get("scene_layer") or payload.get("layer_id") or ""),
            "payload": payload,
        }
    )


def api_post(
    path: str,
    payload: dict[str, Any],
    timeout: int = 45,
    remember: bool = True,
    record: bool = True,
) -> dict[str, Any]:
    url = f"{normalize_base_url(st.session_state.base_url)}{path}"
    response = LOCAL_API_SESSION.post(url, json=payload, timeout=timeout)
    data: dict[str, Any]
    try:
        data = response.json()
    except ValueError:
        data = {"status_code": response.status_code, "text": response.text}
    if response.ok:
        if remember:
            st.session_state.last_response = {"path": path, "payload": payload, "response": data}
        st.session_state.last_error = None
        if record:
            feedback_from_action_response(path, data)
        if path in {
            "/auth/login",
            "/action",
            "/nl-action",
            "/interact",
            "/observe",
            "/pick-drop",
            "/scene/layer",
        }:
            try:
                refresh_scene_snapshot(show_errors=False)
            except NameError:
                pass
        return data
    if remember:
        st.session_state.last_error = {"path": path, "payload": payload, "response": data}
    if record:
        record_feedback(
            {
                "ts": time.strftime("%H:%M:%S"),
                "source": "api",
                "path": path,
                "status": "failed",
                "title": short_text(data.get("error") or data),
                "scene_id": "",
                "layer_id": "",
                "payload": data,
            }
        )
    raise requests.HTTPError(
        f"{response.status_code} {response.reason}",
        response=response,
        request=response.request,
    )


def safe_json_text(raw: str) -> dict[str, Any]:
    raw = raw.strip()
    if not raw:
        return {}
    parsed = json.loads(raw)
    if not isinstance(parsed, dict):
        raise ValueError("JSON payload must be an object")
    return parsed


def stream_bucket() -> dict[str, Any]:
    key = st.session_state.event_stream_key
    bucket = STREAM_REGISTRY.get(key)
    if bucket is None:
        bucket = {
            "events": deque(maxlen=120),
            "errors": deque(maxlen=20),
            "queue": queue.Queue(),
            "thread": None,
            "stop_event": None,
            "active": False,
            "player_id": "",
            "base_url": "",
        }
        STREAM_REGISTRY[key] = bucket
    drain_stream_queue(bucket)
    return bucket


def drain_stream_queue(bucket: dict[str, Any]) -> None:
    event_queue: queue.Queue = bucket["queue"]
    while True:
        try:
            kind, payload = event_queue.get_nowait()
        except queue.Empty:
            break
        if kind == "event":
            bucket["events"].appendleft(payload)
            feedback_from_stream_event(payload)
        elif kind == "error":
            bucket["errors"].appendleft(payload)
            record_feedback(
                {
                    "ts": payload.get("ts", time.strftime("%H:%M:%S")),
                    "source": "sse",
                    "path": "listener",
                    "status": "failed",
                    "title": short_text(payload.get("error") or payload),
                    "scene_id": "",
                    "layer_id": "",
                    "payload": payload,
                }
            )
        elif kind == "closed":
            bucket["active"] = False


def stream_worker(base_url: str, player_id: str, sink: queue.Queue, stop_event: threading.Event) -> None:
    url = f"{normalize_base_url(base_url)}/events"
    try:
        with LOCAL_API_SESSION.get(url, params={"player_id": player_id}, stream=True, timeout=65) as resp:
            resp.raise_for_status()
            event_name = "message"
            data_lines: list[str] = []
            for raw_line in resp.iter_lines(decode_unicode=True):
                if stop_event.is_set():
                    break
                if raw_line is None:
                    continue
                line = raw_line.strip("\r")
                if line == "":
                    if data_lines:
                        data = "\n".join(data_lines)
                        try:
                            parsed = json.loads(data)
                        except json.JSONDecodeError:
                            parsed = {"raw": data}
                        sink.put(
                            (
                                "event",
                                {
                                    "ts": time.strftime("%H:%M:%S"),
                                    "event": event_name,
                                    "data": parsed,
                                },
                            )
                        )
                    event_name = "message"
                    data_lines = []
                    continue
                if line.startswith(":"):
                    continue
                if line.startswith("event:"):
                    event_name = line.split(":", 1)[1].strip() or "message"
                    continue
                if line.startswith("data:"):
                    data_lines.append(line.split(":", 1)[1].strip())
    except Exception as exc:
        sink.put(("error", {"ts": time.strftime("%H:%M:%S"), "error": str(exc)}))
    finally:
        sink.put(("closed", {"ts": time.strftime("%H:%M:%S")}))


def start_stream() -> None:
    bucket = stream_bucket()
    if (
        bucket["active"]
        and bucket.get("player_id") == st.session_state.player_id
        and bucket.get("base_url") == st.session_state.base_url
    ):
        return
    if bucket["active"]:
        stop_stream()
    stop_event = threading.Event()
    thread = threading.Thread(
        target=stream_worker,
        args=(
            st.session_state.base_url,
            st.session_state.player_id,
            bucket["queue"],
            stop_event,
        ),
        daemon=True,
    )
    bucket["stop_event"] = stop_event
    bucket["thread"] = thread
    bucket["active"] = True
    bucket["player_id"] = st.session_state.player_id
    bucket["base_url"] = st.session_state.base_url
    thread.start()


def stop_stream() -> None:
    bucket = stream_bucket()
    stop_event = bucket.get("stop_event")
    if stop_event is not None:
        stop_event.set()
    bucket["active"] = False


def clear_stream_events() -> None:
    bucket = stream_bucket()
    bucket["events"].clear()
    bucket["errors"].clear()


def render_response(title: str, payload: Any) -> None:
    st.markdown(f"### {title}")
    st.json(payload, expanded=2)


def render_error(exc: Exception) -> None:
    detail = st.session_state.last_error or {"error": str(exc)}
    st.error(str(exc))
    st.json(detail, expanded=2)


def render_object_cards(items: list[dict[str, Any]], title: str) -> None:
    st.markdown(f"### {title}")
    if not items:
        st.info("暂无数据")
        return
    for item in items:
        tags = "".join(f'<span class="sao-pill">{tag}</span>' for tag in item.get("tags", []))
        st.markdown(
            f"""
            <div class="sao-card">
              <h4 style="margin:0;">{item.get("name", "(unnamed)")}</h4>
              <div class="sao-caption">{item.get("id", "")} | layer: {item.get("layer", "")}</div>
              <div>{item.get("description", "")}</div>
              <div style="margin-top:0.7rem;">{tags}</div>
            </div>
            """,
            unsafe_allow_html=True,
        )
        with st.expander("查看 state"):
            st.json(item.get("state", {}), expanded=1)


def render_object_cards(items: list[dict[str, Any]], title: str) -> None:
    st.markdown(f"### {title}")
    if not items:
        st.info("No data.")
        return
    for item in items:
        with st.container(border=True):
            st.markdown(f"**{item.get('name', '(unnamed)')}**")
            st.caption(f"{item.get('id', '')} | layer: {item.get('layer', '')}")
            if item.get("description"):
                st.write(item.get("description"))
            tags = item.get("tags", [])
            if tags:
                st.write("Tags: " + ", ".join(str(tag) for tag in tags))
            with st.expander("State"):
                st.json(item.get("state", {}), expanded=1)


def refresh_scene_snapshot(show_errors: bool = False) -> dict[str, Any] | None:
    try:
        nearby = api_post(
            "/scene/nearby",
            {"player_id": st.session_state.player_id},
            timeout=12,
            remember=False,
            record=False,
        )
        st.session_state.map_snapshot = nearby
        st.session_state.map_last_refresh = time.strftime("%H:%M:%S")
        return nearby
    except Exception as exc:
        if show_errors:
            render_error(exc)
        return st.session_state.map_snapshot


def render_feedback_panel(limit: int = 8) -> None:
    feedback = list(st.session_state.feedback_log)[:limit]
    if not feedback:
        st.info("No live feedback yet. Start SSE, log in, then send an action.")
        return
    for item in feedback:
        failed = str(item.get("status", "")).lower() not in {"ok", "success"}
        meta_bits = [
            str(item.get("ts", "")),
            str(item.get("source", "")),
            str(item.get("path", "")),
        ]
        if item.get("scene_id"):
            meta_bits.append(f"scene={item['scene_id']}")
        if item.get("layer_id"):
            meta_bits.append(f"layer={item['layer_id']}")
        with st.container(border=True):
            if failed:
                st.error(str(item.get("title") or "(event)"))
            else:
                st.markdown(f"**{item.get('title') or '(event)'}**")
            st.caption(" | ".join(part for part in meta_bits if part))


def dot_escape(value: Any) -> str:
    text = str(value or "")
    text = text.replace("\\", "\\\\").replace('"', '\\"')
    return text.replace("\n", "\\n")


def object_summary(obj: dict[str, Any]) -> tuple[str, str, str]:
    obj_state = obj.get("state") if isinstance(obj.get("state"), dict) else {}
    name = str(obj.get("name") or obj.get("id") or "(object)")
    obj_id = str(obj.get("id") or "")
    desc = short_text(obj.get("description") or obj_state.get("description"), 100)
    return name, obj_id, desc


def layer_objects_for(layer_objects: dict[str, Any], layer_id: str) -> list[dict[str, Any]]:
    objects = layer_objects.get(layer_id, [])
    if isinstance(objects, list):
        return [obj for obj in objects if isinstance(obj, dict)]
    return []


def render_layer_objects(layer_id: str, objects: list[dict[str, Any]]) -> None:
    if not objects:
        st.caption("No visible objects in this layer.")
        return
    for obj in objects:
        name, obj_id, desc = object_summary(obj)
        with st.container(border=True):
            st.markdown(f"**{name}**")
            if obj_id:
                st.caption(obj_id)
            if desc:
                st.write(desc)


def build_map_dot(
    current: str,
    parent: str,
    children: list[str],
    layer_objects: dict[str, Any],
) -> str:
    lines = [
        "digraph sao_map {",
        "  graph [rankdir=TB, bgcolor=\"transparent\", pad=\"0.2\", nodesep=\"0.45\", ranksep=\"0.55\"];",
        "  node [shape=box, style=\"rounded,filled\", fontname=\"Segoe UI\", fontsize=11, color=\"#6f7d7b\", fillcolor=\"#ffffff\"];",
        "  edge [color=\"#7f8c8d\", arrowsize=0.7, fontname=\"Segoe UI\", fontsize=9];",
    ]
    node_ids: dict[str, str] = {}

    def add_layer_node(layer_id: str, role: str) -> str:
        if layer_id in node_ids:
            return node_ids[layer_id]
        node_name = f"layer_{len(node_ids)}"
        node_ids[layer_id] = node_name
        objects = layer_objects_for(layer_objects, layer_id)
        label = f"{role}\\n{layer_id}"
        if objects:
            label += f"\\nobjects: {len(objects)}"
        fill = "#fff8ef"
        color = "#b85c38"
        if role == "Parent":
            fill = "#f3efe6"
            color = "#8d806f"
        elif role == "Child":
            fill = "#edf7f3"
            color = "#3f7f75"
        lines.append(f'  {node_name} [label="{dot_escape(label)}", fillcolor="{fill}", color="{color}"];')
        return node_name

    current_node = add_layer_node(current or "Unknown current layer", "Current")
    if parent:
        parent_node = add_layer_node(parent, "Parent")
        lines.append(f"  {parent_node} -> {current_node} [label=\"contains\"];")
    else:
        lines.append('  root [label="Scene root", fillcolor="#f3efe6", color="#8d806f"];')
        lines.append(f"  root -> {current_node} [style=dashed, label=\"current\"];")

    for child in children:
        child_node = add_layer_node(child, "Child")
        lines.append(f"  {current_node} -> {child_node} [label=\"child\"];")

    object_index = 0
    layers_with_objects = [current] + children
    for layer_id in layers_with_objects:
        layer_node = node_ids.get(layer_id)
        if not layer_node:
            continue
        for obj in layer_objects_for(layer_objects, layer_id)[:4]:
            name, obj_id, _ = object_summary(obj)
            label = name if not obj_id else f"{name}\\n{obj_id}"
            obj_node = f"obj_{object_index}"
            object_index += 1
            lines.append(
                f'  {obj_node} [label="{dot_escape(label)}", shape=note, fillcolor="#ffffff", color="#c9b99b"];'
            )
            lines.append(f"  {layer_node} -> {obj_node} [style=dotted, label=\"object\"];")
    lines.append("}")
    return "\n".join(lines)


def build_map_tree(current: str, parent: str, children: list[str], layer_objects: dict[str, Any]) -> str:
    root = parent or "Scene root"
    current_objects = layer_objects_for(layer_objects, current)
    lines = [root]
    current_label = current or "Unknown current layer"
    lines.append(f"`-- {current_label} [current, objects={len(current_objects)}]")
    for obj in current_objects[:6]:
        name, obj_id, _ = object_summary(obj)
        suffix = f" ({obj_id})" if obj_id else ""
        lines.append(f"    |-- object: {name}{suffix}")
    for idx, child in enumerate(children):
        child_objects = layer_objects_for(layer_objects, child)
        branch = "`--" if idx == len(children) - 1 else "|--"
        lines.append(f"    {branch} {child} [child, objects={len(child_objects)}]")
        child_indent = "        " if idx == len(children) - 1 else "    |   "
        for obj in child_objects[:6]:
            name, obj_id, _ = object_summary(obj)
            suffix = f" ({obj_id})" if obj_id else ""
            lines.append(f"{child_indent}|-- object: {name}{suffix}")
    if not children and not current_objects:
        lines.append("    `-- no nearby child layers or visible objects")
    return "\n".join(lines)


def render_scene_map(snapshot: dict[str, Any] | None) -> None:
    if not snapshot:
        st.info("No scene snapshot yet. Click Refresh Map after login.")
        return
    current = str(snapshot.get("current_layer") or "")
    parent = str(snapshot.get("parent_layer") or "")
    children = snapshot.get("child_layers") or []
    if not isinstance(children, list):
        children = []
    layer_objects = snapshot.get("layer_objects") or {}
    if not isinstance(layer_objects, dict):
        layer_objects = {}
    children = [str(child) for child in children if str(child).strip()]
    current_objects = layer_objects_for(layer_objects, current)

    st.code(build_map_tree(current, parent, children, layer_objects), language="text")

    st.markdown("### Layer Hierarchy")
    tree_cols = st.columns([0.28, 0.36, 0.36])
    with tree_cols[0]:
        st.markdown("**Parent**")
        if parent:
            with st.container(border=True):
                st.write(parent)
        else:
            st.caption("Current layer is at the scene root.")
    with tree_cols[1]:
        st.markdown("**Current**")
        with st.container(border=True):
            st.success(current or "Unknown current layer")
            st.caption(f"{len(current_objects)} visible object(s)")
            render_layer_objects(current, current_objects)
    with tree_cols[2]:
        st.markdown("**Children**")
        if not children:
            st.caption("No child layers nearby.")
        for child in children:
            objects = layer_objects_for(layer_objects, child)
            with st.expander(f"{child} ({len(objects)} object(s))", expanded=False):
                render_layer_objects(child, objects)

    latest = next(iter(st.session_state.feedback_log), None)
    if latest:
        st.markdown("### Latest Feedback")
        render_feedback_panel(limit=1)


def maybe_refresh_scene_snapshot() -> None:
    if not st.session_state.live_auto_refresh:
        return
    now = time.time()
    interval = max(1.0, min(float(st.session_state.live_refresh_ms) / 1000.0, 15.0))
    if now - float(st.session_state.map_last_poll_ts) < interval:
        return
    st.session_state.map_last_poll_ts = now
    refresh_scene_snapshot(show_errors=False)


@st.fragment(run_every=1)
def render_live_map_fragment() -> None:
    bucket = stream_bucket()
    drain_stream_queue(bucket)
    maybe_refresh_scene_snapshot()

    top_left, top_right = st.columns([0.62, 0.38])
    with top_left:
        st.subheader("Live Map")
        st.caption("Current layer, nearby layers, visible objects, and latest backend feedback.")
    with top_right:
        c1, c2 = st.columns(2)
        with c1:
            if st.button("Refresh Map", use_container_width=True):
                refresh_scene_snapshot(show_errors=True)
        with c2:
            st.metric("Last Refresh", st.session_state.map_last_refresh or "-")

    map_col, feedback_col = st.columns([0.66, 0.34])
    with map_col:
        render_scene_map(st.session_state.map_snapshot)
    with feedback_col:
        st.markdown("### Live Feedback")
        render_feedback_panel(limit=10)

    with st.expander("Raw scene snapshot"):
        st.json(st.session_state.map_snapshot or {}, expanded=1)


ensure_state()


with st.sidebar:
    st.title("SAO Console")
    st.caption("Streamlit API demo for the Go TRPG backend")
    st.session_state.base_url = st.text_input("Base URL", st.session_state.base_url)
    st.session_state.player_id = st.text_input("Player ID", st.session_state.player_id)
    st.session_state.device_id = st.text_input("Device ID", st.session_state.device_id)
    st.session_state.scene_id = st.text_input("Scene ID", st.session_state.scene_id)
    st.session_state.player_name = st.text_input("Player Name", st.session_state.player_name)

    bucket = stream_bucket()
    st.session_state.live_auto_start = st.toggle(
        "Auto-start live feedback",
        value=st.session_state.live_auto_start,
    )
    st.session_state.live_auto_refresh = st.toggle(
        "Auto-refresh live panels",
        value=st.session_state.live_auto_refresh,
    )
    st.session_state.live_refresh_ms = st.slider(
        "Refresh interval (ms)",
        min_value=1000,
        max_value=15000,
        value=int(st.session_state.live_refresh_ms),
        step=500,
    )
    c1, c2 = st.columns(2)
    with c1:
        if st.button("Start SSE", use_container_width=True):
            start_stream()
    with c2:
        if st.button("Stop SSE", use_container_width=True):
            stop_stream()
    c3, c4 = st.columns(2)
    with c3:
        if st.button("Clear Events", use_container_width=True):
            clear_stream_events()
    with c4:
        st.metric("Event Count", len(bucket["events"]))

    st.markdown("---")
    st.caption("SSE status")
    st.write(
        {
            "active": bucket["active"],
            "player_id": bucket["player_id"],
            "base_url": bucket["base_url"],
        }
    )


bucket = stream_bucket()
if st.session_state.live_auto_start and not bucket["active"]:
    start_stream()
    bucket = stream_bucket()
if st.session_state.map_snapshot is None:
    refresh_scene_snapshot(show_errors=False)


header_left, header_right = st.columns([1.25, 0.75])
with header_left:
    st.title("SAO TRPG Frontend Demo")
    st.caption("覆盖登录、动作、场景、面板、背包和 SSE 事件流。")
with header_right:
    bucket = stream_bucket()
    st.metric("Streaming", "ON" if bucket["active"] else "OFF")
    st.metric("Player", st.session_state.player_id)


tabs = st.tabs(
    [
        "Auth",
        "NL Action",
        "Direct Actions",
        "Scene",
        "Player",
        "Live Map",
        "SSE",
        "Last Response",
    ]
)


with tabs[0]:
    left, right = st.columns(2)
    with left:
        st.subheader("Login")
        login_attrs = {}
        with st.form("login_form"):
            login_name = st.text_input("Display Name", st.session_state.player_name)
            login_scene = st.text_input("Login Scene", st.session_state.scene_id)
            with st.expander("Optional Attributes"):
                for attr in ATTRIBUTES:
                    login_attrs[attr] = st.number_input(
                        attr,
                        min_value=1,
                        max_value=30,
                        value=int(st.session_state.attribute_editor.get(attr, 10)),
                        key=f"login_{attr}",
                    )
            submitted = st.form_submit_button("Login", use_container_width=True)
        if submitted:
            payload = {
                "player_id": st.session_state.player_id,
                "device_id": st.session_state.device_id,
                "name": login_name,
                "scene_id": login_scene,
                "attributes": login_attrs,
            }
            try:
                render_response("Login Response", api_post("/auth/login", payload))
            except Exception as exc:
                render_error(exc)

    with right:
        st.subheader("Logout / Accounts")
        if st.button("Logout Current Device", use_container_width=True):
            try:
                render_response(
                    "Logout Response",
                    api_post(
                        "/auth/logout",
                        {
                            "player_id": st.session_state.player_id,
                            "device_id": st.session_state.device_id,
                        },
                    ),
                )
            except Exception as exc:
                render_error(exc)

        with st.form("accounts_form"):
            filter_scene_id = st.text_input("Scene Filter", st.session_state.scene_id)
            submitted = st.form_submit_button("List Available Accounts", use_container_width=True)
        if submitted:
            try:
                render_response(
                    "Available Accounts",
                    api_post("/auth/accounts", {"scene_id": filter_scene_id}),
                )
            except Exception as exc:
                render_error(exc)


with tabs[1]:
    st.subheader("Natural Language Action")
    st.caption("调用 `/nl-action`，让后端 referee 解析文本。")
    with st.form("nl_action_form"):
        nl_input = st.text_area("Input", "观察四周", height=120)
        submitted = st.form_submit_button("Send NL Action", use_container_width=True)
    if submitted:
        try:
            render_response(
                "NL Action Response",
                api_post(
                    "/nl-action",
                    {"player_id": st.session_state.player_id, "input": nl_input},
                ),
            )
        except Exception as exc:
            render_error(exc)


with tabs[2]:
    st.subheader("Direct Action Endpoints")
    action_tab_a, action_tab_b, action_tab_c, action_tab_d = st.tabs(
        ["/action", "/interact", "/observe", "/pick-drop"]
    )

    with action_tab_a:
        st.caption("完整动作信封，直接命中 `/action`。")
        default_action = {
            "type": "observe",
            "interaction": "",
            "target_object_id": "",
            "target_query": "四周",
            "layer_id": "",
            "dice_expr": "",
            "patch": {},
            "payload": {"raw_input": "观察四周"},
        }
        with st.form("action_form"):
            action_json = st.text_area(
                "Action JSON",
                value=json.dumps(default_action, ensure_ascii=False, indent=2),
                height=260,
            )
            submitted = st.form_submit_button("POST /action", use_container_width=True)
        if submitted:
            try:
                action = safe_json_text(action_json)
                render_response(
                    "/action Response",
                    api_post("/action", {"player_id": st.session_state.player_id, "action": action}),
                )
            except Exception as exc:
                render_error(exc)

    with action_tab_b:
        with st.form("interact_form"):
            interaction = st.selectbox(
                "Interaction",
                ["open_container", "pickup_item", "drop_item", ""],
                index=0,
            )
            target_query = st.text_input("Target Query", "箱子")
            target_object_id = st.text_input("Target Object ID", "")
            payload_text = st.text_area(
                "Payload JSON",
                value=json.dumps({"raw_input": "打开箱子"}, ensure_ascii=False, indent=2),
                height=120,
            )
            submitted = st.form_submit_button("POST /interact", use_container_width=True)
        if submitted:
            try:
                render_response(
                    "/interact Response",
                    api_post(
                        "/interact",
                        {
                            "player_id": st.session_state.player_id,
                            "interaction": interaction,
                            "target_query": target_query,
                            "target_object_id": target_object_id,
                            "payload": safe_json_text(payload_text),
                        },
                    ),
                )
            except Exception as exc:
                render_error(exc)

    with action_tab_c:
        with st.form("observe_form"):
            target_query = st.text_input("Observe Target Query", "四周")
            target_object_id = st.text_input("Observe Target Object ID", "")
            layer_id = st.text_input("Layer ID", "")
            payload_text = st.text_area(
                "Observe Payload JSON",
                value=json.dumps({"raw_input": "观察四周"}, ensure_ascii=False, indent=2),
                height=120,
            )
            submitted = st.form_submit_button("POST /observe", use_container_width=True)
        if submitted:
            try:
                render_response(
                    "/observe Response",
                    api_post(
                        "/observe",
                        {
                            "player_id": st.session_state.player_id,
                            "target_query": target_query,
                            "target_object_id": target_object_id,
                            "layer_id": layer_id,
                            "payload": safe_json_text(payload_text),
                        },
                    ),
                )
            except Exception as exc:
                render_error(exc)

    with action_tab_d:
        with st.form("pick_drop_form"):
            pick_drop_action = st.selectbox("Action", ["pick", "drop"])
            target_query = st.text_input("Item Query", "测试钥匙")
            target_object_id = st.text_input("Item Object ID", "")
            payload_text = st.text_area(
                "Pick/Drop Payload JSON",
                value=json.dumps({"raw_input": "捡起测试钥匙"}, ensure_ascii=False, indent=2),
                height=120,
            )
            submitted = st.form_submit_button("POST /pick-drop", use_container_width=True)
        if submitted:
            try:
                render_response(
                    "/pick-drop Response",
                    api_post(
                        "/pick-drop",
                        {
                            "player_id": st.session_state.player_id,
                            "action": pick_drop_action,
                            "target_query": target_query,
                            "target_object_id": target_object_id,
                            "payload": safe_json_text(payload_text),
                        },
                    ),
                )
            except Exception as exc:
                render_error(exc)


with tabs[3]:
    st.subheader("Scene Tools")
    left, right = st.columns(2)
    with left:
        with st.form("create_layer_form"):
            layer_id = st.text_input("New Layer ID", "scene-测试区")
            relation = st.text_input("Relation", "child")
            parent_layer_id = st.text_input("Parent Layer ID", st.session_state.scene_id)
            submitted = st.form_submit_button("POST /scene/layer", use_container_width=True)
        if submitted:
            try:
                render_response(
                    "/scene/layer Response",
                    api_post(
                        "/scene/layer",
                        {
                            "player_id": st.session_state.player_id,
                            "layer_id": layer_id,
                            "relation": relation,
                            "parent_layer_id": parent_layer_id,
                        },
                    ),
                )
            except Exception as exc:
                render_error(exc)

    with right:
        st.caption("查看当前层、父层、子层和各层物体。")
        if st.button("POST /scene/nearby", use_container_width=True):
            try:
                nearby = api_post("/scene/nearby", {"player_id": st.session_state.player_id})
                st.session_state.map_snapshot = nearby
                st.session_state.map_last_refresh = time.strftime("%H:%M:%S")
                render_response("/scene/nearby Response", nearby)
                layer_objects = nearby.get("layer_objects", {})
                for layer_name, objects in layer_objects.items():
                    render_object_cards(objects, f"Layer: {layer_name}")
            except Exception as exc:
                render_error(exc)


with tabs[4]:
    st.subheader("Player Panel & Inventory")
    left, right = st.columns(2)
    with left:
        st.markdown("### Attribute Panel")
        col_a, col_b = st.columns(2)
        with col_a:
            if st.button("POST /player/panel/get", use_container_width=True):
                try:
                    panel = api_post("/player/panel/get", {"player_id": st.session_state.player_id})
                    render_response("Panel Response", panel)
                    attrs = panel.get("attributes", {})
                    for attr in ATTRIBUTES:
                        if attr in attrs:
                            st.session_state.attribute_editor[attr] = attrs[attr]
                except Exception as exc:
                    render_error(exc)
        with col_b:
            if st.button("POST /player/inventory/get", use_container_width=True):
                try:
                    inventory = api_post("/player/inventory/get", {"player_id": st.session_state.player_id})
                    render_response("Inventory Response", inventory)
                    render_object_cards(inventory.get("items", []), "Inventory Items")
                except Exception as exc:
                    render_error(exc)

    with right:
        with st.form("panel_set_form"):
            st.caption("部分更新属性，提交到 `/player/panel/set`。")
            new_attrs = {}
            for attr in ATTRIBUTES:
                new_attrs[attr] = st.number_input(
                    attr,
                    min_value=1,
                    max_value=30,
                    value=int(st.session_state.attribute_editor.get(attr, 10)),
                    key=f"editor_{attr}",
                )
            submitted = st.form_submit_button("POST /player/panel/set", use_container_width=True)
        if submitted:
            try:
                panel = api_post(
                    "/player/panel/set",
                    {"player_id": st.session_state.player_id, "attributes": new_attrs},
                )
                st.session_state.attribute_editor.update(new_attrs)
                render_response("Panel Update Response", panel)
            except Exception as exc:
                render_error(exc)


with tabs[5]:
    render_live_map_fragment()


with tabs[6]:
    bucket = stream_bucket()
    left, right = st.columns([0.7, 0.3])
    with left:
        st.subheader("Live Event Stream")
        st.caption("来自 `GET /events?player_id=...`。页面会在交互时刷新并读取后台监听线程已收集的事件。")
    with right:
        if st.button("Manual Refresh", use_container_width=True):
            drain_stream_queue(bucket)

    if bucket["errors"]:
        st.warning("SSE listener has errors")
        st.json(list(bucket["errors"]), expanded=1)

    events = list(bucket["events"])
    if not events:
        st.info("还没有收到事件。先登录，再启动 SSE，然后执行几个动作。")
    else:
        for item in events:
            with st.container(border=True):
                cols = st.columns([0.18, 0.18, 0.64])
                cols[0].markdown(f"**{item['ts']}**")
                cols[1].markdown(f"`{item['event']}`")
                cols[2].json(item["data"], expanded=1)

    st.markdown("### Feedback Summary")
    render_feedback_panel(limit=12)


with tabs[7]:
    st.subheader("Last Response / Error")
    if st.session_state.last_response:
        st.markdown("### Last Success")
        st.json(st.session_state.last_response, expanded=2)
    else:
        st.info("还没有成功请求。")
    if st.session_state.last_error:
        st.markdown("### Last Error")
        st.json(st.session_state.last_error, expanded=2)
