package main

import (
	"math"
)

// Chord is a bass note plus a pad voicing (MIDI note numbers).
type Chord struct {
	Bass  int
	Notes []int
}

// N is a melody note within a bar.
type N struct {
	Beat, Len float64
	Midi      int
}

func songs() []*Song { return []*Song{neonHarbor(), sendMeTheChorus(), paperSatellites()} }

// ---------------------------------------------------------------------------
// Neon Harbor — synthwave, 100 BPM, A minor.

func neonHarbor() *Song {
	verse := []Chord{
		{45, []int{57, 60, 64, 71}}, // Am9
		{41, []int{53, 57, 60, 64}}, // Fmaj7
		{48, []int{55, 60, 64, 67}}, // C
		{40, []int{55, 59, 62, 64}}, // Em7
	}
	chorus := []Chord{
		{41, []int{53, 57, 60, 65}}, // F
		{43, []int{55, 59, 62, 67}}, // G
		{40, []int{55, 59, 64, 67}}, // Em
		{45, []int{57, 60, 64, 69}}, // Am
	}
	lead := [][]N{
		{{0, 1, 76}, {1, 0.5, 74}, {1.5, 0.5, 72}, {2, 1.5, 74}, {3.5, 0.5, 76}},
		{{0, 2, 72}, {2, 1, 69}, {3, 1, 72}},
		{{0, 1.5, 79}, {1.5, 0.5, 76}, {2, 1, 74}, {3, 1, 72}},
		{{0, 3, 71}},
	}
	chorusLines := []string{
		"Neon harbor, hold this moment",
		"Every signal pulling me home",
		"Neon harbor, burn a little brighter",
		"I was never out here alone",
	}
	return &Song{
		ID: "neon-harbor", Title: "Neon Harbor", Artist: "Orchid Relay",
		Album: "Night Signals", TrackNumber: 1, ReleaseYear: 2026, Genre: "Synthwave",
		BPM: 100, LengthScale: 0.95, Seed: 1, FadeBars: 4, DrumsOverMusic: 1,
		Sections: []Section{
			{Name: "intro", MinBars: 8},
			{Name: "verse", MinBars: 8, Lines: []string{
				"Midnight rolls in over the harbor",
				"Headlights spill like paint on the tide",
				"The radio hums a song with no name",
				"I keep the window down for the ride",
			}},
			{Name: "chorus", MinBars: 8, Lines: chorusLines},
			{Name: "inst", MinBars: 4},
			{Name: "verse", MinBars: 8, Lines: []string{
				"Cranes stand tall like sleeping giants",
				"Ferries blink their slow goodbyes",
				"Your voice still loops inside the static",
				"Somewhere under violet skies",
			}},
			{Name: "chorus", MinBars: 8, Lines: chorusLines},
			{Name: "outro", MinBars: 8, Lines: []string{"Hold this moment"}},
		},
		Instrumental: func(a *Arrangement, m *Mix) {
			n := m.Music.Len()
			pads, arp, snares, leads := NewBus(n), NewBus(n), NewBus(n), NewBus(n)
			kick := KickTone{Low: 47, High: 150, PitchDecay: 0.03, AmpDecay: 0.2, Drive: 2}
			for _, p := range a.Parts {
				prog := verse
				if p.Name == "chorus" {
					prog = chorus
				}
				for bi := 0; bi < p.Bars; bi++ {
					bar := p.StartBar + bi
					ch := prog[bi%4]
					t := func(beat float64) float64 { return a.T(bar, beat) }
					intro, outro := p.Name == "intro", p.Name == "outro"
					full := p.Name == "chorus" || p.Name == "inst"
					sparse := intro && bi < 4

					cutoff := 1300.0
					if full {
						cutoff = 2200
					}
					if intro {
						cutoff = 500 + 900*float64(bi)/float64(p.Bars)
					}
					for _, note := range ch.Notes {
						Pad(pads, m.rng, t(0), a.Bar*0.98, note, 0.8, cutoff)
					}
					if !sparse {
						for e := 0; e < 8; e++ {
							note, vel := ch.Bass, 0.8
							if e%2 == 1 {
								note, vel = ch.Bass+12, 0.6
							}
							Bass(m.Music, t(float64(e)*0.5), a.Beat*0.4, note, vel, 600)
						}
					}
					tones := append(append([]int{}, ch.Notes...), ch.Notes[0]+12)
					pattern := []int{0, 2, 1, 3, 4, 3, 2, 1}
					for s := 0; s < 16; s++ {
						vel := 0.55
						if s%4 == 0 {
							vel = 0.8
						}
						pan := -0.35
						if s%2 == 1 {
							pan = 0.35
						}
						bright := 2500.0
						if full {
							bright = 4200
						}
						if sparse {
							bright = 1200 + 300*float64(bi)
						}
						Pluck(arp, t(float64(s)*0.25), a.Beat*0.2, tones[pattern[s%8]]+12, vel, bright, pan)
					}
					if !sparse && !(outro && bi >= p.Bars-2) {
						Kick(m.Drums, t(0), 1, kick)
						Kick(m.Drums, t(2), 1, kick)
						if full {
							Kick(m.Drums, t(2.5), 0.7, kick)
						}
						Snare(snares, m.rng, t(1), 0.9, 185, 0.14, 0)
						Snare(snares, m.rng, t(3), 0.95, 185, 0.14, 0)
						for e := 0; e < 8; e++ {
							vel := 0.35
							if e%2 == 1 {
								vel = 0.6
							}
							Hat(m.Drums, m.rng, t(float64(e)*0.5), vel, 0.035, 0.25)
						}
						if full {
							Hat(m.Drums, m.rng, t(3.5), 0.5, 0.2, 0.25)
						}
					}
					if (intro && bi >= 4) || p.Name == "inst" || outro {
						for _, note := range lead[bi%4] {
							Lead(leads, t(note.Beat), note.Len*a.Beat*0.95, note.Midi, 0.9, 3200, 0.1)
						}
					}
				}
			}
			m.Music.Mix(pads, 1)
			m.Reverb.Mix(pads, 0.3)
			m.Music.Mix(arp, 0.8)
			m.Delay.Mix(arp, 0.25)
			m.Drums.Mix(snares, 1)
			m.Reverb.Mix(snares, 0.6)
			m.Music.Mix(leads, 1)
			m.Delay.Mix(leads, 0.4)
			m.Reverb.Mix(leads, 0.3)
		},
		Artwork: neonHarborArt,
	}
}

func neonHarborArt(size int) *Canvas {
	c := NewCanvas(size)
	horizon := 0.66
	top, mid, low := hex(0x0b0926), hex(0x3b1a63), hex(0xff5c8a)
	c.Shade(func(u, v float64) RGB {
		if v < horizon {
			k := v / horizon
			col := top.Mix(mid, smoothstep(0, 0.7, k)).Mix(low, smoothstep(0.55, 1, k))
			haze := fbm(u*3, v*6, 7)
			return col.Add(hex(0xff6fb0).Scale(0.12 * smoothstep(0.45, 0.8, haze) * k))
		}
		k := (v - horizon) / (1 - horizon)
		return hex(0x1c0b3a).Mix(hex(0x05030e), math.Sqrt(k))
	})
	// Stars.
	for i := 0; i < 180; i++ {
		x, y := hash2(uint32(i), 1, 11), hash2(uint32(i), 2, 11)*0.45
		r := 0.0008 + 0.0022*hash2(uint32(i), 3, 11)
		b := 0.4 + 0.6*hash2(uint32(i), 4, 11)
		c.Fill(x-r, y-r, x+r, y+r, func(u, v float64) float64 { return sdCircle(u, v, x, y, r) }, solid(hex(0xfff3ff)), b*(1-y*1.5))
	}
	// Sun with stripe cut-outs.
	sx, sy, sr := 0.5, 0.5, 0.22
	gaps := func(v float64) float64 {
		d := 1.0
		for k := 0; k < 7; k++ {
			g := 0.52 + float64(k)*0.03
			th := 0.003 + float64(k)*0.0022
			d = math.Min(d, math.Abs(v-g)-th/2)
		}
		return d
	}
	sun := func(u, v float64) float64 { return math.Max(sdCircle(u, v, sx, sy, sr), -gaps(v)) }
	c.Glow(func(u, v float64) float64 { return sdCircle(u, v, sx, sy, sr) }, hex(0xff4f9a), 0.07, 0.35)
	c.Fill(sx-sr, sy-sr, sx+sr, horizon, func(u, v float64) float64 { return math.Max(sun(u, v), v-horizon) },
		func(u, v float64) RGB { return hex(0xffe66d).Mix(hex(0xff3f8f), smoothstep(sy-sr, sy+sr*0.7, v)) }, 1)
	// Harbour silhouette: skyline blocks and two cranes.
	sil := hex(0x160a30)
	for i := 0; i < 26; i++ {
		x := float64(i)/26 + (hash2(uint32(i), 5, 3)-0.5)*0.01
		w := 0.018 + 0.02*hash2(uint32(i), 6, 3)
		h := 0.015 + 0.05*hash2(uint32(i), 7, 3)*(0.4+math.Abs(x-0.5))
		c.Fill(x, horizon-h, x+w, horizon, func(u, v float64) float64 { return sdRoundBox(u, v, x+w/2, horizon-h/2, w/2, h/2, 0.001) }, solid(sil), 1)
		for wy := horizon - h + 0.006; wy < horizon-0.004; wy += 0.008 {
			for wx := x + 0.004; wx < x+w-0.003; wx += 0.007 {
				if hash2(uint32(wx*1e4), uint32(wy*1e4), 9) > 0.72 {
					wxx, wyy := wx, wy
					c.Fill(wxx-0.002, wyy-0.002, wxx+0.002, wyy+0.002, func(u, v float64) float64 { return sdRoundBox(u, v, wxx, wyy, 0.0015, 0.0012, 0.0003) }, solid(hex(0x7ff6ff)), 0.8)
				}
			}
		}
	}
	crane := func(x float64, dir float64) {
		shapes := []func(u, v float64) float64{
			func(u, v float64) float64 { return sdSegment(u, v, x, horizon, x, horizon-0.2, 0.005) },
			func(u, v float64) float64 {
				return sdSegment(u, v, x-dir*0.05, horizon-0.19, x+dir*0.17, horizon-0.19, 0.004)
			},
			func(u, v float64) float64 { return sdSegment(u, v, x, horizon-0.24, x+dir*0.17, horizon-0.19, 0.0015) },
			func(u, v float64) float64 { return sdSegment(u, v, x, horizon-0.24, x-dir*0.05, horizon-0.19, 0.0015) },
			func(u, v float64) float64 { return sdSegment(u, v, x, horizon-0.24, x, horizon-0.19, 0.003) },
			func(u, v float64) float64 {
				return sdSegment(u, v, x+dir*0.12, horizon-0.19, x+dir*0.12, horizon-0.11, 0.0012)
			},
			func(u, v float64) float64 { return sdRoundBox(u, v, x+dir*0.12, horizon-0.105, 0.012, 0.007, 0.001) },
		}
		for _, s := range shapes {
			c.Fill(x-0.25, horizon-0.26, x+0.25, horizon, s, solid(sil), 1)
		}
		lx, ly := x+dir*0.17, horizon-0.19
		c.Fill(lx-0.01, ly-0.01, lx+0.01, ly+0.01, func(u, v float64) float64 { return sdCircle(u, v, lx, ly, 0.004) }, solid(hex(0xff3355)), 1)
	}
	crane(0.13, 1)
	crane(0.87, -1)
	// Sun reflection on the water.
	for k := 0; k < 22; k++ {
		y := horizon + 0.008 + float64(k)*0.012
		w := sr * (1 - float64(k)/26) * (0.6 + 0.4*hash2(uint32(k), 8, 2))
		col := hex(0xffd36d).Mix(hex(0xff3f8f), float64(k)/22)
		c.Fill(sx-w, y-0.004, sx+w, y+0.004, func(u, v float64) float64 { return sdRoundBox(u, v, sx, y, w, 0.0022, 0.002) }, solid(col), 0.55*(1-float64(k)/24))
	}
	// Perspective grid.
	grid := hex(0xff3fd0)
	for k := 1; k < 14; k++ {
		d := float64(k) / 13
		y := horizon + (1-horizon)*d*d
		c.Fill(0, y-0.004, 1, y+0.004, func(u, v float64) float64 { return math.Abs(v-y) - 0.0008 - 0.0012*d }, solid(grid), 0.25+0.5*d)
	}
	for k := -12; k <= 12; k++ {
		x := 0.5 + float64(k)*0.09
		c.Fill(0, horizon, 1, 1, func(u, v float64) float64 {
			return sdSegment(u, v, 0.5+float64(k)*0.012, horizon, x+float64(k)*0.18, 1.1, 0.0009)
		}, solid(grid), 0.45)
	}
	c.Finish(0.045, 0.35, 21)
	return c
}

// ---------------------------------------------------------------------------
// Send Me the Chorus — dance pop, 118 BPM, G minor / B♭ major.

func sendMeTheChorus() *Song {
	verse := []Chord{
		{43, []int{58, 62, 65, 67}}, // Gm7
		{39, []int{58, 62, 63, 67}}, // Ebmaj7
		{46, []int{58, 62, 65, 70}}, // Bb
		{41, []int{57, 60, 65, 69}}, // F
	}
	chorus := []Chord{
		{39, []int{58, 63, 67, 70}}, // Eb
		{41, []int{57, 60, 65, 69}}, // F
		{43, []int{58, 62, 67, 70}}, // Gm
		{38, []int{58, 62, 65, 70}}, // Bb/D
	}
	hook := [][]N{
		{{0, 0.5, 79}, {0.5, 0.5, 77}, {1, 1, 75}, {2.5, 0.5, 74}, {3, 1, 75}},
		{{0, 0.5, 77}, {0.5, 0.5, 72}, {1, 1.5, 77}, {2.5, 0.5, 81}, {3, 1, 79}},
		{{0, 0.5, 74}, {0.5, 0.5, 77}, {1, 1, 79}, {2, 0.5, 77}, {2.5, 0.5, 74}, {3, 1, 70}},
		{{0, 0.5, 72}, {0.5, 0.5, 74}, {1, 1.5, 77}, {2.5, 0.5, 74}, {3, 1, 74}},
	}
	chorusLines := []string{
		"Send me the chorus, just the good part",
		"Skip the intro, cut to the heart",
		"Send me the chorus, turn it up loud",
		"Ten seconds of us in a crowd",
	}
	return &Song{
		ID: "send-me-the-chorus", Title: "Send Me the Chorus", Artist: "Orchid Relay",
		Album: "Night Signals", TrackNumber: 3, ReleaseYear: 2026, Genre: "Dance Pop",
		BPM: 118, LengthScale: 0.9, Seed: 2, FadeBars: 4, DrumsOverMusic: 2,
		Sections: []Section{
			{Name: "intro", MinBars: 8},
			{Name: "verse", MinBars: 8, Lines: []string{
				"Found a song on the late train home",
				"Three minutes long, but I need ten seconds",
				"The part that sounds like you and me",
				"The drop that lands like a confession",
			}},
			{Name: "chorus", MinBars: 8, Lines: chorusLines},
			{Name: "break", MinBars: 4, Lines: []string{"Just the good part"}},
			{Name: "verse", MinBars: 8, Lines: []string{
				"You reply with a bassline and a smile",
				"I scrub back to the moment we found",
				"Loop it once, loop it twice, loop it all night",
				"Every message comes with a sound",
			}},
			{Name: "chorus", MinBars: 8, Lines: chorusLines},
			{Name: "hook", MinBars: 8},
			{Name: "outro", MinBars: 4},
		},
		Instrumental: func(a *Arrangement, m *Mix) {
			n := m.Music.Len()
			stabs, leads, claps := NewBus(n), NewBus(n), NewBus(n)
			kick := KickTone{Low: 50, High: 160, PitchDecay: 0.025, AmpDecay: 0.18, Drive: 2.5}
			for _, p := range a.Parts {
				prog := chorus
				if p.Name == "verse" || p.Name == "break" {
					prog = verse
				}
				for bi := 0; bi < p.Bars; bi++ {
					bar := p.StartBar + bi
					ch := prog[bi%4]
					t := func(beat float64) float64 { return a.T(bar, beat) }
					big := p.Name == "chorus" || p.Name == "hook"
					brk := p.Name == "break"
					drums := !brk && !(p.Name == "intro" && bi < 4)

					cutoff := 1800.0
					if big {
						cutoff = 3000
					}
					if p.Name == "intro" {
						cutoff = 700 + 1800*float64(bi)/float64(p.Bars)
					}
					if brk {
						cutoff = 1200
					}
					for _, note := range ch.Notes {
						Pad(m.Pumped, m.rng, t(0), a.Bar*0.98, note, 0.75, cutoff)
					}
					if drums || brk {
						for _, beat := range []float64{0.5, 1.5, 2.5, 3.5} {
							note := ch.Bass
							if beat == 1.5 || beat == 3.5 {
								note += 12
							}
							if !brk {
								Bass(m.Pumped, t(beat), a.Beat*0.42, note, 0.85, 900)
							}
						}
					}
					rhythm := []float64{0, 0.75, 1.5}
					if big {
						rhythm = []float64{0, 0.75, 1.5, 2.5, 3.25}
					}
					if !brk {
						for _, beat := range rhythm {
							for i, note := range ch.Notes {
								Stab(stabs, t(beat), a.Beat*0.35, note+12, 0.7, float64(i-1)*0.3)
							}
						}
					}
					if drums {
						for beat := 0.0; beat < 4; beat++ {
							Kick(m.Drums, t(beat), 1, kick)
							m.Kicks = append(m.Kicks, t(beat))
							Hat(m.Drums, m.rng, t(beat+0.5), 0.55, 0.12, 0.2)
						}
						if p.Name != "intro" {
							Clap(claps, m.rng, t(1), 0.9, 0)
							Clap(claps, m.rng, t(3), 0.9, 0)
						}
						if big {
							for s := 0; s < 16; s++ {
								vel := 0.2
								if s%2 == 1 {
									vel = 0.32
								}
								Hat(m.Drums, m.rng, t(float64(s)*0.25), vel, 0.02, -0.3)
							}
						}
					}
					if brk && bi >= p.Bars-2 {
						if bi == p.Bars-2 {
							Riser(m.Music, m.rng, t(0), a.Bar*2, 0.8)
						}
						if bi == p.Bars-1 {
							for s := 0; s < 16; s++ {
								Snare(m.Drums, m.rng, t(float64(s)*0.25), 0.25+0.6*float64(s)/16, 200, 0.08, 0)
							}
						}
					}
					if p.Name == "hook" || (p.Name == "intro" && bi >= 4) {
						for _, note := range hook[bi%4] {
							Lead(leads, t(note.Beat), note.Len*a.Beat*0.9, note.Midi, 0.9, 4200, -0.1)
						}
					}
				}
			}
			m.Pumped.Mix(stabs, 1)
			m.Reverb.Mix(stabs, 0.2)
			m.Drums.Mix(claps, 1)
			m.Reverb.Mix(claps, 0.35)
			m.Music.Mix(leads, 1)
			m.Delay.Mix(leads, 0.45)
			m.Reverb.Mix(leads, 0.25)
		},
		Artwork: sendMeTheChorusArt,
	}
}

func sendMeTheChorusArt(size int) *Canvas {
	c := NewCanvas(size)
	c.Shade(func(u, v float64) RGB {
		k := smoothstep(0, 1, (u+v)/2)
		col := hex(0xff7a59).Mix(hex(0xff3d8b), smoothstep(0, 0.5, k)).Mix(hex(0x6a35ff), smoothstep(0.45, 1, k))
		return col
	})
	c.Glow(func(u, v float64) float64 { return sdCircle(u, v, 0.15, 0.2, 0.05) }, hex(0xffd166), 0.18, 0.35)
	c.Glow(func(u, v float64) float64 { return sdCircle(u, v, 0.9, 0.85, 0.05) }, hex(0x38d9ff), 0.2, 0.3)
	// Sound rings.
	for k := 1; k < 9; k++ {
		r := 0.18 + float64(k)*0.07
		c.Fill(0, 0, 1, 1, func(u, v float64) float64 { return math.Abs(sdCircle(u, v, 0.5, 0.48, r)) - 0.0015 }, solid(hex(0xffffff)), 0.16-float64(k)*0.012)
	}
	// Confetti.
	palette := []RGB{hex(0xffd166), hex(0x38d9ff), hex(0xffffff), hex(0x7cffb2), hex(0xffa3d7)}
	for i := 0; i < 70; i++ {
		x, y := hash2(uint32(i), 1, 5), hash2(uint32(i), 2, 5)
		if math.Abs(x-0.5) < 0.36 && math.Abs(y-0.5) < 0.27 {
			continue
		}
		w := 0.006 + 0.01*hash2(uint32(i), 3, 5)
		ang := hash2(uint32(i), 4, 5) * math.Pi
		col := palette[i%len(palette)]
		ca, sa := math.Cos(ang), math.Sin(ang)
		c.Fill(x-0.03, y-0.03, x+0.03, y+0.03, func(u, v float64) float64 {
			du, dv := u-x, v-y
			return sdRoundBox(du*ca+dv*sa, -du*sa+dv*ca, 0, 0, w, w*0.45, w*0.3)
		}, solid(col), 0.9)
	}
	// Message bubble with a waveform and a MIFS-style selection.
	bx, by, hw, hh := 0.5, 0.47, 0.31, 0.2
	tail := [][2]float64{{0.28, 0.6}, {0.4, 0.62}, {0.24, 0.74}}
	bubble := func(u, v float64) float64 {
		return math.Min(sdRoundBox(u, v, bx, by, hw, hh, 0.09), sdPolygon(u, v, tail)-0.01)
	}
	c.Glow(bubble, hex(0x2a0a55), 0.03, -0.9)
	c.Fill(0.1, 0.2, 0.9, 0.8, bubble, solid(hex(0xfffaf5)), 1)
	bars := 17
	for i := 0; i < bars; i++ {
		x := bx - 0.24 + float64(i)*0.48/float64(bars-1)
		h := 0.03 + 0.11*math.Pow(math.Sin(math.Pi*float64(i)/float64(bars-1)), 1.5)*(0.55+0.45*hash2(uint32(i), 9, 1))
		col := hex(0xff6a5b).Mix(hex(0x6a35ff), float64(i)/float64(bars-1))
		c.Fill(x-0.012, by-h, x+0.012, by+h, func(u, v float64) float64 { return sdRoundBox(u, v, x, by, 0.0095, h, 0.0095) }, solid(col), 1)
	}
	c.Fill(0.3, 0.28, 0.7, 0.66, func(u, v float64) float64 {
		return math.Abs(sdRoundBox(u, v, bx+0.03, by, 0.1, 0.155, 0.03)) - 0.006
	}, solid(hex(0x2b1466)), 1)
	c.Finish(0.03, 0.2, 33)
	return c
}

// ---------------------------------------------------------------------------
// Paper Satellites — lo-fi hip hop, 84 BPM, D major, swung.

func paperSatellites() *Song {
	verse := []Chord{
		{38, []int{54, 57, 61, 64}}, // Dmaj9
		{35, []int{50, 57, 61, 66}}, // Bm9
		{40, []int{54, 55, 59, 62}}, // Em9
		{33, []int{55, 61, 66, 71}}, // A13
	}
	chorus := []Chord{
		{43, []int{54, 59, 62, 66}}, // Gmaj7
		{42, []int{52, 57, 61, 64}}, // F#m7
		{40, []int{54, 55, 59, 62}}, // Em9
		{33, []int{55, 62, 64, 69}}, // A7sus4
	}
	melody := [][]N{
		{{0, 1, 78}, {1, 0.5, 81}, {1.5, 1, 83}, {3, 1, 81}},
		{{0, 1.5, 78}, {1.5, 0.5, 76}, {2, 2, 74}},
		{{0, 0.5, 76}, {0.5, 0.5, 78}, {1, 1.5, 79}, {2.5, 0.5, 78}, {3, 1, 74}},
		{{0, 1, 81}, {1, 1, 76}, {2, 2, 73}},
	}
	chorusLines := []string{
		"Paper satellites",
		"Drifting over rooftops tonight",
		"Carry what I couldn't say",
		"Land it softly where you stay",
	}
	wow := func(t float64) float64 {
		return math.Pow(2, (0.07*math.Sin(2*math.Pi*0.55*t)+0.02*math.Sin(2*math.Pi*5.8*t))/12)
	}
	return &Song{
		ID: "paper-satellites", Title: "Paper Satellites", Artist: "Juniper Fold",
		ReleaseYear: 2026, Genre: "Lo-fi Hip Hop",
		BPM: 84, LengthScale: 1.0, Seed: 3, FadeBars: 4, DrumsOverMusic: 1,
		Sections: []Section{
			{Name: "intro", MinBars: 8},
			{Name: "verse", MinBars: 8, Lines: []string{
				"Folded a rocket out of homework",
				"Taped a promise to its wing",
				"Threw it past the streetlight orbit",
				"Waited for the sky to ring",
			}},
			{Name: "chorus", MinBars: 8, Lines: chorusLines},
			{Name: "verse", MinBars: 8, Lines: []string{
				"Kettle whistles in the kitchen",
				"Rain is writing on the glass",
				"Every little thing I send you",
				"Is a moment made to last",
			}},
			{Name: "chorus", MinBars: 8, Lines: chorusLines},
			{Name: "outro", MinBars: 8},
		},
		Instrumental: func(a *Arrangement, m *Mix) {
			n := m.Music.Len()
			keys, drums, bells, bed := NewBus(n), NewBus(n), NewBus(n), NewBus(n)
			kick := KickTone{Low: 50, High: 115, PitchDecay: 0.03, AmpDecay: 0.24, Drive: 1.6}
			swing := 0.09 // of a beat, applied to off-beat eighths
			for _, p := range a.Parts {
				prog := verse
				if p.Name == "chorus" {
					prog = chorus
				}
				for bi := 0; bi < p.Bars; bi++ {
					bar := p.StartBar + bi
					ch := prog[bi%4]
					t := func(beat float64) float64 { return a.T(bar, beat) }
					intro, outro := p.Name == "intro", p.Name == "outro"
					drumsOn := !(intro && bi < 4) && !(outro && bi >= 4)

					for _, hit := range [][2]float64{{0, 1.4}, {1.75 + swing/2, 0.5}, {2.5 + swing, 1.3}} {
						vel := 0.55 + 0.2*m.rng.Float64()
						for i, note := range ch.Notes {
							EPiano(keys, t(hit[0])+float64(i)*0.012, hit[1]*a.Beat, note, vel, float64(i-1)*0.25, wow)
						}
					}
					if !(intro && bi < 2) {
						SubBass(m.Music, t(0), a.Beat*1.5, ch.Bass, 0.65)
						SubBass(m.Music, t(2.5+swing), a.Beat*1.2, ch.Bass+7, 0.5)
					}
					if drumsOn {
						Kick(drums, t(0), 1, kick)
						Kick(drums, t(2.5+swing), 0.85, kick)
						if bi%2 == 1 {
							Kick(drums, t(3.5+swing), 0.6, kick)
						}
						Snare(drums, m.rng, t(1), 0.85, 170, 0.12, 0.05)
						Snare(drums, m.rng, t(3), 0.9, 170, 0.12, 0.05)
						for e := 0; e < 8; e++ {
							beat := float64(e) * 0.5
							vel := 0.45 + 0.15*m.rng.Float64()
							if e%2 == 1 {
								beat += swing
								vel *= 0.7
							}
							Hat(drums, m.rng, t(beat), vel, 0.03, 0.3)
						}
					}
					if intro || outro {
						for _, note := range melody[bi%4] {
							Bell(bells, t(note.Beat), note.Len*a.Beat, note.Midi, 0.8, -0.2, wow)
						}
					}
				}
			}
			// Vinyl bed: sparse crackle, a low rumble and a little hiss.
			var rumble, hiss SVF
			for i := 0; i < n; i++ {
				x := 0.0
				if m.rng.Float64() < 9.0/sampleRate {
					x = math.Pow(m.rng.Float64(), 3) * 0.5 * (m.rng.Float64()*2 - 1)
				}
				x += rumble.LP(m.rng.Float64()*2-1, 200, 0.7)*0.02 + hiss.HP(m.rng.Float64()*2-1, 6000, 0.7)*0.004
				bed.Add(i, x, x*0.9)
			}
			Filter(keys, "lp", 5500, 0.7)
			Filter(drums, "lp", 7000, 0.7)
			m.Music.Mix(keys, 1)
			m.Reverb.Mix(keys, 0.25)
			m.Drums.Mix(drums, 1)
			m.Reverb.Mix(drums, 0.08)
			m.Music.Mix(bells, 1)
			m.Delay.Mix(bells, 0.4)
			m.Reverb.Mix(bells, 0.35)
			m.Music.Mix(bed, 1)
		},
		Artwork: paperSatellitesArt,
	}
}

func paperSatellitesArt(size int) *Canvas {
	c := NewCanvas(size)
	c.Shade(func(u, v float64) RGB {
		col := hex(0x0a1430).Mix(hex(0x274a6e), smoothstep(0, 1, v)).Mix(hex(0x5a4a7a), smoothstep(0.55, 0.85, v)*0.5)
		haze := fbm(u*2.5+3, v*4, 17)
		return col.Add(hex(0x6fb3c9).Scale(0.08 * smoothstep(0.5, 0.85, haze)))
	})
	for i := 0; i < 140; i++ {
		x, y := hash2(uint32(i), 1, 23), hash2(uint32(i), 2, 23)*0.65
		r := 0.0008 + 0.002*hash2(uint32(i), 3, 23)
		c.Fill(x-r, y-r, x+r, y+r, func(u, v float64) float64 { return sdCircle(u, v, x, y, r) }, solid(hex(0xfff5dc)), 0.35+0.6*hash2(uint32(i), 4, 23))
	}
	// Crescent moon.
	moon := func(u, v float64) float64 {
		return math.Max(sdCircle(u, v, 0.76, 0.2, 0.085), -sdCircle(u, v, 0.8, 0.17, 0.08))
	}
	c.Glow(func(u, v float64) float64 { return sdCircle(u, v, 0.76, 0.2, 0.085) }, hex(0xffe3a3), 0.08, 0.18)
	c.Fill(0.6, 0.05, 0.9, 0.35, moon, solid(hex(0xffeec2)), 1)
	// Paper planes with dashed trails.
	plane := func(x, y, s, ang float64, trail [][2]float64) {
		for i := 0; i+1 < len(trail); i++ {
			ax, ay, bx, by := trail[i][0], trail[i][1], trail[i+1][0], trail[i+1][1]
			for d := 0.0; d < 1; d += 0.2 {
				sx, sy := ax+(bx-ax)*d, ay+(by-ay)*d
				ex, ey := ax+(bx-ax)*(d+0.1), ay+(by-ay)*(d+0.1)
				c.Fill(math.Min(sx, ex)-0.01, math.Min(sy, ey)-0.01, math.Max(sx, ex)+0.01, math.Max(sy, ey)+0.01,
					func(u, v float64) float64 { return sdSegment(u, v, sx, sy, ex, ey, 0.0018) }, solid(hex(0xf4efe6)), 0.45)
			}
		}
		rot := func(px, py float64) [2]float64 {
			ca, sa := math.Cos(ang), math.Sin(ang)
			return [2]float64{x + (px*ca-py*sa)*s, y + (px*sa+py*ca)*s}
		}
		nose, tailTop, tailBottom, fold := rot(0.1, 0), rot(-0.08, -0.05), rot(-0.06, 0.04), rot(-0.035, 0.006)
		top := [][2]float64{nose, tailTop, fold}
		bottom := [][2]float64{nose, fold, tailBottom}
		for _, tri := range []struct {
			pts [][2]float64
			col RGB
		}{{top, hex(0xfbf7ef)}, {bottom, hex(0xc9c3d6)}} {
			x0, y0, x1, y1 := bounds(tri.pts)
			pts := tri.pts
			c.Fill(x0, y0, x1, y1, func(u, v float64) float64 { return sdPolygon(u, v, pts) }, solid(tri.col), 1)
		}
	}
	plane(0.34, 0.33, 1.25, -0.25, [][2]float64{{0.04, 0.5}, {0.14, 0.44}, {0.24, 0.37}})
	plane(0.62, 0.47, 0.8, -0.1, [][2]float64{{0.42, 0.56}, {0.5, 0.52}, {0.56, 0.48}})
	plane(0.2, 0.62, 0.55, -0.35, [][2]float64{{0.05, 0.7}, {0.1, 0.67}, {0.16, 0.64}})
	// Rooftops with warm windows.
	roofs := [][2]float64{{0, 1}, {0, 0.8}, {0.08, 0.8}, {0.14, 0.74}, {0.2, 0.8}, {0.27, 0.8}, {0.27, 0.77}, {0.4, 0.77},
		{0.4, 0.83}, {0.48, 0.83}, {0.55, 0.76}, {0.62, 0.83}, {0.66, 0.83}, {0.66, 0.72}, {0.78, 0.72}, {0.78, 0.79},
		{0.86, 0.79}, {0.92, 0.74}, {1, 0.79}, {1, 1}}
	c.Fill(0, 0.7, 1, 1, func(u, v float64) float64 { return sdPolygon(u, v, roofs) }, solid(hex(0x0b1022)), 1)
	c.Fill(0.68, 0.6, 0.76, 0.73, func(u, v float64) float64 {
		return math.Min(sdRoundBox(u, v, 0.72, 0.655, 0.03, 0.025, 0.012), sdSegment(u, v, 0.72, 0.68, 0.72, 0.73, 0.004))
	}, solid(hex(0x0b1022)), 1)
	c.Fill(0.45, 0.65, 0.5, 0.84, func(u, v float64) float64 { return sdSegment(u, v, 0.47, 0.83, 0.47, 0.68, 0.0022) }, solid(hex(0x0b1022)), 1)
	windows := [][2]float64{{0.05, 0.86}, {0.16, 0.84}, {0.3, 0.84}, {0.36, 0.9}, {0.52, 0.88}, {0.7, 0.79}, {0.74, 0.86}, {0.9, 0.85}, {0.95, 0.92}}
	for i, w := range windows {
		wx, wy := w[0], w[1]
		c.Glow(func(u, v float64) float64 { return sdRoundBox(u, v, wx, wy, 0.012, 0.016, 0.003) }, hex(0xffa552), 0.02, 0.25)
		c.Fill(wx-0.02, wy-0.02, wx+0.02, wy+0.02, func(u, v float64) float64 { return sdRoundBox(u, v, wx, wy, 0.012, 0.016, 0.003) },
			solid(hex(0xffc27a).Mix(hex(0xff9a4d), float64(i%3)/3)), 1)
	}
	c.Finish(0.08, 0.4, 41)
	return c
}
