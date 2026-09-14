import Phaser from "phaser";
import "./style.css";
import { interactRemoteWorld, loadRemoteWorld, moveRemoteWorld, pixelAPIBaseURL, resetRemoteWorld, subscribeRemoteWorld, useItemRemote } from "./remote";
import { loadWorld, resetWorld, saveWorld } from "./storage";
import type { Entity, Point, WorldEvent, WorldState } from "./types";
import { appendEvent, getTileType, isBlocked, MAP_HEIGHT, MAP_WIDTH, TILE_SIZE } from "./world";

const elements = {
  position: document.querySelector<HTMLSpanElement>("#player-position")!,
  contextTitle: document.querySelector<HTMLElement>("#context-title")!,
  contextCopy: document.querySelector<HTMLElement>("#context-copy")!,
  contextKey: document.querySelector<HTMLElement>("#context-key")!,
  eventList: document.querySelector<HTMLElement>("#event-list")!,
  saveState: document.querySelector<HTMLElement>("#save-state")!,
  interact: document.querySelector<HTMLButtonElement>("#interact-button")!,
  reset: document.querySelector<HTMLButtonElement>("#reset-button")!,
  staff: document.querySelector<HTMLButtonElement>("#staff-button")!,
  staffIcon: document.querySelector<HTMLImageElement>("#staff-icon")!,
  staffCount: document.querySelector<HTMLElement>("#staff-count")!
};

let state: WorldState;
let scene: PixelScene;
let remoteMode = false;
let staffTargeting = false;

function renderHud(nearby?: Entity): void {
  elements.position.textContent = `X ${state.player.x} / Y ${state.player.y}`;
  elements.contextTitle.textContent = nearby ? nearby.name : "探索森林遗迹";
  elements.contextCopy.textContent = nearby ? describeEntity(nearby) : "沿道路前进，寻找可交互目标。";
  elements.contextKey.textContent = nearby ? "E 交互" : "WASD";
  elements.interact.disabled = !nearby;
  const staff = state.player.inventory.find((item) => item.id === "staff-rift");
  elements.staffCount.textContent = `x${staff?.quantity ?? 0}`;
  elements.staff.disabled = !staff;
  elements.staff.classList.toggle("is-selected", staffTargeting);
  elements.eventList.replaceChildren(...state.eventLog.slice(0, 4).map(renderEvent));
}

function renderEvent(event: WorldEvent): HTMLElement {
  const item = document.createElement("div");
  item.className = "event-item";
  item.innerHTML = `<strong>${event.title}</strong><span>${event.detail}</span>`;
  return item;
}

function describeEntity(entity: Entity): string {
  if (entity.kind === "chest") return entity.state.opened ? "箱内已空。" : "锁扣泛着琥珀色微光。";
  if (entity.kind === "gate") return entity.state.opened ? "石门已开启。" : "门缝透出冷白光。";
  if (entity.kind === "npc") return entity.state.spoken ? "侦察员正在观察遗迹。" : "他似乎有情报要交代。";
  return "水晶在空气中发出低鸣。";
}

function findNearby(): Entity | undefined {
  return state.entities.find((entity) => Math.abs(entity.position.x - state.player.x) + Math.abs(entity.position.y - state.player.y) <= 1);
}

async function commit(next: WorldState, saveLocal = !remoteMode): Promise<void> {
  state = next;
  if (saveLocal) await saveWorld(state);
  elements.saveState.textContent = remoteMode ? "FILE SAVED" : "SAVED";
  window.setTimeout(() => { elements.saveState.textContent = remoteMode ? "LOCAL FILE" : "LOCAL SAVE"; }, 900);
  scene.redraw();
}

async function movePlayer(to: Point): Promise<void> {
  if (isBlocked(to.x, to.y, state)) return;
  if (remoteMode) {
    try { await commit(await moveRemoteWorld(to.x, to.y), false); } catch { elements.saveState.textContent = "SYNC ERROR"; }
    return;
  }
  await commit(appendEvent({ ...state, player: { ...state.player, ...to } }, "位置更新", `抵达 ${to.x}, ${to.y}`));
}

async function interact(): Promise<void> {
  const nearby = findNearby();
  if (!nearby) return;
  if (remoteMode) {
    try { await commit(await interactRemoteWorld(nearby.id), false); scene.playGeneratedObjectAnimation(nearby); } catch { elements.saveState.textContent = "SYNC ERROR"; }
    return;
  }
  const entities = state.entities.map((entity) => {
    if (entity.id !== nearby.id) return entity;
    if (entity.kind === "chest") return { ...entity, state: { ...entity.state, opened: true } };
    if (entity.kind === "gate") return { ...entity, state: { ...entity.state, opened: true } };
    if (entity.kind === "npc") return { ...entity, state: { ...entity.state, spoken: true } };
    return { ...entity, state: { ...entity.state, active: !entity.state.active } };
  });
  const title = nearby.kind === "chest" ? "补给箱开启" : nearby.kind === "gate" ? "遗迹石门开启" : nearby.kind === "npc" ? "获得区域情报" : "水晶频率改变";
  await commit(appendEvent({ ...state, entities }, title, describeEntity({ ...nearby, state: entities.find((entity) => entity.id === nearby.id)!.state })));
}

async function useStaffAt(target: Point): Promise<void> {
  if (!remoteMode) return;
  const targetEntity = state.entities.find((entity) => entity.position.x === target.x && entity.position.y === target.y && entity.state.destroyed !== true);
	const terrainKind = getTileType(target.x, target.y, state);
  try {
    await commit(await useItemRemote("staff-rift", target.x, target.y), false);
    staffTargeting = false;
		scene.refreshMap(() => {
			if (targetEntity) scene.playGeneratedObjectAnimation(targetEntity, "break");
			else scene.playGeneratedTerrainAnimation(terrainKind, target);
		});
  } catch {
    elements.saveState.textContent = "INVALID TARGET";
  }
}

class PixelScene extends Phaser.Scene {
  private player!: Phaser.GameObjects.Container;
  private entityViews = new Map<string, Phaser.GameObjects.Container>();
  private keys!: Record<string, Phaser.Input.Keyboard.Key>;
  private moving = false;
  private mapTextureKey = "pixel-map";

  constructor() { super("pixel-world"); }

  preload(): void {
    this.load.image("pixel-map", `${pixelAPIBaseURL}/pixel/render/map.png`);
    this.load.image("sprite-player", `${pixelAPIBaseURL}/pixel/render/sprite/player.png`);
    this.load.image("sprite-staff", `${pixelAPIBaseURL}/pixel/render/sprite/staff.png`);
    this.load.image("sprite-chest-closed", `${pixelAPIBaseURL}/pixel/render/sprite/chest.png?variant=closed`);
    this.load.image("sprite-chest-open", `${pixelAPIBaseURL}/pixel/render/sprite/chest.png?variant=open`);
    this.load.image("sprite-gate-closed", `${pixelAPIBaseURL}/pixel/render/sprite/gate.png?variant=closed`);
    this.load.image("sprite-gate-open", `${pixelAPIBaseURL}/pixel/render/sprite/gate.png?variant=open`);
    this.load.image("sprite-npc", `${pixelAPIBaseURL}/pixel/render/sprite/npc.png`);
    this.load.image("sprite-crystal-active", `${pixelAPIBaseURL}/pixel/render/sprite/crystal.png?variant=active`);
    this.load.image("sprite-crystal-inactive", `${pixelAPIBaseURL}/pixel/render/sprite/crystal.png?variant=inactive`);
	state.entities.filter((entity) => entity.state.destroyed !== true).forEach((entity) => {
		this.load.image(objectTextureKey(entity), objectAssetURL(entity));
	});
    ["chest", "gate", "npc", "crystal", "effect-break-tree", "effect-break-grass", "effect-break-path", "effect-break-water"].forEach((kind) => {
      this.load.spritesheet(`animation-${kind}`, `${pixelAPIBaseURL}/pixel/render/animation/${kind}.png`, { frameWidth: 32, frameHeight: 32 });
    });
  }

  create(): void {
    this.cameras.main.setBackgroundColor("#152b24");
    this.keys = this.input.keyboard!.addKeys("W,A,S,D,UP,DOWN,LEFT,RIGHT,E") as Record<string, Phaser.Input.Keyboard.Key>;
    this.input.on("pointerdown", (pointer: Phaser.Input.Pointer) => {
      const x = Math.floor(pointer.worldX / TILE_SIZE);
      const y = Math.floor(pointer.worldY / TILE_SIZE);
      if (staffTargeting) { void useStaffAt({ x, y }); return; }
      this.moveTo({ x, y });
    });
    this.cameras.main.setBounds(0, 0, MAP_WIDTH * TILE_SIZE, MAP_HEIGHT * TILE_SIZE);
    this.cameras.main.setZoom(1.45);
    this.cameras.main.centerOn(state.player.x * TILE_SIZE, state.player.y * TILE_SIZE);
    ["chest", "gate", "npc", "crystal", "effect-break-tree", "effect-break-grass", "effect-break-path", "effect-break-water"].forEach((kind) => {
      this.anims.create({ key: `play-${kind}`, frames: this.anims.generateFrameNumbers(`animation-${kind}`, { start: 0, end: 11 }), frameRate: 11, repeat: 0 });
    });
    this.redraw();
  }

  update(): void {
    if (Phaser.Input.Keyboard.JustDown(this.keys.E)) void interact();
    if (this.moving) return;
    const direction = Phaser.Input.Keyboard.JustDown(this.keys.W) || Phaser.Input.Keyboard.JustDown(this.keys.UP) ? { x: 0, y: -1 }
      : Phaser.Input.Keyboard.JustDown(this.keys.S) || Phaser.Input.Keyboard.JustDown(this.keys.DOWN) ? { x: 0, y: 1 }
      : Phaser.Input.Keyboard.JustDown(this.keys.A) || Phaser.Input.Keyboard.JustDown(this.keys.LEFT) ? { x: -1, y: 0 }
      : Phaser.Input.Keyboard.JustDown(this.keys.D) || Phaser.Input.Keyboard.JustDown(this.keys.RIGHT) ? { x: 1, y: 0 }
      : undefined;
    if (direction) this.moveTo({ x: state.player.x + direction.x, y: state.player.y + direction.y });
  }

  refreshMap(afterRefresh?: () => void): void {
    if (this.load.isLoading()) return;
    const nextKey = `pixel-map-${state.revision}`;
    if (nextKey === this.mapTextureKey) return;
    const previousKey = this.mapTextureKey;
    this.load.image(nextKey, `${pixelAPIBaseURL}/pixel/render/map.png?revision=${state.revision}`);
    this.load.once(Phaser.Loader.Events.COMPLETE, () => {
      this.mapTextureKey = nextKey;
      this.redraw();
      this.textures.remove(previousKey);
      afterRefresh?.();
    });
    this.load.start();
  }

  redraw(): void {
    this.children.removeAll(true);
    this.entityViews.clear();
    this.drawTerrain();
    state.entities.filter((entity) => entity.state.destroyed !== true).forEach((entity) => this.drawEntity(entity));
    this.drawPlayer();
    renderHud(findNearby());
  }

  private drawTerrain(): void {
    this.add.image((MAP_WIDTH * TILE_SIZE) / 2, (MAP_HEIGHT * TILE_SIZE) / 2, this.mapTextureKey)
      .setDisplaySize(MAP_WIDTH * TILE_SIZE, MAP_HEIGHT * TILE_SIZE)
      .setDepth(-1);
  }

  private drawEntity(entity: Entity): void {
    const x = entity.position.x * TILE_SIZE + 16;
    const y = entity.position.y * TILE_SIZE + 16;
    const group = this.add.container(x, y).setDepth(y);
    const generatedKey = objectTextureKey(entity);
    if (!this.textures.exists(generatedKey)) this.queueObjectAsset(entity);
    const size = objectDisplaySize(entity);
    group.add(this.add.image(0, 0, this.textures.exists(generatedKey) ? generatedKey : spriteKey(entity)).setDisplaySize(size.width, size.height));
    group.add(this.add.rectangle(0, 13, 22, 4, 0x10251f, 0.6));
    const label = this.add.text(0, -24, entity.name, {
      fontFamily: "Microsoft YaHei UI, Microsoft YaHei, Arial, sans-serif",
      fontSize: "15px",
      fontStyle: "bold",
      color: "#f8ffe9",
      backgroundColor: "#10261fe8",
      padding: { x: 4, y: 2 },
      stroke: "#10261f",
      strokeThickness: 2
    }).setOrigin(0.5).setResolution(4);
    group.add(label);
    this.entityViews.set(entity.id, group);
  }

  private drawPlayer(): void {
    const x = state.player.x * TILE_SIZE + 16;
    const y = state.player.y * TILE_SIZE + 16;
    this.player = this.add.container(x, y).setDepth(y + 1);
	this.player.add(this.add.image(0, 4, "sprite-player").setDisplaySize(16, 24));
	if (state.player.inventory.some((item) => item.id === "staff-rift" && item.quantity > 0)) {
		this.player.add(this.add.image(9, 2, "sprite-staff").setDisplaySize(16, 16).setRotation(0.35));
	}
    this.cameras.main.startFollow(this.player, true, 0.14, 0.14);
  }

	private queueObjectAsset(entity: Entity): void {
		if (this.load.isLoading()) return;
		const key = objectTextureKey(entity);
		this.load.image(key, objectAssetURL(entity));
		this.load.once(Phaser.Loader.Events.COMPLETE, () => this.redraw());
		this.load.start();
	}

  private moveTo(to: Point): void {
    if (this.moving || Math.abs(to.x - state.player.x) + Math.abs(to.y - state.player.y) !== 1) return;
    if (isBlocked(to.x, to.y, state)) return;
    this.moving = true;
    this.tweens.add({
      targets: this.player,
      x: to.x * TILE_SIZE + 16,
      y: to.y * TILE_SIZE + 16,
      duration: 115,
      ease: "Linear",
      onComplete: () => { this.moving = false; void movePlayer(to); }
    });
  }

  playServerAnimation(kind: string, position: Point): void {
    const textureKey = `animation-${kind}`;
    const animationKey = `play-${kind}`;
    if (!this.textures.exists(textureKey) || !this.anims.exists(animationKey)) return;
    const effect = this.add.sprite(position.x * TILE_SIZE + 16, position.y * TILE_SIZE + 16, textureKey)
      .setDisplaySize(44, 44)
      .setDepth(position.y * TILE_SIZE + 100);
    effect.play(animationKey);
    effect.once(Phaser.Animations.Events.ANIMATION_COMPLETE, () => effect.destroy());
  }

	playGeneratedObjectAnimation(entity: Entity, action = "interact"): void {
		const dimensions = action === "break" ? { width: 32, height: 32 } : objectDisplaySize(entity);
		const textureKey = `object-animation-${entity.id}-${objectStateHash(entity)}-${action}`;
		const animationKey = `play-${textureKey}`;
		const play = (): void => {
			if (!this.anims.exists(animationKey)) {
				this.anims.create({ key: animationKey, frames: this.anims.generateFrameNumbers(textureKey, { start: 0, end: 11 }), frameRate: 11, repeat: 0 });
			}
			const effect = this.add.sprite(entity.position.x * TILE_SIZE + 16, entity.position.y * TILE_SIZE + 16, textureKey)
				.setDisplaySize(dimensions.width, dimensions.height)
				.setDepth(entity.position.y * TILE_SIZE + 100);
			effect.play(animationKey);
			effect.once(Phaser.Animations.Events.ANIMATION_COMPLETE, () => effect.destroy());
		};
		if (this.textures.exists(textureKey)) { play(); return; }
		if (this.load.isLoading()) return;
		this.load.spritesheet(textureKey, `${pixelAPIBaseURL}/pixel/render/object-animation/${encodeURIComponent(entity.id)}.png?action=${action}`, { frameWidth: dimensions.width, frameHeight: dimensions.height });
		this.load.once(Phaser.Loader.Events.COMPLETE, play);
		this.load.start();
	}

	playGeneratedTerrainAnimation(kind: string, position: Point): void {
		const textureKey = `terrain-animation-${kind}-${position.x}-${position.y}-${state.revision}`;
		const animationKey = `play-${textureKey}`;
		const play = (): void => {
			if (!this.anims.exists(animationKey)) {
				this.anims.create({ key: animationKey, frames: this.anims.generateFrameNumbers(textureKey, { start: 0, end: 11 }), frameRate: 11, repeat: 0 });
			}
			const effect = this.add.sprite(position.x * TILE_SIZE + 16, position.y * TILE_SIZE + 16, textureKey).setDepth(position.y * TILE_SIZE + 100);
			effect.play(animationKey);
			effect.once(Phaser.Animations.Events.ANIMATION_COMPLETE, () => effect.destroy());
		};
		if (this.textures.exists(textureKey)) { play(); return; }
		if (this.load.isLoading()) return;
		this.load.spritesheet(textureKey, `${pixelAPIBaseURL}/pixel/render/terrain-animation/${encodeURIComponent(kind)}.png?x=${position.x}&y=${position.y}`, { frameWidth: 32, frameHeight: 32 });
		this.load.once(Phaser.Loader.Events.COMPLETE, play);
		this.load.start();
	}
}

function spriteKey(entity: Entity): string {
  if (entity.kind === "chest") return entity.state.opened ? "sprite-chest-open" : "sprite-chest-closed";
  if (entity.kind === "gate") return entity.state.opened ? "sprite-gate-open" : "sprite-gate-closed";
  if (entity.kind === "crystal") return entity.state.active ? "sprite-crystal-active" : "sprite-crystal-inactive";
  return "sprite-npc";
}

function objectTextureKey(entity: Entity): string {
	return `object-${entity.id}-${objectStateHash(entity)}`;
}

function objectAssetURL(entity: Entity): string {
	return `${pixelAPIBaseURL}/pixel/render/object/${encodeURIComponent(entity.id)}.png?state=${objectStateHash(entity)}`;
}

function objectStateHash(entity: Entity): string {
	const source = JSON.stringify(entity.state);
	let hash = 2166136261;
	for (let index = 0; index < source.length; index += 1) {
		hash ^= source.charCodeAt(index);
		hash = Math.imul(hash, 16777619);
	}
	return (hash >>> 0).toString(36);
}

function objectDisplaySize(entity: Entity): { width: number; height: number } {
	if (entity.kind === "npc") return { width: 24, height: 36 };
	if (entity.kind === "gate") return { width: 64, height: 64 };
	return { width: 64, height: 64 };
}

async function bootstrap(): Promise<void> {
  try {
    state = await loadRemoteWorld();
    remoteMode = true;
    elements.saveState.textContent = "LOCAL FILE";
  } catch {
    state = await loadWorld();
  }
  scene = new PixelScene();
  new Phaser.Game({
    type: Phaser.AUTO,
    parent: "game-root",
    width: 960,
    height: 672,
    pixelArt: true,
    scene,
    backgroundColor: "#152b24",
    scale: { mode: Phaser.Scale.FIT, autoCenter: Phaser.Scale.CENTER_BOTH }
  });
  elements.interact.addEventListener("click", () => void interact());
  elements.reset.addEventListener("click", async () => {
    state = remoteMode ? await resetRemoteWorld() : await resetWorld();
    scene.redraw();
  });
  elements.staffIcon.src = `${pixelAPIBaseURL}/pixel/render/sprite/staff.png`;
  elements.staff.addEventListener("click", () => { staffTargeting = !staffTargeting; renderHud(findNearby()); });
  if (remoteMode) {
    subscribeRemoteWorld(async () => {
      try { await commit(await loadRemoteWorld(), false); } catch { /* The next action retries synchronization. */ }
    });
  }
}

void bootstrap();
