import type { Entity, WorldEvent, WorldState } from "./types";

export const MAP_WIDTH = 30;
export const MAP_HEIGHT = 21;
export const TILE_SIZE = 32;

const nowEvent = (title: string, detail: string): WorldEvent => ({
  id: crypto.randomUUID(),
  title,
  detail,
  time: Date.now(),
});

const entities: Entity[] = [
  { id: "chest-amber", kind: "chest", name: "琥珀补给箱", position: { x: 8, y: 8 }, state: { opened: false } },
  { id: "gate-ruin", kind: "gate", name: "遗迹石门", position: { x: 22, y: 5 }, state: { opened: false } },
  { id: "npc-scout", kind: "npc", name: "巡林侦察员", position: { x: 17, y: 14 }, state: { spoken: false } },
  { id: "crystal-aqua", kind: "crystal", name: "传送水晶", position: { x: 5, y: 16 }, state: { active: true } }
];

export const createInitialWorld = (): WorldState => ({
  revision: 1,
  player: { name: "爱丽丝", x: 14, y: 10, inventory: [{ id: "staff-rift", name: "破界法杖", kind: "terrain_breaker", quantity: 1, state: { sprite: "staff" } }] },
  entities: structuredClone(entities),
  changedTiles: {},
  eventLog: [nowEvent("区域同步完成", "本地世界已载入")]
});

export const getTileType = (x: number, y: number, state: WorldState): "water" | "tree" | "path" | "grass" => {
  const changed = state.changedTiles[`${x},${y}`];
  if (changed) return changed;
  if (x < 0 || y < 0 || x >= MAP_WIDTH || y >= MAP_HEIGHT) return "tree";
  if ((x >= 11 && x <= 12) || (x >= 19 && x <= 20 && y > 11)) return "water";
  if ((x < 2 || x > 27) && y % 3 !== 1) return "tree";
  if ((y < 2 || y > 18) && x % 4 !== 2) return "tree";
  if ((x === 14 || x === 15) && y > 2 && y < 18) return "path";
  if (y === 10 && x > 4 && x < 25) return "path";
  if (x > 18 && y > 3 && y < 8) return "path";
  if (x < 10 && y > 13) return "path";
  return "grass";
};

export const isBlocked = (x: number, y: number, state: WorldState): boolean => {
  const tile = getTileType(x, y, state);
  if (tile === "water" || tile === "tree") return true;
  return state.entities.some((entity) => {
    if (entity.position.x !== x || entity.position.y !== y) return false;
    return entity.kind === "gate" && entity.state.opened !== true;
  });
};

export const appendEvent = (state: WorldState, title: string, detail: string): WorldState => ({
  ...state,
  revision: state.revision + 1,
  eventLog: [nowEvent(title, detail), ...state.eventLog].slice(0, 8)
});
