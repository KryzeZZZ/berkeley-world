import Dexie, { type EntityTable } from "dexie";
import type { WorldState } from "./types";
import { createInitialWorld } from "./world";

type SaveRecord = { id: "active"; state: WorldState; updatedAt: number };

class LocalWorldDatabase extends Dexie {
  saves!: EntityTable<SaveRecord, "id">;

  constructor() {
    super("sao-pixel-world");
    this.version(1).stores({ saves: "id, updatedAt" });
  }
}

const database = new LocalWorldDatabase();

export async function loadWorld(): Promise<WorldState> {
  const save = await database.saves.get("active");
  return save?.state ?? createInitialWorld();
}

export async function saveWorld(state: WorldState): Promise<void> {
  await database.saves.put({ id: "active", state, updatedAt: Date.now() });
}

export async function resetWorld(): Promise<WorldState> {
  const state = createInitialWorld();
  await saveWorld(state);
  return state;
}
