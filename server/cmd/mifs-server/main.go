// Command mifs-server serves the MIFS song catalog and imports songs into it.
//
//	mifs-server serve               start the HTTP API and ingestion workers (default)
//	mifs-server ingest <dir>...     import song folders (see internal/ingest)
//
// Configuration comes from flags or MIFS_* environment variables; run with -h for details.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/christienetto/mifs/server/internal/api"
	"github.com/christienetto/mifs/server/internal/audio"
	"github.com/christienetto/mifs/server/internal/blob"
	"github.com/christienetto/mifs/server/internal/catalog"
	"github.com/christienetto/mifs/server/internal/clip"
	"github.com/christienetto/mifs/server/internal/discovery"
	"github.com/christienetto/mifs/server/internal/ingest"
	"github.com/christienetto/mifs/server/internal/lyrics"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	command, args := "serve", os.Args[1:]
	if len(args) > 0 && (args[0] == "serve" || args[0] == "ingest") {
		command, args = args[0], args[1:]
	}

	var err error
	switch command {
	case "serve":
		err = serve(logger, args)
	case "ingest":
		err = importSongs(logger, args)
	}
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		logger.Error(command+" failed", "err", err)
		os.Exit(1)
	}
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func openData(dir string) (*catalog.Store, *blob.Store, error) {
	blobs, err := blob.Open(filepath.Join(dir, "media"))
	if err != nil {
		return nil, nil, err
	}
	store, err := catalog.Open(filepath.Join(dir, "mifs.db"))
	return store, blobs, err
}

func serve(logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := flags.String("addr", env("MIFS_ADDR", "127.0.0.1:8080"), "listen address; use :8080 to accept LAN devices (MIFS_ADDR)")
	data := flags.String("data", env("MIFS_DATA_DIR", "data"), "directory holding mifs.db and media/ (MIFS_DATA_DIR)")
	publicURL := flags.String("public-url", env("MIFS_PUBLIC_URL", ""), "external origin, e.g. https://api.example.com; default: the request's host (MIFS_PUBLIC_URL)")
	mediaURL := flags.String("media-url", env("MIFS_MEDIA_URL", ""), "base URL media is published at, e.g. a CDN; default: <public-url>/media (MIFS_MEDIA_URL)")
	ffmpeg := flags.String("ffmpeg", env("MIFS_FFMPEG", "ffmpeg"), "ffmpeg binary, for mifs and ingestion (MIFS_FFMPEG)")
	ffprobe := flags.String("ffprobe", env("MIFS_FFPROBE", "ffprobe"), "ffprobe binary (MIFS_FFPROBE)")
	workers := flags.Int("workers", envInt("MIFS_WORKERS", 2), "songs ingested at once; 0 disables ingestion here (MIFS_WORKERS)")
	discoveryNames := flags.String("discovery", env("MIFS_DISCOVERY", "spotify"), "discovery providers to search, in order; spotify needs MIFS_SPOTIFY_CLIENT_ID/SECRET (MIFS_DISCOVERY)")
	lyricsNames := flags.String("lyrics", env("MIFS_LYRICS", "lrclib"), "synced lyrics providers, in order (MIFS_LYRICS)")
	libraryDir := flags.String("library", env("MIFS_LIBRARY_DIR", ""), "audio source: a folder of audio files you own (MIFS_LIBRARY_DIR)")
	audioCommand := flags.String("audio-command", env("MIFS_AUDIO_COMMAND", ""), "audio source: a shell command that fetches a song's audio (MIFS_AUDIO_COMMAND; see internal/audio/command.go)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	store, blobs, err := openData(*data)
	if err != nil {
		return err
	}
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	providers, err := discoveryProviders(*discoveryNames, logger)
	if err != nil {
		return err
	}
	lyricProviders, err := lyricsProviders(*lyricsNames)
	if err != nil {
		return err
	}
	var sources []audio.Source
	if *libraryDir != "" {
		library := &audio.Library{Dir: *libraryDir, FFprobe: *ffprobe, IndexPath: filepath.Join(*data, "library-index.json"), Logger: logger}
		go func() {
			if err := library.Refresh(ctx); err != nil {
				logger.Error("library scan failed", "dir", *libraryDir, "err", err)
			}
		}()
		sources = append(sources, library)
	}
	if *audioCommand != "" {
		sources = append(sources, &audio.Command{Script: *audioCommand})
	}
	if *audioCommand == "" {
		defaultBinary := "spotdl"
		if info, err := os.Stat(".venv/bin/spotdl"); err == nil && !info.IsDir() {
			defaultBinary = ".venv/bin/spotdl"
		}
		binary := env("MIFS_SPOTDL", defaultBinary)
		if _, err := exec.LookPath(binary); err != nil {
			logger.Warn("spotdl not installed: new Spotify songs cannot be downloaded", "binary", binary)
		}
		sources = append(sources, &audio.SpotDL{Binary: binary, CookieFile: env("MIFS_SPOTDL_COOKIE_FILE", "")})
	}
	if _, err := exec.LookPath(*ffmpeg); err != nil {
		logger.Warn("ffmpeg not found: making mifs and ingesting songs will fail", "ffmpeg", *ffmpeg)
	}

	worker := &ingest.Worker{
		Ingester:    &ingest.Ingester{Store: store, Blobs: blobs, FFmpeg: *ffmpeg, FFprobe: *ffprobe},
		Sources:     sources,
		Lyrics:      lyricProviders,
		Discovery:   providers,
		Logger:      logger,
		Concurrency: *workers,
	}
	handler, err := api.New(api.Config{
		Store: store, MediaDir: blobs.Dir(), PublicURL: *publicURL, MediaURL: *mediaURL, Logger: logger,
		Discovery: providers,
		IOSAppID:  env("MIFS_IOS_APP_ID", ""),
		Clips:     &clip.Renderer{Blobs: blobs, FFmpeg: *ffmpeg},
		Notify:    worker.Notify,
	})
	if err != nil {
		return err
	}

	count, err := store.Count(ctx)
	if err != nil {
		return err
	}
	if count == 0 {
		logger.Warn("catalog is empty; import songs with: mifs-server ingest seed")
	}
	if *workers > 0 {
		go worker.Run(ctx)
	}
	logger.Info("sources", "discovery", names(providers), "lyrics", names(lyricProviders), "audio", names(sources), "workers", *workers)

	server := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	logger.Info("MIFS server listening", "addr", *addr, "songs", count, "data", *data)

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}

func envInt(key string, fallback int) int {
	var n int
	if _, err := fmt.Sscan(env(key, ""), &n); err != nil {
		return fallback
	}
	return n
}

func list(value string) []string {
	var out []string
	for _, name := range strings.Split(value, ",") {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func discoveryProviders(value string, logger *slog.Logger) ([]discovery.Provider, error) {
	var providers []discovery.Provider
	for _, name := range list(value) {
		switch name {
		case "deezer":
			providers = append(providers, &discovery.Deezer{})
		case "spotify":
			id, secret := env("MIFS_SPOTIFY_CLIENT_ID", ""), env("MIFS_SPOTIFY_CLIENT_SECRET", "")
			if id == "" || secret == "" {
				logger.Info("spotify discovery off: set MIFS_SPOTIFY_CLIENT_ID and MIFS_SPOTIFY_CLIENT_SECRET to enable it")
				continue
			}
			providers = append(providers, &discovery.Spotify{ClientID: id, ClientSecret: secret, Market: env("MIFS_SPOTIFY_MARKET", "")})
		default:
			return nil, fmt.Errorf("unknown discovery provider %q (have: spotify, deezer)", name)
		}
	}
	return providers, nil
}

func lyricsProviders(value string) ([]lyrics.Provider, error) {
	var providers []lyrics.Provider
	for _, name := range list(value) {
		switch name {
		case "lrclib":
			providers = append(providers, &lyrics.LRCLIB{})
		default:
			return nil, fmt.Errorf("unknown lyrics provider %q (have: lrclib)", name)
		}
	}
	return providers, nil
}

func names[T interface{ Name() string }](items []T) string {
	var out []string
	for _, item := range items {
		out = append(out, item.Name())
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ",")
}

func importSongs(logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("ingest", flag.ContinueOnError)
	data := flags.String("data", env("MIFS_DATA_DIR", "data"), "directory holding mifs.db and media/ (MIFS_DATA_DIR)")
	ffmpeg := flags.String("ffmpeg", env("MIFS_FFMPEG", "ffmpeg"), "ffmpeg binary (MIFS_FFMPEG)")
	ffprobe := flags.String("ffprobe", env("MIFS_FFPROBE", "ffprobe"), "ffprobe binary (MIFS_FFPROBE)")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: mifs-server ingest [flags] <song folder or folder of song folders>...")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() == 0 {
		flags.Usage()
		return flag.ErrHelp
	}

	store, blobs, err := openData(*data)
	if err != nil {
		return err
	}
	defer store.Close()
	ingester := &ingest.Ingester{Store: store, Blobs: blobs, FFmpeg: *ffmpeg, FFprobe: *ffprobe}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var failed int
	for _, root := range flags.Args() {
		dirs, err := ingest.SongDirs(root)
		if err != nil {
			return err
		}
		for _, dir := range dirs {
			start := time.Now()
			song, err := ingester.Ingest(ctx, dir)
			if err != nil {
				failed++
				logger.Error("ingest failed", "dir", dir, "err", err)
				continue
			}
			logger.Info("ingested", "id", song.ID, "title", song.Title, "artist", song.Artist,
				"duration", time.Duration(song.DurationMs)*time.Millisecond, "lyrics", song.HasLyrics,
				"took", time.Since(start).Round(time.Millisecond))
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d song(s) failed to import", failed)
	}
	return nil
}
