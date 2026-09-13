package main

import (
	"math"
	"math/rand/v2"
)

const sampleRate = 44100

// Bus is a stereo float buffer covering the whole song.
type Bus struct{ L, R []float32 }

func NewBus(n int) *Bus { return &Bus{L: make([]float32, n), R: make([]float32, n)} }

func (b *Bus) Len() int { return len(b.L) }

func (b *Bus) Add(i int, l, r float64) {
	if i >= 0 && i < len(b.L) {
		b.L[i] += float32(l)
		b.R[i] += float32(r)
	}
}

// Mix adds src·gain into b.
func (b *Bus) Mix(src *Bus, gain float64) {
	g := float32(gain)
	for i := range b.L {
		b.L[i] += src.L[i] * g
		b.R[i] += src.R[i] * g
	}
}

// Scale multiplies b by a per-sample gain curve.
func (b *Bus) Scale(curve []float32) {
	for i := range b.L {
		b.L[i] *= curve[i]
		b.R[i] *= curve[i]
	}
}

func sample(t float64) int { return int(math.Round(t * sampleRate)) }

func mtof(m float64) float64 { return 440 * math.Pow(2, (m-69)/12) }

// panGains is an equal-power pan law for p in -1 (left)...1 (right).
func panGains(p float64) (float64, float64) {
	a := (p + 1) * math.Pi / 4
	return math.Cos(a), math.Sin(a)
}

func polyBLEP(t, dt float64) float64 {
	switch {
	case t < dt:
		t /= dt
		return t + t - t*t - 1
	case t > 1-dt:
		t = (t - 1) / dt
		return t*t + t + t + 1
	}
	return 0
}

// Osc is a band-limited oscillator; call one waveform per sample.
type Osc struct{ phase float64 }

func (o *Osc) advance(freq float64) (float64, float64) {
	dt := freq / sampleRate
	p := o.phase
	o.phase += dt
	if o.phase >= 1 {
		o.phase -= math.Floor(o.phase)
	}
	return p, dt
}

func (o *Osc) Saw(freq float64) float64 {
	p, dt := o.advance(freq)
	return 2*p - 1 - polyBLEP(p, dt)
}

func (o *Osc) Square(freq float64) float64 {
	p, dt := o.advance(freq)
	v := -1.0
	if p < 0.5 {
		v = 1
	}
	return v + polyBLEP(p, dt) - polyBLEP(math.Mod(p+0.5, 1), dt)
}

func (o *Osc) Sine(freq float64) float64 {
	p, _ := o.advance(freq)
	return math.Sin(2 * math.Pi * p)
}

func (o *Osc) Triangle(freq float64) float64 {
	p, _ := o.advance(freq)
	return 1 - 4*math.Abs(p-0.5)
}

// ADSR envelope; gate is how long the note is held.
type ADSR struct{ A, D, S, R float64 }

func (e ADSR) level(t float64) float64 {
	if t < e.A {
		return t / e.A
	}
	t -= e.A
	if t < e.D {
		return 1 - (1-e.S)*t/e.D
	}
	return e.S
}

func (e ADSR) At(t, gate float64) float64 {
	if t < gate {
		return e.level(t)
	}
	rt := t - gate
	if rt >= e.R {
		return 0
	}
	return e.level(gate) * (1 - rt/e.R)
}

// SVF is a topology-preserving state variable filter (Zavalishin).
type SVF struct{ ic1, ic2 float64 }

// Process returns low-, band- and high-pass outputs.
func (f *SVF) Process(x, cutoff, q float64) (lp, bp, hp float64) {
	cutoff = math.Min(math.Max(cutoff, 10), sampleRate*0.45)
	g := math.Tan(math.Pi * cutoff / sampleRate)
	k := 1 / q
	a1 := 1 / (1 + g*(g+k))
	a2 := g * a1
	a3 := g * a2
	v3 := x - f.ic2
	v1 := a1*f.ic1 + a2*v3
	v2 := f.ic2 + a2*f.ic1 + a3*v3
	f.ic1 = 2*v1 - f.ic1
	f.ic2 = 2*v2 - f.ic2
	return v2, v1, x - k*v1 - v2
}

func (f *SVF) LP(x, cutoff, q float64) float64 { lp, _, _ := f.Process(x, cutoff, q); return lp }
func (f *SVF) BP(x, cutoff, q float64) float64 { _, bp, _ := f.Process(x, cutoff, q); return bp }
func (f *SVF) HP(x, cutoff, q float64) float64 { _, _, hp := f.Process(x, cutoff, q); return hp }

// ---- Instruments. Each renders one note into a bus at t0 seconds. ----

// Pad: three detuned saws spread across the stereo field through a slowly moving low-pass.
func Pad(b *Bus, rng *rand.Rand, t0, dur float64, midi int, vel, cutoff float64) {
	env := ADSR{A: 0.45, D: 0.8, S: 0.75, R: 1.1}
	f := mtof(float64(midi))
	detune := []float64{-0.12, 0.0, 0.10}
	pans := []float64{-0.75, 0, 0.75}
	start := sample(t0)
	n := sample(dur + env.R)
	lfo := rng.Float64() * 2 * math.Pi
	for v := range detune {
		osc := Osc{phase: rng.Float64()}
		var filt SVF
		fv := f * math.Pow(2, detune[v]/12)
		gl, gr := panGains(pans[v])
		for i := 0; i < n; i++ {
			t := float64(i) / sampleRate
			c := cutoff * (1 + 0.25*math.Sin(2*math.Pi*0.18*(t0+t)+lfo))
			s := filt.LP(osc.Saw(fv), c, 0.7) * env.At(t, dur) * vel * 0.16
			b.Add(start+i, s*gl, s*gr)
		}
	}
}

// Pluck: bright saw/square blip with a fast filter decay, used for arpeggios.
func Pluck(b *Bus, t0, dur float64, midi int, vel, bright, pan float64) {
	f := mtof(float64(midi))
	var o1, o2 Osc
	o2.phase = 0.3
	var filt SVF
	gl, gr := panGains(pan)
	start := sample(t0)
	n := sample(math.Min(dur+0.25, 1.2))
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		amp := math.Exp(-t / 0.2)
		if t > dur {
			amp *= math.Exp(-(t - dur) / 0.04)
		}
		c := 400 + bright*math.Exp(-t/0.11)
		s := filt.LP(0.6*o1.Saw(f)+0.4*o2.Square(f*1.003), c, 1.1) * amp * vel * 0.22
		b.Add(start+i, s*gl, s*gr)
	}
}

// Bass: saw plus sine sub, filter envelope, a touch of drive.
func Bass(b *Bus, t0, dur float64, midi int, vel, bright float64) {
	env := ADSR{A: 0.004, D: 0.12, S: 0.7, R: 0.06}
	f := mtof(float64(midi))
	var saw, sub Osc
	var filt SVF
	start := sample(t0)
	n := sample(dur + env.R)
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		c := 160 + bright*math.Exp(-t/0.09)
		x := filt.LP(saw.Saw(f), c, 0.9)*0.8 + sub.Sine(f)*0.7
		s := math.Tanh(1.6*x) * env.At(t, dur) * vel * 0.34
		b.Add(start+i, s, s)
	}
}

// SubBass: rounded sine bass for the lo-fi track.
func SubBass(b *Bus, t0, dur float64, midi int, vel float64) {
	env := ADSR{A: 0.012, D: 0.3, S: 0.8, R: 0.12}
	f := mtof(float64(midi))
	var o Osc
	start := sample(t0)
	n := sample(dur + env.R)
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		x := o.Sine(f)
		s := math.Tanh(1.8*x) / math.Tanh(1.8) * env.At(t, dur) * vel * 0.4
		b.Add(start+i, s, s)
	}
}

// Lead: square/saw pair with delayed vibrato.
func Lead(b *Bus, t0, dur float64, midi int, vel, cutoff, pan float64) {
	env := ADSR{A: 0.012, D: 0.25, S: 0.7, R: 0.18}
	f := mtof(float64(midi))
	var o1, o2 Osc
	var filt SVF
	gl, gr := panGains(pan)
	start := sample(t0)
	n := sample(dur + env.R)
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		vib := 1.0
		if t > 0.18 {
			depth := math.Min((t-0.18)/0.3, 1) * 0.12
			vib = math.Pow(2, depth*math.Sin(2*math.Pi*5.4*t)/12)
		}
		x := 0.55*o1.Square(f*vib) + 0.45*o2.Saw(f*vib*1.004)
		s := filt.LP(x, cutoff*(0.6+0.4*math.Exp(-t/0.3)), 0.8) * env.At(t, dur) * vel * 0.18
		b.Add(start+i, s*gl, s*gr)
	}
}

// Bell: soft triangle/sine bell, for the lo-fi melody. wow modulates pitch (tape).
func Bell(b *Bus, t0, dur float64, midi int, vel, pan float64, wow func(float64) float64) {
	f := mtof(float64(midi))
	var o1, o2 Osc
	gl, gr := panGains(pan)
	start := sample(t0)
	n := sample(dur + 0.9)
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		w := wow(t0 + t)
		amp := math.Min(t/0.006, 1) * math.Exp(-t/0.9)
		if t > dur {
			amp *= math.Exp(-(t - dur) / 0.25)
		}
		s := (0.8*o1.Triangle(f*w) + 0.2*o2.Sine(f*w*3.01)*math.Exp(-t/0.15)) * amp * vel * 0.2
		b.Add(start+i, s*gl, s*gr)
	}
}

// EPiano: two-operator FM electric piano with stereo tremolo.
func EPiano(b *Bus, t0, dur float64, midi int, vel, pan float64, wow func(float64) float64) {
	f := mtof(float64(midi))
	var carrier, mod, tine Osc
	gl, gr := panGains(pan)
	start := sample(t0)
	n := sample(dur + 0.35)
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		w := wow(t0 + t)
		index := (1.6*math.Exp(-t/0.35) + 0.25) * (0.6 + 0.4*vel)
		m := mod.Sine(f * w)
		// Phase modulation: offset the carrier's phase by the modulator.
		p, _ := carrier.advance(f * w)
		x := math.Sin(2*math.Pi*p + index*m)
		x += 0.12 * tine.Sine(f*w*7) * math.Exp(-t/0.05)
		amp := math.Min(t/0.004, 1) * math.Exp(-t/2.2)
		if t > dur {
			amp *= math.Max(0, 1-(t-dur)/0.35)
		}
		trem := 0.12 * math.Sin(2*math.Pi*4.2*(t0+t))
		s := x * amp * vel * 0.16
		b.Add(start+i, s*gl*(1+trem), s*gr*(1-trem))
	}
}

// Stab: bright additive piano-like chord hit for the dance track.
func Stab(b *Bus, t0, dur float64, midi int, vel, pan float64) {
	f := mtof(float64(midi))
	gl, gr := panGains(pan)
	start := sample(t0)
	n := sample(dur + 0.12)
	oscs := make([]Osc, 7)
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		x := 0.0
		for k := range oscs {
			h := float64(k + 1)
			x += oscs[k].Sine(f*h*(1+0.0004*h*h)) / math.Pow(h, 1.25) * math.Exp(-t/(0.55/math.Pow(h, 0.6)))
		}
		amp := math.Min(t/0.002, 1)
		if t > dur {
			amp *= math.Max(0, 1-(t-dur)/0.12)
		}
		s := x * amp * vel * 0.12
		b.Add(start+i, s*gl, s*gr)
	}
}

// ---- Drums ----

type KickTone struct {
	Low, High, PitchDecay, AmpDecay, Drive float64
}

func Kick(b *Bus, t0, vel float64, k KickTone) {
	start := sample(t0)
	n := sample(k.AmpDecay * 6)
	phase := 0.0
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		f := k.Low + (k.High-k.Low)*math.Exp(-t/k.PitchDecay)
		phase += f / sampleRate
		x := math.Sin(2*math.Pi*phase) * math.Exp(-t/k.AmpDecay)
		if t < 0.004 {
			x += (1 - t/0.004) * 0.3 // click
		}
		s := math.Tanh(k.Drive*x) / math.Tanh(k.Drive) * vel * 0.9
		b.Add(start+i, s, s)
	}
}

func Snare(b *Bus, rng *rand.Rand, t0, vel, tone, decay, pan float64) {
	start := sample(t0)
	n := sample(decay * 6)
	var o Osc
	var bp, hp SVF
	gl, gr := panGains(pan)
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		body := o.Sine(tone*(1+0.5*math.Exp(-t/0.01))) * math.Exp(-t/0.06) * 0.55
		noise := rng.Float64()*2 - 1
		snap := hp.HP(bp.BP(noise, 2600, 0.6), 700, 0.7) * math.Exp(-t/decay) * 1.4
		s := (body + snap) * vel * 0.5
		b.Add(start+i, s*gl, s*gr)
	}
}

func Clap(b *Bus, rng *rand.Rand, t0, vel, pan float64) {
	start := sample(t0)
	n := sample(0.5)
	var bp SVF
	gl, gr := panGains(pan)
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		var env float64
		for _, off := range []float64{0, 0.011, 0.023} {
			if t >= off {
				env = math.Max(env, math.Exp(-(t-off)/0.007))
			}
		}
		if t >= 0.023 {
			env = math.Max(env, 0.55*math.Exp(-(t-0.023)/0.11))
		}
		s := bp.BP(rng.Float64()*2-1, 1400, 1.4) * env * vel * 1.3
		b.Add(start+i, s*gl, s*gr)
	}
}

func Hat(b *Bus, rng *rand.Rand, t0, vel, decay, pan float64) {
	start := sample(t0)
	n := sample(decay * 6)
	var hp SVF
	gl, gr := panGains(pan)
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		s := hp.HP(rng.Float64()*2-1, 7500, 0.8) * math.Exp(-t/decay) * vel * 0.28
		b.Add(start+i, s*gl, s*gr)
	}
}

// Riser: band-passed noise sweeping upwards over dur seconds.
func Riser(b *Bus, rng *rand.Rand, t0, dur, vel float64) {
	start := sample(t0)
	n := sample(dur)
	var fl, fr SVF
	for i := 0; i < n; i++ {
		x := float64(i) / float64(n)
		c := 300 * math.Pow(40, x)
		amp := x * x * vel * 0.5
		b.Add(start+i, fl.BP(rng.Float64()*2-1, c, 2)*amp, fr.BP(rng.Float64()*2-1, c, 2)*amp)
	}
}
