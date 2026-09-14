package pixelworld

import (
	"image"
	"image/color"
	"image/draw"
)

const RenderTileSize = 32

func RenderMap(snapshot Snapshot) image.Image {
	base := image.NewRGBA(image.Rect(0, 0, MapWidth*16, MapHeight*16))
	for y := 0; y < MapHeight; y++ {
		for x := 0; x < MapWidth; x++ {
			drawTile(base, x, y, tileKind(Position{X: x, Y: y}, snapshot))
		}
	}
	canvas := scale2x(base)
	for y := 0; y < MapHeight; y++ {
		for x := 0; x < MapWidth; x++ {
			addTileDetail(canvas.(*image.RGBA), x*32, y*32, tileKind(Position{X: x, Y: y}, snapshot), x, y)
		}
	}
	return canvas
}

// RenderGeneratedMap composes the complete scene server-side from generated
// terrain matrices. The client receives one map image and never recreates
// terrain tiles in the browser.
func RenderGeneratedMap(snapshot Snapshot) (image.Image, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, MapWidth*RenderTileSize, MapHeight*RenderTileSize))
	assets := map[string]image.Image{}
	for y := 0; y < MapHeight; y++ {
		for x := 0; x < MapWidth; x++ {
			kind := tileKind(Position{X: x, Y: y}, snapshot)
			asset, ok := assets[kind]
			if !ok {
				matrix, err := LoadOrGenerateTerrainAsset(kind)
				if err != nil {
					return nil, err
				}
				asset, err = RenderMatrixAsset(matrix)
				if err != nil {
					return nil, err
				}
				assets[kind] = opaqueTerrainTile(asset)
			}
			draw.Draw(canvas, image.Rect(x*RenderTileSize, y*RenderTileSize, (x+1)*RenderTileSize, (y+1)*RenderTileSize), asset, image.Point{}, draw.Src)
		}
	}
	return canvas, nil
}

// Terrain matrices may use transparency for decorative detail. A map tile is
// different from a sprite: it must cover its full rectangle, otherwise paths
// and water read as disconnected stickers. Fill transparent pixels with the
// dominant generated color before composing the complete scene.
func opaqueTerrainTile(source image.Image) image.Image {
	bounds := source.Bounds()
	counts := map[color.RGBA]int{}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.RGBAModel.Convert(source.At(x, y)).(color.RGBA)
			if c.A > 0 {
				counts[c]++
			}
		}
	}
	background := color.RGBA{39, 80, 54, 255}
	for c, count := range counts {
		if count > counts[background] {
			background = c
		}
	}
	canvas := image.NewRGBA(bounds)
	fill(canvas, 0, 0, bounds.Dx(), bounds.Dy(), background)
	draw.Draw(canvas, bounds, source, bounds.Min, draw.Over)
	return canvas
}

func RenderSprite(kind string, state map[string]any) image.Image {
	canvas := image.NewRGBA(image.Rect(0, 0, 16, 16))
	switch kind {
	case "player":
		drawPlayer(canvas)
	case "staff":
		drawStaff(canvas)
	case "chest":
		opened, _ := state["opened"].(bool)
		drawChest(canvas, opened)
	case "gate":
		opened, _ := state["opened"].(bool)
		drawGate(canvas, opened)
	case "npc":
		drawNPC(canvas)
	case "crystal":
		active, _ := state["active"].(bool)
		drawCrystal(canvas, active)
	}
	high := scale2x(canvas).(*image.RGBA)
	enhanceSprite(high, kind)
	return high
}

func RenderAnimation(kind string) image.Image {
	canvas := image.NewRGBA(image.Rect(0, 0, 384, 32))
	for frame := 0; frame < 12; frame++ {
		if len(kind) >= 12 && kind[:12] == "effect-break" {
			drawBreakFrame(canvas, frame*32+8, frame)
			continue
		}
		draw.Draw(canvas, image.Rect(frame*32, 0, frame*32+32, 32), RenderSprite(kind, map[string]any{}), image.Point{}, draw.Src)
		if frame >= 3 && frame <= 7 {
			fill(canvas, frame*32+4, 4, 24, 4, color.RGBA{238, 255, 202, 255})
		}
		if frame >= 5 {
			pixel(canvas, frame*32+10, 14, color.RGBA{255, 255, 255, 255})
			pixel(canvas, frame*32+22, 22, color.RGBA{255, 255, 255, 255})
		}
		if frame >= 9 {
			fill(canvas, frame*32, 28, 32, 4, color.RGBA{96, 74, 56, 255})
		}
	}
	return canvas
}

// RenderMatrixAnimation derives every frame from the generated object matrix.
// It does not use any of the legacy hand-authored animation sprites.
func RenderMatrixAnimation(asset MatrixAsset, action string) (image.Image, error) {
	if err := validateMatrixAsset(asset, asset.Width, asset.Height); err != nil {
		return nil, err
	}
	frameCount := 12
	canvas := image.NewRGBA(image.Rect(0, 0, asset.Width*frameCount, asset.Height))
	source, err := RenderMatrixAsset(asset)
	if err != nil {
		return nil, err
	}
	for frame := 0; frame < frameCount; frame++ {
		offsetY := 0
		if action == "interact" && frame >= 3 && frame <= 7 {
			offsetY = -1
		}
		if action == "break" {
			drawDispersedFrame(canvas, source, frame*asset.Width, frame)
			continue
		}
		draw.Draw(canvas, image.Rect(frame*asset.Width, offsetY, (frame+1)*asset.Width, asset.Height+offsetY), source, image.Point{}, draw.Over)
	}
	return canvas, nil
}

// RenderRasterAnimation derives animation frames from any backend-generated
// bitmap, including a crop of the ComfyUI scene background.
func RenderRasterAnimation(source image.Image, action string) image.Image {
	bounds := source.Bounds()
	frameCount := 12
	canvas := image.NewRGBA(image.Rect(0, 0, bounds.Dx()*frameCount, bounds.Dy()))
	for frame := 0; frame < frameCount; frame++ {
		if action == "break" {
			drawDispersedFrame(canvas, source, frame*bounds.Dx(), frame)
			continue
		}
		draw.Draw(canvas, image.Rect(frame*bounds.Dx(), 0, (frame+1)*bounds.Dx(), bounds.Dy()), source, bounds.Min, draw.Over)
	}
	return canvas
}

func drawDispersedFrame(canvas *image.RGBA, source image.Image, frameX, frame int) {
	bounds := source.Bounds()
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			c := color.RGBAModel.Convert(source.At(x, y)).(color.RGBA)
			if c.A == 0 || ((x*7+y*11+frame*3)%13 < frame/2) {
				continue
			}
			dx := (x + frameX) + ((x+y)%3-1)*(frame/3)
			dy := y + ((x*5+y*3)%3)*(frame/4)
			pixel(canvas, dx, dy, c)
		}
	}
}

func scale2x(source image.Image) image.Image {
	bounds := source.Bounds()
	target := image.NewRGBA(image.Rect(0, 0, bounds.Dx()*2, bounds.Dy()*2))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			c := source.At(bounds.Min.X+x, bounds.Min.Y+y)
			target.Set(x*2, y*2, c)
			target.Set(x*2+1, y*2, c)
			target.Set(x*2, y*2+1, c)
			target.Set(x*2+1, y*2+1, c)
		}
	}
	return target
}

func addTileDetail(canvas *image.RGBA, x, y int, kind string, tx, ty int) {
	switch kind {
	case "grass":
		for n := 0; n < 5; n++ {
			pixel(canvas, x+4+n*5, y+5+(tx+ty+n*3)%19, color.RGBA{138, 190, 84, 255})
		}
	case "path":
		fill(canvas, x+3, y+3, 2, 2, color.RGBA{220, 190, 125, 255})
		fill(canvas, x+23, y+21, 3, 2, color.RGBA{126, 96, 62, 255})
	case "water":
		line(canvas, x+5, y+9, 15, color.RGBA{117, 210, 207, 255})
		line(canvas, x+13, y+22, 12, color.RGBA{72, 160, 180, 255})
	case "tree":
		fill(canvas, x+7, y+5, 18, 4, color.RGBA{74, 145, 78, 255})
		pixel(canvas, x+22, y+12, color.RGBA{159, 207, 91, 255})
	}
}

func enhanceSprite(canvas *image.RGBA, kind string) {
	switch kind {
	case "player":
		fill(canvas, 8, 15, 16, 3, color.RGBA{255, 173, 189, 255})
		pixel(canvas, 12, 9, color.RGBA{80, 50, 58, 255})
		pixel(canvas, 20, 9, color.RGBA{80, 50, 58, 255})
	case "staff":
		fill(canvas, 13, 5, 6, 3, color.RGBA{216, 255, 255, 255})
		pixel(canvas, 16, 1, color.RGBA{255, 255, 255, 255})
	case "chest":
		fill(canvas, 12, 16, 8, 3, color.RGBA{255, 220, 95, 255})
	case "gate":
		fill(canvas, 5, 5, 3, 19, color.RGBA{221, 239, 222, 255})
	case "npc":
		fill(canvas, 8, 15, 16, 3, color.RGBA{116, 185, 143, 255})
	}
}

func drawTile(canvas *image.RGBA, tx, ty int, kind string) {
	// The source map is authored on a 16px grid then expanded to 32px for display.
	x, y := tx*16, ty*16
	switch kind {
	case "rubble":
		fill(canvas, x, y, 16, 16, color.RGBA{76, 79, 72, 255})
		fill(canvas, x+3, y+7, 5, 4, color.RGBA{154, 145, 125, 255})
		fill(canvas, x+10, y+9, 3, 3, color.RGBA{122, 113, 98, 255})
	case "rubble-tree":
		fill(canvas, x, y, 16, 16, color.RGBA{54, 120, 70, 255})
		fill(canvas, x+2, y+9, 12, 4, color.RGBA{102, 66, 37, 255})
	case "rubble-water":
		fill(canvas, x, y, 16, 16, color.RGBA{30, 104, 131, 255})
		fill(canvas, x+3, y+10, 10, 3, color.RGBA{173, 174, 154, 255})
	case "rubble-path":
		fill(canvas, x, y, 16, 16, color.RGBA{121, 101, 72, 255})
		fill(canvas, x+3, y+7, 5, 4, color.RGBA{179, 160, 126, 255})
	case "rubble-grass":
		fill(canvas, x, y, 16, 16, color.RGBA{54, 120, 70, 255})
		fill(canvas, x+4, y+9, 8, 3, color.RGBA{107, 88, 66, 255})
	case "water":
		fill(canvas, x, y, 16, 16, color.RGBA{30, 104, 131, 255})
		for offset := 2; offset < 16; offset += 5 {
			line(canvas, x+offset, y+4+(tx+ty)%3, 5, color.RGBA{102, 190, 193, 255})
		}
	case "tree":
		fill(canvas, x, y, 16, 16, color.RGBA{25, 67, 48, 255})
		fill(canvas, x+2, y+1, 12, 10, color.RGBA{43, 112, 70, 255})
		fill(canvas, x+5, y+10, 5, 6, color.RGBA{91, 61, 37, 255})
		pixel(canvas, x+4, y+4, color.RGBA{91, 153, 82, 255})
	case "path":
		fill(canvas, x, y, 16, 16, color.RGBA{166, 132, 78, 255})
		for offset := 2; offset < 15; offset += 6 {
			pixel(canvas, x+offset, y+(offset*3+tx)%12+2, color.RGBA{213, 181, 112, 255})
		}
	default:
		fill(canvas, x, y, 16, 16, color.RGBA{54, 120, 70, 255})
		for offset := 2; offset < 16; offset += 5 {
			pixel(canvas, x+offset, y+(offset+tx*2+ty)%14+1, color.RGBA{113, 169, 76, 255})
		}
		if (tx*3+ty*5)%11 == 0 {
			pixel(canvas, x+7, y+7, color.RGBA{240, 209, 104, 255})
		}
	}
}

func tileKind(position Position, snapshot Snapshot) string {
	if changed, ok := snapshot.ChangedTiles[tileKey(position)]; ok {
		return changed
	}
	if (position.X >= 11 && position.X <= 12) || (position.X >= 19 && position.X <= 20 && position.Y > 11) {
		return "water"
	}
	if (position.X < 2 || position.X > 27) && position.Y%3 != 1 {
		return "tree"
	}
	if (position.Y < 2 || position.Y > 18) && position.X%4 != 2 {
		return "tree"
	}
	if (position.X == 14 || position.X == 15) && position.Y > 2 && position.Y < 18 {
		return "path"
	}
	if position.Y == 10 && position.X > 4 && position.X < 25 {
		return "path"
	}
	if position.X > 18 && position.Y > 3 && position.Y < 8 {
		return "path"
	}
	if position.X < 10 && position.Y > 13 {
		return "path"
	}
	return "grass"
}

func drawPlayer(canvas *image.RGBA) {
	fill(canvas, 5, 3, 6, 5, color.RGBA{244, 211, 190, 255})
	fill(canvas, 4, 7, 8, 6, color.RGBA{222, 112, 142, 255})
	fill(canvas, 3, 13, 4, 2, color.RGBA{38, 49, 76, 255})
	fill(canvas, 9, 13, 4, 2, color.RGBA{38, 49, 76, 255})
}
func drawStaff(canvas *image.RGBA) {
	fill(canvas, 7, 3, 2, 11, color.RGBA{112, 75, 48, 255})
	fill(canvas, 4, 1, 8, 4, color.RGBA{111, 225, 229, 255})
	pixel(canvas, 7, 0, color.RGBA{239, 255, 255, 255})
	pixel(canvas, 4, 2, color.RGBA{219, 255, 255, 255})
}
func drawChest(canvas *image.RGBA, opened bool) {
	fill(canvas, 2, 7, 12, 7, color.RGBA{133, 79, 39, 255})
	fill(canvas, 2, 8, 12, 2, color.RGBA{225, 171, 59, 255})
	if opened {
		fill(canvas, 3, 2, 10, 4, color.RGBA{162, 101, 45, 255})
	} else {
		fill(canvas, 3, 4, 10, 4, color.RGBA{170, 102, 48, 255})
	}
}
func drawGate(canvas *image.RGBA, opened bool) {
	if opened {
		fill(canvas, 2, 2, 3, 12, color.RGBA{171, 193, 186, 255})
		fill(canvas, 11, 2, 3, 12, color.RGBA{171, 193, 186, 255})
		return
	}
	fill(canvas, 2, 2, 12, 12, color.RGBA{171, 193, 186, 255})
	fill(canvas, 5, 5, 6, 9, color.RGBA{51, 81, 82, 255})
}
func drawNPC(canvas *image.RGBA) {
	fill(canvas, 5, 2, 6, 5, color.RGBA{218, 177, 143, 255})
	fill(canvas, 4, 7, 8, 6, color.RGBA{70, 137, 111, 255})
	fill(canvas, 3, 13, 4, 2, color.RGBA{41, 58, 70, 255})
	fill(canvas, 9, 13, 4, 2, color.RGBA{41, 58, 70, 255})
}
func drawCrystal(canvas *image.RGBA, active bool) {
	c := color.RGBA{92, 201, 211, 255}
	if !active {
		c = color.RGBA{81, 106, 130, 255}
	}
	fill(canvas, 7, 1, 2, 2, c)
	fill(canvas, 5, 3, 6, 8, c)
	fill(canvas, 6, 11, 4, 3, c)
	pixel(canvas, 7, 5, color.RGBA{238, 255, 255, 255})
}
func drawBreakFrame(canvas *image.RGBA, x, frame int) {
	count := 2 + frame
	if count > 10 {
		count = 10
	}
	for index := 0; index < count; index++ {
		pixel(canvas, x+3+index*2, 4+((index+frame)%5)*2, color.RGBA{239, 219, 150, 255})
	}
}
func fill(canvas *image.RGBA, x, y, width, height int, c color.RGBA) {
	for py := y; py < y+height; py++ {
		for px := x; px < x+width; px++ {
			pixel(canvas, px, py, c)
		}
	}
}
func line(canvas *image.RGBA, x, y, width int, c color.RGBA) {
	for px := x; px < x+width; px++ {
		pixel(canvas, px, y, c)
	}
}
func pixel(canvas *image.RGBA, x, y int, c color.RGBA) {
	if image.Pt(x, y).In(canvas.Bounds()) {
		canvas.SetRGBA(x, y, c)
	}
}
