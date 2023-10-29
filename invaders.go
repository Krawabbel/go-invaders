package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/Krawabbel/go-8080/intel8080"

	"github.com/veandco/go-sdl2/mix"
	"github.com/veandco/go-sdl2/sdl"
)

const (
	CYCLES_PER_SECOND = 2e6
	FRAMES_PER_SECOND = 60
	CYCLES_PER_FRAME  = CYCLES_PER_SECOND / FRAMES_PER_SECOND
	LINES_PER_FRAME   = 224
	CYCLES_PER_LINE   = CYCLES_PER_FRAME / LINES_PER_FRAME
)

const (
	SOUND_SHOOT = iota
	SOUND_INVADER_DEATH
	SOUND_EXPLOSION
	SOUND_FLEET_MOVEMENT_1
	SOUND_FLEET_MOVEMENT_2
	SOUND_FLEET_MOVEMENT_3
	SOUND_FLEET_MOVEMENT_4
	SOUND_UFO_PASSING
	SOUND_UFO_HIT
	SOUND_SIZE
)

var MODE_MONOCHROME = true
var MODE_COCKTAIL = false

func main() {
	panic(Run(os.Args[1]))
}

func Run(data_path string) error {

	eventc := make(chan sdl.Event)
	defer close(eventc)

	screenc := make(chan [][]bool)
	defer close(screenc)

	port_0_channel := make(chan byte)
	defer close(port_0_channel)

	port_1_channel := make(chan byte)
	defer close(port_1_channel)

	port_2_channel := make(chan byte)
	defer close(port_2_channel)

	soundc := make(chan int)
	defer close(soundc)

	go inputHandler(port_0_channel, port_1_channel, port_2_channel, eventc)

	go sdlDriver(data_path, eventc, screenc, soundc)

	emulate(data_path, screenc, soundc, port_0_channel, port_1_channel, port_2_channel)

	return nil // unreachable
}

func inputHandler(port_0_channel, port_1_channel, port_2_channel chan<- byte, eventc <-chan sdl.Event) {

	port_0_signal := byte(0b00001110)
	port_1_signal := byte(0b00001000)
	port_2_signal := byte(0)

	for {
		select {
		case port_0_channel <- port_0_signal:
			// fmt.Println("[Input Handler] Signal", port_0_signal, "sent on port 0")

		case port_1_channel <- port_1_signal:
			// fmt.Println("[Input Handler] Signal", port_1_signal, "sent on port 1")

		case port_2_channel <- port_2_signal:
			// fmt.Println("[Input Handler] Signal", port_2_signal, "sent on port 2")

		case event := <-eventc:

			switch t := event.(type) {
			case *sdl.QuitEvent:
				os.Exit(0)

			case *sdl.KeyboardEvent:
				keyCode := t.Keysym.Sym
				switch keyCode {
				case sdl.K_c: // coin
					set(&port_1_signal, 0, t.State == sdl.PRESSED)
				case sdl.K_t: // tilt the machine
					set(&port_2_signal, 2, t.State == sdl.PRESSED)
				case sdl.K_b: // dipswitch bonus life
					set(&port_2_signal, 3, t.State == sdl.PRESSED)
				case sdl.K_i: // dipswitch coin info
					set(&port_2_signal, 7, t.State == sdl.PRESSED)

				case sdl.K_1: // start game in one-player mode
					set(&port_1_signal, 2, t.State == sdl.PRESSED)
				case sdl.K_2: // start game in two-player mode
					set(&port_1_signal, 1, t.State == sdl.PRESSED)

				case sdl.K_LEFT: // player 1: move left
					set(&port_1_signal, 5, t.State == sdl.PRESSED)
				case sdl.K_RIGHT: // player 1: move right
					set(&port_1_signal, 6, t.State == sdl.PRESSED)
				case sdl.K_COMMA, sdl.K_SPACE: // player 1: shoot
					set(&port_1_signal, 4, t.State == sdl.PRESSED)

				case sdl.K_y: // player 2: move left
					set(&port_2_signal, 5, t.State == sdl.PRESSED)
				case sdl.K_x: // player 2: move right
					set(&port_2_signal, 6, t.State == sdl.PRESSED)
				case sdl.K_v: // player 2: shoot
					set(&port_2_signal, 4, t.State == sdl.PRESSED)

				case sdl.K_F9: // toggle between black and white/coloured mode
					if t.State == sdl.PRESSED && t.Repeat == 0 {
						MODE_MONOCHROME = !MODE_MONOCHROME
					}
					// fmt.Println("[Input Handler]", string(keyCode), "saved")
				}
			default: // ignore
			}
		}
	}
}

func set(bits *byte, pos uint, flag bool) {
	if flag {
		*bits |= (1 << pos)
	} else {
		*bits &= ^(1 << pos)
	}
}

func playSoundEffect(soundc chan<- int, sounds_playing []bool, sound_id int, sound_flag bool) []bool {

	if sounds_playing[sound_id] && !sound_flag {
		sounds_playing[sound_id] = false
	}

	if !sounds_playing[sound_id] && sound_flag {
		sounds_playing[sound_id] = true
		soundc <- sound_id
	}

	return sounds_playing
}

func emulate(data_path string, screenc chan<- [][]bool, soundc chan<- int, port_0_channel, port_1_channel, port_2_channel <-chan byte) {

	bus := newArcadeBus(data_path)

	i8080 := intel8080.NewIntel8080(bus, 0)

	sounds_playing := make([]bool, SOUND_SIZE)

	i8080.OutputDevice[0x03] = func(b byte) {
		// bit 0=UFO (repeats)        SX0 0.raw
		// bit 1=Shot                 SX1 1.raw
		// bit 2=Flash (player die)   SX2 2.raw
		// bit 3=Invader die          SX3 3.raw
		// bit 4=Extended play        SX4
		// bit 5= AMP enable          SX5
		// bit 6= NC (not wired)
		// bit 7= NC (not wired)

		sounds_playing = playSoundEffect(soundc, sounds_playing, SOUND_UFO_PASSING, b&(1<<0) > 0)

		sounds_playing = playSoundEffect(soundc, sounds_playing, SOUND_SHOOT, b&(1<<1) > 0)

		sounds_playing = playSoundEffect(soundc, sounds_playing, SOUND_INVADER_DEATH, b&(1<<3) > 0)

		sounds_playing = playSoundEffect(soundc, sounds_playing, SOUND_EXPLOSION, b&(1<<2) > 0)

		if b&(1<<4) > 0 {
			println("[???] Extended play")
		}

		if b&(1<<5) > 0 {
			// println("[???] AMP enable")
		}

	}

	i8080.OutputDevice[0x05] = func(b byte) {
		// bit 0=Fleet movement 1     SX6 4.raw
		// bit 1=Fleet movement 2     SX7 5.raw
		// bit 2=Fleet movement 3     SX8 6.raw
		// bit 3=Fleet movement 4     SX9 7.raw
		// bit 4=UFO Hit              SX10 8.raw
		// bit 5= NC (Cocktail mode control ... to flip screen)
		// bit 6= NC (not wired)
		// bit 7= NC (not wired)

		for i := 0; i < 4; i++ {
			sounds_playing = playSoundEffect(soundc, sounds_playing, SOUND_FLEET_MOVEMENT_1+i, b&(1<<i) > 0)
		}

		sounds_playing = playSoundEffect(soundc, sounds_playing, SOUND_UFO_HIT, b&(1<<4) > 0)

		if b&(1<<5) > 0 {
			MODE_COCKTAIL = !MODE_COCKTAIL
		}

	}

	i8080.OutputDevice[0x06] = func(b byte) {} // ignored: debug port?

	i8080.InputDevice[0x00] = func() byte { return <-port_0_channel }
	i8080.InputDevice[0x01] = func() byte { return <-port_1_channel }
	i8080.InputDevice[0x02] = func() byte { return <-port_2_channel }

	bitshift_register := uint16(0x00)
	bitshift_offset := byte(0x00)

	i8080.InputDevice[0x03] = func() byte {
		return byte(bitshift_register >> (8 - bitshift_offset))
	}

	i8080.OutputDevice[0x02] = func(b byte) {
		bitshift_offset = b & 0b111
	}

	i8080.OutputDevice[0x04] = func(b byte) {
		bitshift_register = bitshift_register>>8 | uint16(b)<<8
	}

	frame_ticker := time.NewTicker(time.Second / 60)

	debug_ticker := time.NewTicker(time.Second)

	tStart := time.Now()
	for {

		select {

		case _ = <-frame_ticker.C:

			for i := 0; float64(i) < 96*CYCLES_PER_LINE; i++ {
				i8080.Step()
			}

			i8080.Interrupt(0xCF)

			for i := 0; float64(i) < (LINES_PER_FRAME-96)*CYCLES_PER_LINE; i++ {
				i8080.Step()
			}

			i8080.Interrupt(0xD7)

			screenc <- buffer_frame(bus)

		case _ = <-debug_ticker.C:
			print(i8080.DebugSpeed(time.Since(tStart)))

		}
	}

}

type arcadeBus struct {
	memory []byte
}

func (bus arcadeBus) Read(addr uint16) byte {
	switch {
	case addr < 0x4000:
		return bus.memory[addr]
	case 0x4000 >= addr && addr < 0x6000:
		return bus.memory[addr-0x2000]
	default:
		return 0
	}
}

func (bus arcadeBus) Write(addr uint16, val byte) {
	if addr >= 0x2000 && addr < 0x4000 {
		bus.memory[addr] = val
	}
}

func newArcadeBus(path string) intel8080.Bus {

	memory := make([]byte, 0x4000)

	invaders_h, err := intel8080.Load(filepath.Join(path, "invaders.h"))
	if err != nil {
		panic(err)
	}
	copy(memory[0x0000:], invaders_h)

	invaders_g, err := intel8080.Load(filepath.Join(path, "invaders.g"))
	if err != nil {
		panic(err)
	}
	copy(memory[0x0800:], invaders_g)

	invaders_f, err := intel8080.Load(filepath.Join(path, "invaders.f"))
	if err != nil {
		panic(err)
	}
	copy(memory[0x1000:], invaders_f)

	invaders_e, err := intel8080.Load(filepath.Join(path, "invaders.e"))
	if err != nil {
		panic(err)
	}
	copy(memory[0x1800:], invaders_e)

	bus := new(arcadeBus)
	bus.memory = memory

	return bus
}

func buffer_frame(bus intel8080.Bus) [][]bool {

	screen := make([][]bool, 224)
	for row := range screen {
		screen[row] = make([]bool, 256)
	}

	row := 0
	col := 0

	for addr := uint16(0x2400); addr < 0x4000; addr++ {
		val := bus.Read(addr)
		for bitpos := 0; bitpos < 8; bitpos++ {
			screen[row][col] = val&(1<<bitpos) > 0
			col++
			if col == 256 {
				col = 0
				row++
			}
		}
	}

	return screen
}

func makeChunk(data_path, name string) *mix.Chunk {
	chunk, err := mix.LoadWAV(filepath.Join(data_path, name))
	if err != nil {
		panic(err)
	}
	return chunk
}

func sdlDriver(data_path string, eventc chan<- sdl.Event, screenc <-chan [][]bool, soundc <-chan int) {

	if err := sdl.Init(sdl.INIT_EVERYTHING); err != nil {
		panic(err)
	}
	defer sdl.Quit()

	window, err := sdl.CreateWindow("Go Invaders!", sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED, 224, 256, sdl.WINDOW_SHOWN)
	if err != nil {
		panic(err)
	}
	defer window.Destroy()

	renderer, err := sdl.CreateRenderer(window, -1, sdl.RENDERER_ACCELERATED)
	if err != nil {
		panic(err)
	}
	defer renderer.Destroy()

	if err := mix.OpenAudio(44100, mix.DEFAULT_FORMAT, 4, mix.DEFAULT_CHUNKSIZE); err != nil {
		panic(err)
	}

	chunks := make([]*mix.Chunk, SOUND_SIZE)
	chunks[SOUND_SHOOT] = makeChunk(data_path, "shoot.wav")
	chunks[SOUND_INVADER_DEATH] = makeChunk(data_path, "invaderkilled.wav")
	chunks[SOUND_EXPLOSION] = makeChunk(data_path, "explosion.wav")
	chunks[SOUND_FLEET_MOVEMENT_1] = makeChunk(data_path, "fastinvader1.wav")
	chunks[SOUND_FLEET_MOVEMENT_2] = makeChunk(data_path, "fastinvader2.wav")
	chunks[SOUND_FLEET_MOVEMENT_3] = makeChunk(data_path, "fastinvader3.wav")
	chunks[SOUND_FLEET_MOVEMENT_4] = makeChunk(data_path, "fastinvader4.wav")
	chunks[SOUND_UFO_PASSING] = makeChunk(data_path, "ufo_lowpitch.wav")
	chunks[SOUND_UFO_HIT] = makeChunk(data_path, "ufo_highpitch.wav")

	for _, chunk := range chunks {
		defer chunk.Free()
	}

	for {
		select {
		case screen := <-screenc:
			// fmt.Println("[SDL Driver] graphics update received")
			updateGraphics(renderer, screen)

		case sound := <-soundc:

			_, err := chunks[sound].Play(-1, 1)
			if err != nil {
				panic(err)
			}
			// fmt.Printf("playing sound %d on channel %d\n", sound, ch)

		default:
			if event := sdl.PollEvent(); event != nil {
				// fmt.Println("[SDL Driver] event detected:", event.GetType())
				eventc <- event
			}
			time.Sleep(1 * time.Millisecond)
		}
	}
}

func updateGraphics(renderer *sdl.Renderer, screen [][]bool) {

	if err := renderer.SetDrawColor(0, 0, 0, 255); err != nil {
		panic(err)
	}

	if err := renderer.Clear(); err != nil {
		panic(err)
	}

	if err := renderer.SetDrawColor(255, 255, 255, 255); err != nil {
		panic(err)
	}

	for row := range screen {
		x := int32(row)
		for col := range screen[row] {
			y := int32(col)
			if screen[row][col] {
				if !MODE_MONOCHROME {
					switch {
					case y < 16 && x >= 16 && x < 118+16:
						if err := renderer.SetDrawColor(0, 255, 0, 255); err != nil {
							panic(err)
						}
					case y < 72:
						if err := renderer.SetDrawColor(0, 255, 0, 255); err != nil {
							panic(err)
						}
					case y >= 192 && y < 192+32:
						if err := renderer.SetDrawColor(255, 0, 0, 255); err != nil {
							panic(err)
						}
					default:
						if err := renderer.SetDrawColor(255, 255, 255, 255); err != nil {
							panic(err)
						}
					}
				}

				screen_y := 0xFF - y
				if MODE_COCKTAIL {
					screen_y = y
				}
				if err := renderer.DrawPoint(x, screen_y); err != nil {
					panic(err)
				}
			}
		}
	}

	renderer.Present()
	sdl.Delay(10)
}
