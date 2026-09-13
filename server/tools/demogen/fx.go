package main

import (
	"math"
)

// ---- Reverb (Freeverb) ----

type comb struct {
	buf                   []float64
	idx                   int
	store, feedback, damp float64
}

func (c *comb) process(in float64) float64 {
	out := c.buf[c.idx]
	c.store = out*(1-c.damp) + c.store*c.damp
	c.buf[c.idx] = in + c.store*c.feedback
	if c.idx++; c.idx == len(c.buf) {
		c.idx = 0
	}
	return out
}

type allpass struct {
	buf []float64
	idx int
}

func (a *allpass) process(in float64) float64 {
	bufout := a.buf[a.idx]
	a.buf[a.idx] = in + bufout*0.5
	if a.idx++; a.idx == len(a.buf) {
		a.idx = 0
	}
	return bufout - in
}

// Reverb returns the wet signal of a Freeverb-style room fed by send.
// room is 0...1 (size), damp 0...1 (high-frequency absorption), preDelay in seconds.
func Reverb(send *Bus, room, damp, preDelay float64) *Bus {
	combTunings := []int{1116, 1188, 1277, 1356, 1422, 1491, 1557, 1617}
	allpassTunings := []int{556, 441, 341, 225}
	const spread = 23
	build := func(offset int) ([]*comb, []*allpass) {
		var cs []*comb
		for _, n := range combTunings {
			cs = append(cs, &comb{buf: make([]float64, n+offset), feedback: 0.7 + 0.28*room, damp: damp * 0.4})
		}
		var as []*allpass
		for _, n := range allpassTunings {
			as = append(as, &allpass{buf: make([]float64, n+offset)})
		}
		return cs, as
	}
	cl, al := build(0)
	cr, ar := build(spread)
	out := NewBus(send.Len())
	pre := sample(preDelay)
	var hp, lp SVF
	for i := range send.L {
		j := i - pre
		in := 0.0
		if j >= 0 {
			in = float64(send.L[j]+send.R[j]) * 0.5
		}
		in = lp.LP(hp.HP(in, 180, 0.7), 7000, 0.7) * 0.015
		var l, r float64
		for k := range cl {
			l += cl[k].process(in)
			r += cr[k].process(in)
		}
		for k := range al {
			l = al[k].process(l)
			r = ar[k].process(r)
		}
		out.L[i] = float32(l * 3)
		out.R[i] = float32(r * 3)
	}
	return out
}

// PingPong returns the wet signal of a stereo ping-pong delay fed by send.
func PingPong(send *Bus, delay, feedback float64) *Bus {
	n := sample(delay)
	dl := make([]float64, n)
	dr := make([]float64, n)
	out := NewBus(send.Len())
	var fl, fr SVF
	idx := 0
	for i := range send.L {
		in := float64(send.L[i]+send.R[i]) * 0.5
		ol, or := dl[idx], dr[idx]
		dl[idx] = fl.LP(in+or*feedback, 4500, 0.7)
		dr[idx] = fr.LP(ol*feedback, 4500, 0.7)
		out.L[i] = float32(ol)
		out.R[i] = float32(or)
		if idx++; idx == n {
			idx = 0
		}
	}
	return out
}

// ---- Dynamics ----

// Follower tracks a signal's envelope with separate attack and release times.
func Follower(x []float32, attack, release float64) []float32 {
	ga := math.Exp(-1 / (attack * sampleRate))
	gr := math.Exp(-1 / (release * sampleRate))
	env := make([]float32, len(x))
	v := 0.0
	for i, s := range x {
		a := math.Abs(float64(s))
		if a > v {
			v = ga*v + (1-ga)*a
		} else {
			v = gr*v + (1-gr)*a
		}
		env[i] = float32(v)
	}
	return env
}

// DuckCurve turns a key signal's envelope into a gain curve that dips by up to depthDB.
func DuckCurve(key []float32, threshold, depthDB float64) []float32 {
	env := Follower(key, 0.015, 0.35)
	floor := math.Pow(10, -depthDB/20)
	curve := make([]float32, len(key))
	for i, e := range env {
		amount := math.Min(float64(e)/threshold, 1)
		curve[i] = float32(1 - (1-floor)*amount)
	}
	return curve
}

// PumpCurve is a sidechain gain curve triggered by kick hits.
func PumpCurve(n int, hits []float64, depth, recover float64) []float32 {
	curve := make([]float32, n)
	for i := range curve {
		curve[i] = 1
	}
	for _, h := range hits {
		start := sample(h)
		for i := 0; i < sample(recover*4) && start+i < n; i++ {
			if start+i < 0 {
				continue
			}
			t := float64(i) / sampleRate
			// Quick 5 ms dip, then a smooth recovery.
			g := 1 - depth*math.Min(t/0.005, 1)*math.Exp(-t/recover)
			if float32(g) < curve[start+i] {
				curve[start+i] = float32(g)
			}
		}
	}
	return curve
}

// Compress is a feed-forward compressor on a mono signal (in place).
func Compress(x []float32, thresholdDB, ratio, makeupDB float64) {
	env := Follower(x, 0.004, 0.12)
	makeup := math.Pow(10, makeupDB/20)
	for i := range x {
		level := 20 * math.Log10(math.Max(float64(env[i]), 1e-6))
		gain := 0.0
		if level > thresholdDB {
			gain = (thresholdDB + (level-thresholdDB)/ratio) - level
		}
		x[i] = float32(float64(x[i]) * math.Pow(10, gain/20) * makeup)
	}
}

// Filter runs a static SVF over both channels of a bus in place.
func Filter(b *Bus, mode string, cutoff, q float64) {
	for _, ch := range [][]float32{b.L, b.R} {
		var f SVF
		for i, s := range ch {
			lp, _, hp := f.Process(float64(s), cutoff, q)
			if mode == "hp" {
				ch[i] = float32(hp)
			} else {
				ch[i] = float32(lp)
			}
		}
	}
}

// ---- Loudness (ITU-R BS.1770-4) ----

type biquad struct{ b0, b1, b2, a1, a2, z1, z2 float64 }

func (q *biquad) process(x float64) float64 {
	y := q.b0*x + q.z1
	q.z1 = q.b1*x - q.a1*y + q.z2
	q.z2 = q.b2*x - q.a2*y
	return y
}

func kWeighting() (biquad, biquad) {
	// Coefficients derived for any sample rate (as in libebur128).
	f0, gain, q := 1681.974450955533, 3.999843853973347, 0.7071752369554196
	k := math.Tan(math.Pi * f0 / sampleRate)
	vh := math.Pow(10, gain/20)
	vb := math.Pow(vh, 0.4996667741545416)
	a0 := 1 + k/q + k*k
	shelf := biquad{
		b0: (vh + vb*k/q + k*k) / a0, b1: 2 * (k*k - vh) / a0, b2: (vh - vb*k/q + k*k) / a0,
		a1: 2 * (k*k - 1) / a0, a2: (1 - k/q + k*k) / a0,
	}
	f0, q = 38.13547087602444, 0.5003270373238773
	k = math.Tan(math.Pi * f0 / sampleRate)
	a0 = 1 + k/q + k*k
	highpass := biquad{b0: 1, b1: -2, b2: 1, a1: 2 * (k*k - 1) / a0, a2: (1 - k/q + k*k) / a0}
	return shelf, highpass
}

// IntegratedLoudness returns gated integrated loudness in LUFS.
func IntegratedLoudness(b *Bus) float64 {
	weighted := func(ch []float32) []float64 {
		s, h := kWeighting()
		out := make([]float64, len(ch))
		for i, x := range ch {
			y := h.process(s.process(float64(x)))
			out[i] = y * y
		}
		return out
	}
	l, r := weighted(b.L), weighted(b.R)
	block, step := sample(0.4), sample(0.1)
	var powers []float64
	for start := 0; start+block <= len(l); start += step {
		sum := 0.0
		for i := start; i < start+block; i++ {
			sum += l[i] + r[i]
		}
		powers = append(powers, sum/float64(block))
	}
	lufs := func(p float64) float64 { return -0.691 + 10*math.Log10(p) }
	gated := func(threshold float64) float64 {
		sum, n := 0.0, 0
		for _, p := range powers {
			if lufs(p) > threshold {
				sum += p
				n++
			}
		}
		if n == 0 {
			return -70
		}
		return sum / float64(n)
	}
	relative := lufs(gated(-70)) - 10
	return lufs(gated(relative))
}

// ---- Limiter ----

// truePeak estimates inter-sample peaks with 4x Catmull-Rom interpolation.
func truePeak(ch []float32, i int) float64 {
	at := func(k int) float64 {
		if k < 0 || k >= len(ch) {
			return 0
		}
		return float64(ch[k])
	}
	p0, p1, p2, p3 := at(i-1), at(i), at(i+1), at(i+2)
	peak := math.Abs(p1)
	for _, t := range []float64{0.25, 0.5, 0.75} {
		v := 0.5 * (2*p1 + (-p0+p2)*t + (2*p0-5*p1+4*p2-p3)*t*t + (-p0+3*p1-3*p2+p3)*t*t*t)
		peak = math.Max(peak, math.Abs(v))
	}
	return peak
}

// Limit applies a look-ahead brick-wall limiter with the given ceiling (dBTP) in place.
func Limit(b *Bus, ceilingDB float64) {
	ceiling := math.Pow(10, ceilingDB/20)
	n := b.Len()
	need := make([]float64, n)
	for i := range need {
		p := math.Max(truePeak(b.L, i), truePeak(b.R, i))
		need[i] = 1
		if p > ceiling {
			need[i] = ceiling / p
		}
	}
	w := sample(0.005)
	// Future minimum over [i, i+w), then a w-long moving average: never exceeds need[i].
	fmin := make([]float64, n)
	deque := []int{}
	for i := n - 1; i >= 0; i-- {
		for len(deque) > 0 && need[deque[len(deque)-1]] >= need[i] {
			deque = deque[:len(deque)-1]
		}
		deque = append(deque, i)
		for deque[0] >= i+w {
			deque = deque[1:]
		}
		fmin[i] = need[deque[0]]
	}
	release := 1 - math.Exp(-1/(0.12*sampleRate))
	sum := 0.0
	g := 1.0
	for i := 0; i < n; i++ {
		sum += fmin[i]
		if i >= w {
			sum -= fmin[i-w]
		}
		avg := sum / float64(min(i+1, w))
		g = math.Min(avg, g+(1-g)*release)
		b.L[i] = float32(float64(b.L[i]) * g)
		b.R[i] = float32(float64(b.R[i]) * g)
	}
	for i := range b.L { // safety net for the few residual overs
		b.L[i] = float32(math.Max(-ceiling, math.Min(ceiling, float64(b.L[i]))))
		b.R[i] = float32(math.Max(-ceiling, math.Min(ceiling, float64(b.R[i]))))
	}
}
