package main

import (
	"image"
	"math"
)

// Canvas is a linear float RGB image drawn with anti-aliased signed-distance shapes.
type Canvas struct {
	W, H int
	Pix  []RGB
}

type RGB struct{ R, G, B float64 }

func hex(v uint32) RGB {
	return RGB{float64(v>>16&0xff) / 255, float64(v>>8&0xff) / 255, float64(v&0xff) / 255}
}

func (c RGB) Mix(o RGB, t float64) RGB {
	return RGB{c.R + (o.R-c.R)*t, c.G + (o.G-c.G)*t, c.B + (o.B-c.B)*t}
}

func (c RGB) Scale(k float64) RGB { return RGB{c.R * k, c.G * k, c.B * k} }
func (c RGB) Add(o RGB) RGB       { return RGB{c.R + o.R, c.G + o.G, c.B + o.B} }

func NewCanvas(size int) *Canvas {
	return &Canvas{W: size, H: size, Pix: make([]RGB, size*size)}
}

// Shade sets every pixel from f(u, v) with u, v in 0...1.
func (c *Canvas) Shade(f func(u, v float64) RGB) {
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			c.Pix[y*c.W+x] = f((float64(x)+0.5)/float64(c.W), (float64(y)+0.5)/float64(c.H))
		}
	}
}

// Fill blends col over pixels by the coverage of a shape given as a signed distance in
// normalised units (negative inside). Only the bounding box [x0,x1]×[y0,y1] is visited.
func (c *Canvas) Fill(x0, y0, x1, y1 float64, sdf func(u, v float64) float64, col func(u, v float64) RGB, alpha float64) {
	px := 1 / float64(c.W)
	minX, maxX := max(0, int(x0*float64(c.W))-2), min(c.W, int(x1*float64(c.W))+2)
	minY, maxY := max(0, int(y0*float64(c.H))-2), min(c.H, int(y1*float64(c.H))+2)
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			u, v := (float64(x)+0.5)/float64(c.W), (float64(y)+0.5)/float64(c.H)
			cov := math.Min(math.Max(0.5-sdf(u, v)/px, 0), 1) * alpha
			if cov <= 0 {
				continue
			}
			i := y*c.W + x
			c.Pix[i] = c.Pix[i].Mix(col(u, v), cov)
		}
	}
}

// Glow adds light that falls off with distance from a shape.
func (c *Canvas) Glow(sdf func(u, v float64) float64, col RGB, radius, strength float64) {
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			u, v := (float64(x)+0.5)/float64(c.W), (float64(y)+0.5)/float64(c.H)
			d := math.Max(sdf(u, v), 0)
			k := strength * math.Exp(-d/radius)
			if math.Abs(k) > 0.002 {
				i := y*c.W + x
				c.Pix[i] = c.Pix[i].Add(col.Scale(k))
			}
		}
	}
}

// Finish adds film grain and a vignette.
func (c *Canvas) Finish(grain, vignette float64, seed uint32) {
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			u, v := (float64(x)+0.5)/float64(c.W)-0.5, (float64(y)+0.5)/float64(c.H)-0.5
			i := y*c.W + x
			n := (hash2(uint32(x), uint32(y), seed) - 0.5) * grain
			vig := 1 - vignette*math.Pow(math.Sqrt(u*u+v*v)*1.35, 2.2)
			c.Pix[i] = c.Pix[i].Scale(vig).Add(RGB{n, n, n})
		}
	}
}

func (c *Canvas) Image() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, c.W, c.H))
	to8 := func(v float64) uint8 { return uint8(math.Max(0, math.Min(255, math.Round(v*255)))) }
	for i, p := range c.Pix {
		img.Pix[i*4+0] = to8(p.R)
		img.Pix[i*4+1] = to8(p.G)
		img.Pix[i*4+2] = to8(p.B)
		img.Pix[i*4+3] = 255
	}
	return img
}

// hash2 is a deterministic 0...1 hash of a lattice point.
func hash2(x, y, seed uint32) float64 {
	h := x*374761393 + y*668265263 + seed*2246822519
	h = (h ^ (h >> 13)) * 1274126177
	h ^= h >> 16
	return float64(h&0xffffff) / float64(0xffffff)
}

// valueNoise is smooth 2D noise in 0...1.
func valueNoise(u, v float64, seed uint32) float64 {
	x0, y0 := math.Floor(u), math.Floor(v)
	fx, fy := u-x0, v-y0
	sx, sy := fx*fx*(3-2*fx), fy*fy*(3-2*fy)
	h := func(dx, dy float64) float64 { return hash2(uint32(int32(x0+dx)), uint32(int32(y0+dy)), seed) }
	top := h(0, 0) + (h(1, 0)-h(0, 0))*sx
	bottom := h(0, 1) + (h(1, 1)-h(0, 1))*sx
	return top + (bottom-top)*sy
}

func fbm(u, v float64, seed uint32) float64 {
	sum, amp, norm := 0.0, 0.5, 0.0
	for o := 0; o < 5; o++ {
		sum += valueNoise(u, v, seed+uint32(o)) * amp
		norm += amp
		u, v, amp = u*2.03, v*2.03, amp*0.5
	}
	return sum / norm
}

func smoothstep(a, b, x float64) float64 {
	t := math.Min(math.Max((x-a)/(b-a), 0), 1)
	return t * t * (3 - 2*t)
}

// ---- Signed distance functions (normalised units) ----

func sdCircle(u, v, cx, cy, r float64) float64 { return math.Hypot(u-cx, v-cy) - r }

func sdRoundBox(u, v, cx, cy, hw, hh, r float64) float64 {
	qx, qy := math.Abs(u-cx)-hw+r, math.Abs(v-cy)-hh+r
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
}

func sdSegment(u, v, ax, ay, bx, by, r float64) float64 {
	px, py, dx, dy := u-ax, v-ay, bx-ax, by-ay
	h := math.Min(math.Max((px*dx+py*dy)/(dx*dx+dy*dy), 0), 1)
	return math.Hypot(px-dx*h, py-dy*h) - r
}

// sdPolygon is the signed distance to a closed polygon.
func sdPolygon(u, v float64, pts [][2]float64) float64 {
	d := math.Inf(1)
	sign := 1.0
	for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
		ex, ey := pts[j][0]-pts[i][0], pts[j][1]-pts[i][1]
		wx, wy := u-pts[i][0], v-pts[i][1]
		h := math.Min(math.Max((wx*ex+wy*ey)/(ex*ex+ey*ey), 0), 1)
		d = math.Min(d, math.Hypot(wx-ex*h, wy-ey*h))
		c1, c2, c3 := v >= pts[i][1], v < pts[j][1], ex*wy > ey*wx
		if (c1 && c2 && c3) || (!c1 && !c2 && !c3) {
			sign = -sign
		}
	}
	return sign * d
}

func bounds(pts [][2]float64) (x0, y0, x1, y1 float64) {
	x0, y0, x1, y1 = 1, 1, 0, 0
	for _, p := range pts {
		x0, y0 = math.Min(x0, p[0]), math.Min(y0, p[1])
		x1, y1 = math.Max(x1, p[0]), math.Max(y1, p[1])
	}
	return
}

func solid(c RGB) func(u, v float64) RGB { return func(float64, float64) RGB { return c } }
