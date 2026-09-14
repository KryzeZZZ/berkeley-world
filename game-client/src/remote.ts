import type { Entity, Item, WorldEvent, WorldState } from "./types";

export const pixelAPIBaseURL = import.meta.env.VITE_PIXEL_API_BASE_URL ?? "http://127.0.0.1:8080";

type RemotePlayer = { id: string; name: string; x: number; y: number; inventory?: Item[] };
type RemoteWorld = {
  revision: number;
  players: Record<string, RemotePlayer>;
  entities: Entity[];
  changed_tiles: Record<string, "path" | "grass">;
  event_log: Array<{ id: string; title: string; detail: string; time: number }>;
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
	const response = await fetch(`${pixelAPIBaseURL}${path}`, init);
  if (!response.ok) throw new Error(`pixel api ${response.status}`);
  return response.json() as Promise<T>;
}

function toWorld(world: RemoteWorld): WorldState {
  const player = world.players.p1;
  if (!player) throw new Error("pixel api did not return p1");
  return {
    revision: world.revision,
    player: { name: player.name, x: player.x, y: player.y, inventory: player.inventory ?? [] },
    entities: world.entities,
    changedTiles: world.changed_tiles ?? {},
    eventLog: (world.event_log ?? []).map((event): WorldEvent => ({ ...event }))
  };
}

export async function loadRemoteWorld(): Promise<WorldState> {
  const response = await request<{ world: RemoteWorld }>("/pixel/world");
  return toWorld(response.world);
}

export async function moveRemoteWorld(x: number, y: number): Promise<WorldState> {
  const response = await request<{ world: RemoteWorld }>("/pixel/move", {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ player_id: "p1", to: { x, y } })
  });
  return toWorld(response.world);
}

export async function interactRemoteWorld(entityID: string): Promise<WorldState> {
  const response = await request<{ world: RemoteWorld }>("/pixel/interact", {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ player_id: "p1", entity_id: entityID })
  });
  return toWorld(response.world);
}

export async function useItemRemote(itemID: string, x: number, y: number): Promise<WorldState> {
  const response = await request<{ world: RemoteWorld }>("/pixel/use-item", {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ player_id: "p1", item_id: itemID, target: { x, y } })
  });
  return toWorld(response.world);
}

export async function resetRemoteWorld(): Promise<WorldState> {
  const response = await request<{ world: RemoteWorld }>("/pixel/reset", { method: "POST" });
  return toWorld(response.world);
}

export function subscribeRemoteWorld(onEvent: () => void): EventSource {
	const stream = new EventSource(`${pixelAPIBaseURL}/pixel/events`);
  ["entity_moved", "entity_updated", "world_reset"].forEach((type) => stream.addEventListener(type, onEvent));
  return stream;
}
