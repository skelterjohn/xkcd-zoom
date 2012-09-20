package main

import (
	"code.google.com/p/appengine-go/example/moustachio/resize"
	"fmt"
	"github.com/skelterjohn/go.wde"
	_ "github.com/skelterjohn/go.wde/init"

	"github.com/BurntSushi/xgbutil/xgraphics"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"time"
)

var tiles = map[image.Point]chan image.Image{}

var tileFiles = map[image.Point]string{}

func setTileFile(x, y int, file string) {
	tileFiles[image.Point{x, y}] = file
}

func makeTileChan(file string) (tilechan chan image.Image) {
	tilechan = make(chan image.Image, 1)
	go func() {
		log.Printf("Loading %q\n", file)
		fi, err := os.Open(file)
		if err != nil {
			log.Fatal(err)
		}
		tile, err := png.Decode(fi)
		if err != nil {
			log.Fatal(err)
		}
		tilechan <- tile
	}()
	return
}

func getTile(x, y int) (tile image.Image, ok bool) {
	tilechan, ok := tiles[image.Point{x, y}]
	if !ok {
		var file string
		file, ok = tileFiles[image.Point{x, y}]
		if !ok {
			return
		}
		tilechan = makeTileChan(file)
		tiles[image.Point{x, y}] = tilechan
	}
	select {
	case tile = <-tilechan:
		tilechan <- tile
		ok = true
	default:
		ok = false
	}
	return
}

var scaledTiles = map[float64]map[int]map[int]chan image.Image{}

func getScaledTile(x, y int, scale float64) (scaledTile image.Image, ok bool) {
	unscaled, ok := getTile(x, y)
	if !ok {
		return
	}
	stiles, ok := scaledTiles[scale]
	if !ok {
		stiles = map[int]map[int]chan image.Image{}
		scaledTiles[scale] = stiles
	}
	sxtiles, ok := stiles[x]
	if !ok {
		sxtiles = map[int]chan image.Image{}
		stiles[x] = sxtiles
	}
	scaledTileChan, ok := sxtiles[y]
	if !ok {
		bounds := unscaled.Bounds()
		s := bounds.Size()
		scaledTileChan = make(chan image.Image, 1)
		go func() {
			scaledTileChan <- resize.Resize(unscaled, unscaled.Bounds(), int(.5+float64(s.X)/scale), int(.5+float64(s.Y)/scale))
		}()
		sxtiles[y] = scaledTileChan
	}
	select {
	case scaledTile = <-scaledTileChan:
		scaledTileChan <- scaledTile
	default:
		ok = false
	}
	return
}

func loadImages(imageDir string) {

	log.Printf("scanning %q", imageDir)

	d, err := os.Open(imageDir)
	if err != nil {
		log.Fatal(err)
	}

	files, err := d.Readdirnames(-1)
	if err != nil {
		log.Fatal(err)
	}

	coordRegexp := regexp.MustCompile(`(\d+)(.)(\d+)(.)`)

	for _, file := range files {
		file = filepath.Join(imageDir, file)
		if filepath.Ext(file) == ".png" {
			name := filepath.Base(file)
			name = name[:len(name)-4]

			parts := coordRegexp.FindStringSubmatch(name)
			if len(parts) != 5 {
				log.Fatalf("Couldn't parse %q", name)
			}

			y, err := strconv.Atoi(parts[1])
			if err != nil {
				log.Fatalf(err.Error())
			}
			x, err := strconv.Atoi(parts[3])
			if err != nil {
				log.Fatalf(err.Error())
			}

			switch parts[2] {
			case "n":
				y -= 1
			case "s":
				y *= -1
			default:
				log.Fatalf("Couldn't parse %q", name)
			}
			switch parts[4] {
			case "e":
				x -= 1
			case "w":
				x *= -1
			default:
				log.Fatalf("Couldn't parse %q", name)
			}

			if x < -1 || x > 1 || y < -1 || y > 1 {
				//continue
			}

			setTileFile(x, y, file)
		}
	}
}

func mapToScreen(width, height int, wx, wy float64) (dx, dy int) {
	wx += drawx
	wy += drawy
	wx /= scale
	wy /= scale
	dx = int(wx) // + width/2
	dy = int(wy) // - height/2
	return
}

// (drawx, drawy) is the world point drawn in the center of the window
// 1 drawx or drawy unit represents 1 pixel in the supplied image
var drawx float64 = 120
var drawy float64 = 1000

// scale is how many drawx and drawy units there are per screen pixel
// the higher scale is, the more zoomed out the view is
var scale float64 = 1

const imageWidth, imageHeight = 2048, 2048

func copyToXGraphicsImage(xs *xgraphics.Image, buffer *image.RGBA) {
	//Pix[(y-Rect.Min.Y)*Stride + (x-Rect.Min.X)*4]
	xdata := xs.Pix
	bdata := buffer.Pix
	if xs.Bounds() == buffer.Bounds() {
		copy(xdata, bdata)
	} else {

		xb := xs.Bounds()
		bb := buffer.Bounds()

		miny := xb.Min.Y
		if miny < bb.Min.Y {
			miny = bb.Min.Y
		}
		maxy := xb.Max.Y
		if maxy > bb.Max.Y {
			maxy = bb.Max.Y
		}

		xRowLen := (xb.Max.X - xb.Min.X)
		bRowLen := (bb.Max.X - bb.Min.X)
		rowLen := xRowLen
		if bRowLen < rowLen {
			rowLen = bRowLen
		}

		for y := miny; y < maxy; y++ {
			xstart := (y-xb.Min.Y)*xs.Stride - xb.Min.X*4
			bstart := (y-bb.Min.Y)*buffer.Stride - bb.Min.X*4
			xrow := xdata[xstart : xstart+rowLen*4]
			brow := bdata[bstart : bstart+rowLen*4]
			copy(xrow, brow)
		}
	}
	// xgraphics.Image is BGRA, not RGBA, so swap some bits
	for i := 0; i < len(xdata)/4; i++ {
		index := i * 4
		xdata[index], xdata[index+2] = xdata[index+2], xdata[index]
	}
}

func window() {
	var wg sync.WaitGroup

	size := 500

	wg.Add(1)

	go func() {
		dw, err := wde.NewWindow(size, size)
		if err != nil {
			fmt.Println(err)
			return
		}
		dw.SetTitle("xkcd-zoom")
		dw.SetSize(size, size)
		dw.Show()

		events := dw.EventChan()

		const (
			Draw         = 1
			Redraw       = 2
			ScaleInPlace = 3
		)

		redraw := make(chan int, 1)

		redraw <- Draw

		done := make(chan bool)

		go func() {
			for {
				time.Sleep(time.Second)
				redraw <- Redraw
			}
		}()

		go func() {
		loop:
			for ei := range events {
				redrawType := 0
				runtime.Gosched()
				switch e := ei.(type) {
				case wde.MouseDownEvent:
				case wde.MouseDraggedEvent:
					switch e.Which {
					case wde.LeftButton:
						changedx := e.Where.X - e.From.X
						changedy := e.Where.Y - e.From.Y
						drawx += float64(changedx) * scale
						drawy += float64(-changedy) * scale
						redrawType = Draw
					case wde.RightButton:
						changedy := e.From.Y - e.Where.Y
						mouseScale := float64(changedy) / 100
						scale *= math.Pow(2, mouseScale)
						redrawType = ScaleInPlace
					}
				case wde.MouseUpEvent:
					if e.Which == wde.RightButton {
						scaledTiles = map[float64]map[int]map[int]chan image.Image{}
						redrawType = Draw
					}
				case wde.KeyTypedEvent:
					if e.Key == wde.KeyEscape {
						break loop
					}
				case wde.CloseEvent:
					break loop
				case wde.ResizeEvent:
					redrawType = Draw
				}
				if redrawType != 0 {
					select {
					case redraw <- redrawType:
					default:
					}
				}
			}
			dw.Close()
			done <- true
		}()

		var greyBack, screenBuffer *image.RGBA
		var grey = color.RGBA{155, 155, 155, 255}
		var bufferScale = scale
		var lastRedraw int
		for {
			select {
			case redrawType := <-redraw:
				if lastRedraw == ScaleInPlace && redrawType == Redraw {
					continue
				}
				lastRedraw = redrawType
				s := dw.Screen()
				if redrawType == Draw || redrawType == Redraw {
					width, height := dw.Size()

					tilesh := int(float64(width)/(float64(imageWidth)/scale) + 1)
					tilesv := int(float64(height)/(float64(imageHeight)/scale) + 1)

					tilecx := drawx / imageWidth
					tilecy := drawy / imageHeight

					tileMinX := int(-tilecx) - tilesh - 1
					tileMinY := int(-tilecy) - tilesv - 1
					tileMaxX := int(-tilecx) + tilesh + 1
					tileMaxY := int(-tilecy) + tilesv + 1

					if greyBack == nil || s.Bounds() != greyBack.Bounds() {
						bounds := s.Bounds()
						greyBack = image.NewRGBA(bounds)
						for x := bounds.Min.X; x <= bounds.Max.X; x++ {
							for y := bounds.Min.Y; y <= bounds.Max.Y; y++ {
								greyBack.SetRGBA(x, y, grey)
							}
						}
						screenBuffer = image.NewRGBA(bounds)
					}

					draw.Draw(screenBuffer, screenBuffer.Bounds(), greyBack, image.Point{0, 0}, draw.Src)

					for tilex := tileMinX; tilex <= tileMaxX; tilex++ {
						for tiley := tileMinY; tiley <= tileMaxY; tiley++ {
							scaledTile, ok := getScaledTile(tilex, tiley, scale)
							if !ok || scaledTile == nil {
								continue
							}

							dx, dy := mapToScreen(width, height, float64(tilex)*imageWidth, float64(tiley)*imageHeight)

							drawRect := scaledTile.Bounds()
							drawRect.Min.X += dx
							drawRect.Min.Y -= dy
							drawRect.Max.X += dx
							drawRect.Max.Y -= dy

							draw.Draw(screenBuffer, drawRect, scaledTile, image.Point{0, 0}, draw.Src)
						}
					}

					bufferScale = scale
					if xs, ok := s.(*xgraphics.Image); ok {
						copyToXGraphicsImage(xs, screenBuffer)
					} else {
						draw.Draw(s, s.Bounds(), screenBuffer, image.Point{0, 0}, draw.Src)
					}
				} else if redrawType == ScaleInPlace {
					scaleFactor := scale / bufferScale
					bufBounds := screenBuffer.Bounds()
					width := bufBounds.Max.X - bufBounds.Min.X
					height := bufBounds.Max.Y - bufBounds.Min.Y
					if scaleFactor < 1 {
						widthDiff := width - int(float64(width)*scaleFactor)
						heightDiff := height - int(float64(height)*scaleFactor)
						//bufBounds.Min.X += widthDiff / 2
						bufBounds.Max.X -= widthDiff
						//bufBounds.Min.Y += heightDiff / 2
						bufBounds.Max.Y -= heightDiff
						subBuffer := screenBuffer.SubImage(bufBounds)
						scaledBuffer := resize.Resize(subBuffer, subBuffer.Bounds(), width, height).(*image.RGBA)

						if xs, ok := s.(*xgraphics.Image); ok {
							copyToXGraphicsImage(xs, scaledBuffer)
						} else {
							draw.Draw(s, s.Bounds(), scaledBuffer, image.Point{0, 0}, draw.Src)
						}
					}
					if scaleFactor > 1 {
						scaleFactor = 1 / scaleFactor
						newWidth := int(float64(width) * scaleFactor)
						newHeight := int(float64(height) * scaleFactor)
						scaledBuffer := resize.Resize(screenBuffer, screenBuffer.Bounds(), newWidth, newHeight).(*image.RGBA)

						if xs, ok := s.(*xgraphics.Image); ok {
							copyToXGraphicsImage(xs, scaledBuffer)
						} else {
							draw.Draw(s, s.Bounds(), scaledBuffer, image.Point{0, 0}, draw.Src)
						}
					}
				}
				dw.FlushImage()
			case <-done:
				wg.Done()
				return
			}
		}
	}()

	wg.Wait()
	wde.Stop()
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <image directory>", os.Args[0])
	}
	imageDir := os.Args[1]

	loadImages(imageDir)

	go window()
	wde.Run()
}
