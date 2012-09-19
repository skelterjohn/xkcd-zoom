package main

import (
	"fmt"
	"github.com/skelterjohn/go.wde"
	_ "github.com/skelterjohn/go.wde/init"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"time"
)

var tiles = map[int]map[int]image.Image{}

func setTile(x, y int, tile image.Image) {
	xtiles := tiles[x]
	if xtiles == nil {
		xtiles = map[int]image.Image{}
		tiles[x] = xtiles
	}
	xtiles[y] = tile
	log.Printf("(%d, %d) is %v", x, y, tile.Bounds())
}

func getTile(x, y int) (tile image.Image, ok bool) {
	if xtiles, ok := tiles[x]; ok {
		tile, ok = xtiles[y]
	}
	return
}

var coordRegexp = regexp.MustCompile(`(\d+)(.)(\d+)(.)`)

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
			case "s":
				y *= -1
				y += 1
			default:
				log.Fatalf("Couldn't parse %q", name)
			}
			switch parts[4] {
			case "e":
			case "w":
				x *= -1
				x += 1
			default:
				log.Fatalf("Couldn't parse %q", name)
			}

			imageFile, err := os.Open(file)
			if err != nil {
				log.Fatalf(err.Error())
			}

			log.Printf("Loading %q\n", file)

			tile, err := png.Decode(imageFile)
			if err != nil {
				log.Fatalf(fmt.Sprintf("trying to decode %q: %v", err))
			}

			setTile(x, y, tile)
		}
	}
}

// (drawx, drawy) is the world point drawn in the center of the window
// 1 drawx or drawy unit represents 1 pixel in the supplied image
var drawx, drawy float64

// scale is how many drawx and drawy units there are per screen pixel
// the higher scale is, the more zoomed out the view is
var scale float64 = 1

const imageWidth, imageHeight = 2048, 2048

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

		done := make(chan bool)

		go func() {
		loop:
			for ei := range events {
				runtime.Gosched()
				switch e := ei.(type) {
				case wde.MouseDraggedEvent:
				case wde.KeyTypedEvent:
					fmt.Println("typed", e.Key, e.Glyph, e.Chord)
					if e.Key == wde.KeyEscape {
						break loop
					}
				case wde.CloseEvent:
					fmt.Println("close")
					break loop
				case wde.ResizeEvent:
					fmt.Println("resize", e.Width, e.Height)
				}
			}
			dw.Close()
			done <- true
			fmt.Println("end of events")
		}()

		for i := 0; ; i++ {
			width, height := dw.Size()
			_, _ = width, height
			s := dw.Screen()
			_ = s

			dw.FlushImage()
			select {
			case <-time.After(time.Second):
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
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <image directory>", os.Args[0])
	}
	imageDir := os.Args[1]

	loadImages(imageDir)

	go window()
	wde.Run()
}
