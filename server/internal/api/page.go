package api

import (
	"bytes"
	"cmp"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
)

// mifPage is a mif's share page: the link sent through any messenger. It plays the clip
// with its lyrics in any browser, so a recipient needs neither MIFS nor the sender's music
// service, and its Open Graph tags give messengers a rich preview. "Listen on" links open
// the full song on whichever service the recipient uses.
func (s *server) mifPage(w http.ResponseWriter, r *http.Request) {
	mif, ok := s.loadMif(w, r)
	if !ok {
		return
	}
	song := mif.Song
	data := pageData{
		Mif:      mif,
		Duration: fmt.Sprintf("0:%02d", (mif.DurationMs+500)/1000),
	}
	if song.Artwork != nil {
		data.Artwork = song.Artwork.URL
	}
	data.Description = "A " + strings.TrimPrefix(data.Duration, "0:") + "-second mif on MIFS"
	if len(mif.Lyrics) > 0 {
		data.Description = "“" + mif.Lyrics[0].Text + "”"
	}
	names := map[string]string{"spotify": "Spotify", "deezer": "Deezer", "apple": "Apple Music"}
	has := map[string]bool{}
	for _, link := range song.Links {
		has[link.Provider] = true
		data.Listen = append(data.Listen, listenLink{Name: cmp.Or(names[link.Provider], link.Provider), URL: link.URL})
	}
	// Services MIFS has no link for get a search, so every recipient has a way in.
	term := url.QueryEscape(song.Title + " " + song.Artist)
	if !has["apple"] {
		data.Listen = append(data.Listen, listenLink{Name: "Apple Music", URL: "https://music.apple.com/search?term=" + term})
	}
	if !has["spotify"] {
		data.Listen = append(data.Listen, listenLink{Name: "Spotify", URL: "https://open.spotify.com/search/" + url.PathEscape(song.Title+" "+song.Artist)})
	}
	data.Listen = append(data.Listen, listenLink{Name: "YouTube Music", URL: "https://music.youtube.com/search?q=" + term})

	var body bytes.Buffer
	if err := pageTemplate.Execute(&body, data); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src * data:; media-src *; style-src 'unsafe-inline'; script-src 'unsafe-inline'")
	w.Write(body.Bytes())
}

type pageData struct {
	Mif         mifJSON
	Duration    string
	Artwork     string
	Description string
	Listen      []listenLink
}

type listenLink struct{ Name, URL string }

var pageTemplate = template.Must(template.New("mif").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Mif.Song.Title}} – {{.Mif.Song.Artist}} · MIFS</title>
<meta name="description" content="{{.Description}}">
<meta property="og:type" content="music.song">
<meta property="og:site_name" content="MIFS">
<meta property="og:title" content="{{.Mif.Song.Title}} – {{.Mif.Song.Artist}}">
<meta property="og:description" content="{{.Description}}">
<meta property="og:url" content="{{.Mif.URL}}">
{{with .Artwork}}<meta property="og:image" content="{{.}}">
<meta name="twitter:card" content="summary_large_image">{{end}}
<meta property="og:audio" content="{{.Mif.Audio.URL}}">
<meta property="og:audio:type" content="{{.Mif.Audio.ContentType}}">
<style>
:root { color-scheme: dark; --fg: #fff; --dim: rgba(255,255,255,.62); --line: rgba(255,255,255,.14); }
* { box-sizing: border-box; }
body { margin: 0; min-height: 100vh; font: 16px/1.4 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  color: var(--fg); background: #141019; display: grid; place-items: center; padding: 24px 16px; }
.backdrop { position: fixed; inset: 0; background-size: cover; background-position: center; filter: blur(60px) brightness(.45) saturate(1.3); transform: scale(1.2); z-index: -1; }
main { width: min(420px, 100%); text-align: center; }
.art { width: min(280px, 72vw); aspect-ratio: 1; border-radius: 22px; object-fit: cover; box-shadow: 0 18px 50px rgba(0,0,0,.5); background: var(--line); }
h1 { font-size: 1.5rem; margin: 22px 0 2px; }
.artist { color: var(--dim); margin: 0 0 22px; }
.player { display: flex; align-items: center; gap: 14px; justify-content: center; }
button { width: 68px; height: 68px; border-radius: 50%; border: 0; background: #fff; color: #141019; cursor: pointer; display: grid; place-items: center; }
button svg { width: 26px; height: 26px; }
.time { font-variant-numeric: tabular-nums; color: var(--dim); min-width: 3.5em; text-align: left; }
.bar { height: 4px; border-radius: 2px; background: var(--line); margin: 18px 0 20px; overflow: hidden; }
.bar i { display: block; height: 100%; width: 0; background: #fff; }
.lyrics { list-style: none; padding: 0; margin: 0 0 26px; font-size: 1.15rem; font-weight: 600; }
.lyrics li { color: var(--dim); transition: color .2s, transform .2s; padding: 3px 0; }
.lyrics li.now { color: var(--fg); transform: scale(1.04); }
.listen { color: var(--dim); font-size: .9rem; }
.listen a { color: var(--fg); text-decoration: none; border: 1px solid var(--line); border-radius: 999px; padding: 6px 12px; margin: 4px; display: inline-block; }
footer { margin-top: 26px; color: var(--dim); font-size: .8rem; }
</style>
</head>
<body>
<div class="backdrop"{{with .Artwork}} style="background-image:url('{{.}}')"{{end}}></div>
<main>
  {{if .Artwork}}<img class="art" src="{{.Artwork}}" alt="">{{else}}<div class="art" style="margin:auto"></div>{{end}}
  <h1>{{.Mif.Song.Title}}</h1>
  <p class="artist">{{.Mif.Song.Artist}}</p>
  <div class="player">
    <button id="play" aria-label="Play">
      <svg viewBox="0 0 24 24" fill="currentColor"><path id="icon" d="M8 5v14l11-7z"/></svg>
    </button>
    <span class="time" id="time">{{.Duration}}</span>
  </div>
  <div class="bar"><i id="progress"></i></div>
  <audio id="audio" src="{{.Mif.Audio.URL}}" preload="none" data-start="{{.Mif.StartMs}}"></audio>
  {{if .Mif.Lyrics}}<ul class="lyrics" hidden>{{range .Mif.Lyrics}}
    <li data-start="{{.StartMs}}" data-end="{{.EndMs}}">{{.Text}}</li>{{end}}
  </ul>{{end}}
  <div class="listen">Hear the whole song on<br>{{range .Listen}}<a href="{{.URL}}" rel="noopener">{{.Name}}</a>{{end}}</div>
  <footer>Shared with MIFS</footer>
</main>
<script>
(function () {
  var audio = document.getElementById("audio"), button = document.getElementById("play"),
      icon = document.getElementById("icon"), time = document.getElementById("time"),
      bar = document.getElementById("progress"), start = +audio.dataset.start,
      lines = [].slice.call(document.querySelectorAll(".lyrics li"));
  function render() {
    var lyrics = document.querySelector(".lyrics"), art = document.querySelector(".art");
    if (lyrics) { lyrics.hidden = audio.paused; if (art) art.hidden = !audio.paused; }
    var t = audio.currentTime, d = audio.duration || 0, song = start + t * 1000;
    bar.style.width = d ? (100 * t / d) + "%" : "0";
    var left = Math.max(0, Math.ceil(d - t - 0.1)); // AAC padding makes 10 s read as 10.005 s
    time.textContent = "0:" + (left < 10 ? "0" : "") + left;
    lines.forEach(function (li) { li.classList.toggle("now", !audio.paused && song >= +li.dataset.start && song < +li.dataset.end); });
    icon.setAttribute("d", audio.paused ? "M8 5v14l11-7z" : "M6 5h4v14H6zM14 5h4v14h-4z");
    button.setAttribute("aria-label", audio.paused ? "Play" : "Pause");
  }
  button.onclick = function () { audio.paused ? audio.play() : audio.pause(); };
  ["timeupdate", "play", "pause", "ended", "loadedmetadata"].forEach(function (e) { audio.addEventListener(e, render); });
})();
</script>
</body>
</html>
`))
