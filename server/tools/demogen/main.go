// Command demogen renders the original demo songs that seed the MIFS server: for each song
// an audio master, artwork, line-synced LRC lyrics and a song.json manifest (see server/seed).
//
// Everything is synthesised from code — no samples, loops, or third-party melodies or lyrics.
// Vocals are spoken lines rendered with Piper TTS and placed on the beat grid, so the LRC
// timestamps are the exact placement times rather than hand-typed guesses.
package main

import (
	"flag"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
)

type config struct {
	out, format string
	voice       Voice
	stems       string
}

func main() {
	out := flag.String("out", "seed", "seed directory to write into")
	piper := flag.String("piper", "piper", "Piper executable")
	model := flag.String("voice", "", "Piper voice model (.onnx), e.g. en_US-ljspeech-high.onnx")
	work := flag.String("work", filepath.Join(os.TempDir(), "mifs-demogen"), "cache for vocal renders and stems")
	only := flag.String("only", "", "render only the song with this id")
	format := flag.String("format", "m4a", "master format: m4a (AAC 256 kb/s), flac, or auto (FLAC unless it exceeds 12 MB)")
	flag.Parse()
	if *model == "" {
		log.Fatal("-voice is required (path to a Piper .onnx voice)")
	}
	cfg := config{
		out:    *out,
		format: *format,
		voice:  Voice{Piper: *piper, Model: *model, Cache: *work},
		stems:  filepath.Join(*work, "stems"),
	}
	for _, song := range songs() {
		if *only != "" && song.ID != *only {
			continue
		}
		if err := render(song, cfg); err != nil {
			log.Fatalf("%s: %v", song.ID, err)
		}
	}
}

// ---- Arrangement ----

type Song struct {
	ID, Title, Artist, Album, Genre string
	TrackNumber, ReleaseYear        int
	BPM                             float64
	LengthScale                     float64 // Piper phoneme length; below 1 speaks faster
	Seed                            uint64
	Sections                        []Section
	FadeBars                        int
	DrumsOverMusic                  float64 // target drum loudness relative to the music, in dB
	Instrumental                    func(a *Arrangement, m *Mix)
	Artwork                         func(size int) *Canvas
}

type Section struct {
	Name    string // intro, verse, chorus, break, hook, outro
	MinBars int
	Lines   []string
}

type Part struct {
	Section
	StartBar, Bars int
}

type PlacedLine struct {
	Text       string
	Start, End float64 // seconds
	Clip       []float32
}

type Arrangement struct {
	Song      *Song
	Parts     []Part
	Beat, Bar float64 // seconds
	Bars      int
	Lines     []PlacedLine
}

// T is the time in seconds of a beat within the song.
func (a *Arrangement) T(bar int, beat float64) float64 { return (float64(bar)*4 + beat) * a.Beat }

// arrange lays vocal lines onto a half-bar grid and sizes each section to fit them.
func arrange(s *Song, clips map[string][]float32) *Arrangement {
	a := &Arrangement{Song: s, Beat: 60 / s.BPM}
	a.Bar = a.Beat * 4
	slot := a.Beat * 2
	bar := 0
	for _, sec := range s.Sections {
		start := float64(bar) * a.Bar
		cursor := start
		end := start
		for _, text := range sec.Lines {
			clip := clips[text]
			// Lines start on exact centiseconds so the LRC times are the true placement.
			at := math.Round(cursor*100) / 100
			dur := float64(len(clip)) / sampleRate
			a.Lines = append(a.Lines, PlacedLine{Text: text, Start: at, End: at + dur, Clip: clip})
			end = at + dur
			cursor = start + math.Ceil((end+0.22-start)/slot)*slot
		}
		bars := sec.MinBars
		if len(sec.Lines) > 0 {
			needed := int(math.Ceil((end + 0.4 - start) / a.Bar))
			bars = max(bars, (needed+3)/4*4)
		}
		a.Parts = append(a.Parts, Part{Section: sec, StartBar: bar, Bars: bars})
		bar += bars
	}
	a.Bars = bar
	return a
}

// ---- Mixing ----

type Mix struct {
	Drums, Music, Pumped, Reverb, Delay *Bus
	Kicks                               []float64
	rng                                 *rand.Rand
}

func render(s *Song, cfg config) error {
	log.Printf("%s: rendering vocals", s.ID)
	clips := map[string][]float32{}
	for _, sec := range s.Sections {
		for _, text := range sec.Lines {
			if clips[text] != nil {
				continue
			}
			clip, err := cfg.voice.Line(text, s.LengthScale)
			if err != nil {
				return err
			}
			clips[text] = clip
		}
	}
	a := arrange(s, clips)
	tail := 2.5
	n := sample(float64(a.Bars)*a.Bar + tail)
	m := &Mix{
		Drums: NewBus(n), Music: NewBus(n), Pumped: NewBus(n), Reverb: NewBus(n), Delay: NewBus(n),
		rng: rand.New(rand.NewPCG(s.Seed, 0x6d696673)),
	}
	log.Printf("%s: %d bars, %.1f s, rendering instruments", s.ID, a.Bars, float64(n)/sampleRate)
	s.Instrumental(a, m)

	// Vocals: dry stem (kept for sync verification), then high-pass, compression and sends.
	dry := make([]float32, n)
	for _, l := range a.Lines {
		off := sample(l.Start)
		for i, v := range l.Clip {
			if off+i < n {
				dry[off+i] += v
			}
		}
	}
	if err := writeWAV(filepath.Join(cfg.stems, s.ID+"-vocals.wav"), &Bus{L: dry, R: dry}, false); err != nil {
		return err
	}
	vox := append([]float32(nil), dry...)
	var hp, presence SVF
	for i, v := range vox {
		x := hp.HP(float64(v), 110, 0.7)
		x += 0.35 * presence.BP(x, 3200, 1.2)
		vox[i] = float32(x)
	}
	Compress(vox, -24, 3, 6)

	// Music makes room for the voice (and pumps against the kick where the song asks for it).
	if len(m.Kicks) > 0 {
		m.Pumped.Scale(PumpCurve(n, m.Kicks, 0.6, 0.11))
	}
	m.Music.Mix(m.Pumped, 1)
	drumGain := float32(math.Pow(10, (IntegratedLoudness(m.Music)+s.DrumsOverMusic-IntegratedLoudness(m.Drums))/20))
	for i := range m.Drums.L {
		m.Drums.L[i] *= drumGain
		m.Drums.R[i] *= drumGain
	}
	instrumental := NewBus(n)
	instrumental.Mix(m.Drums, 1)
	instrumental.Mix(m.Music, 1)
	// Spoken vocals sit a little above the instrumental so every word is intelligible.
	voxBus := &Bus{L: vox, R: append([]float32(nil), vox...)}
	instLUFS, voxLUFS := IntegratedLoudness(instrumental), IntegratedLoudness(voxBus)
	gain := float32(math.Pow(10, (instLUFS+1.5-voxLUFS)/20))
	for i := range vox {
		voxBus.L[i] *= gain
		voxBus.R[i] *= gain
	}
	log.Printf("%s: drums %.1f, music %.1f, instrumental %.1f, vocals %.1f -> %.1f LUFS", s.ID,
		IntegratedLoudness(m.Drums), IntegratedLoudness(m.Music), instLUFS, voxLUFS, IntegratedLoudness(voxBus))
	m.Reverb.Mix(voxBus, 0.22)
	m.Delay.Mix(voxBus, 0.10)

	duck := DuckCurve(voxBus.L, 0.05, 4.5)
	m.Music.Scale(duck)
	softDuck := make([]float32, n)
	for i, g := range duck {
		softDuck[i] = 1 - (1-g)*0.4
	}
	m.Drums.Scale(softDuck)

	master := NewBus(n)
	master.Mix(m.Drums, 1)
	master.Mix(m.Music, 1)
	master.Mix(voxBus, 1.0)
	master.Mix(Reverb(m.Reverb, 0.82, 0.35, 0.025), 1)
	master.Mix(PingPong(m.Delay, a.Beat*0.75, 0.38), 0.8)
	Filter(master, "hp", 28, 0.7) // nothing useful lives below the kick's fundamental
	fade(master, float64(a.Bars-s.FadeBars)*a.Bar, float64(n)/sampleRate)

	log.Printf("%s: mastering", s.ID)
	for pass := 0; pass < 4; pass++ {
		lufs := IntegratedLoudness(master)
		if math.Abs(lufs+14) < 0.15 {
			break
		}
		gain := float32(math.Pow(10, (-14-lufs)/20))
		for i := range master.L {
			master.L[i] *= gain
			master.R[i] *= gain
		}
		Limit(master, -1.8) // headroom for AAC overshoot: -1 dBTP after encoding
	}
	log.Printf("%s: %.2f LUFS", s.ID, IntegratedLoudness(master))

	return writeSong(s, a, master, cfg)
}

// fade ramps the song in over 20 ms and out from `from` to `to` seconds.
func fade(b *Bus, from, to float64) {
	in := sample(0.02)
	for i := 0; i < in; i++ {
		g := float32(i) / float32(in)
		b.L[i] *= g
		b.R[i] *= g
	}
	start, end := sample(from), min(sample(to), b.Len())
	for i := start; i < b.Len(); i++ {
		x := math.Min(float64(i-start)/float64(end-start), 1)
		g := float32(math.Pow(1-x, 2))
		b.L[i] *= g
		b.R[i] *= g
	}
}
