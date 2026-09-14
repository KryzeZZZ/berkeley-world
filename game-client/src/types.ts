export type Point = { x: number; y: number };

export type EntityKind = "chest" | "gate" | "npc" | "crystal";

export type Entity = {
  id: string;
  kind: EntityKind;
  name: string;
  position: Point;
  state: Record<string, boolean | number | string>;
};

export type Item = {
  id: string;
  name: string;
  kind: string;
  quantity: number;
  state: Record<string, boolean | number | string>;
};

export type Player = Point & { name: string; inventory: Item[] };

export type WorldState = {
  revision: number;
  player: Player;
  entities: Entity[];
  changedTiles: Record<string, "path" | "grass">;
  eventLog: WorldEvent[];
};

export type WorldEvent = {
  id: string;
  title: string;
  detail: string;
  time: number;
};
