package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"davinci/internal/doc"
	"davinci/internal/render"
	"davinci/web"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// DefaultPort is where davinci listens. 7788 is taken by an older local tool.
const DefaultPort = 7789

// Server wires the store, the command engines, the renderer and the HTTP
// routes together. It owns every document: commands are applied here.
type Server struct {
	port    int
	dataDir string
	// boot identifies this process (stamped into the pages it serves, and
	// reported by /api/health).
	boot   string
	store  *Store
	assets *AssetStore
	fonts  *FontStore
	hub    *Hub
	eng    engines
	// renderer measures text and draws exports (a Node process, on demand).
	renderer *render.Sidecar

	quiet bool

	// fontPrefsMu serialises changes to the favourite / hidden font lists.
	fontPrefsMu sync.Mutex

	e    *echo.Echo
	srv  *http.Server
	done chan struct{}
}

// Options configures a Server.
type Options struct {
	Port    int
	DataDir string
	Quiet   bool
	// Remote marks a server reached over the network (behind a reverse
	// proxy): asset imports may not read local paths or private addresses.
	Remote bool
}

// NewServer builds the server, opening the data directory. It does not listen.
func NewServer(opt Options) (*Server, error) {
	if opt.DataDir == "" {
		opt.DataDir = DefaultDataDir()
	}
	if opt.Port <= 0 {
		opt.Port = DefaultPort
	}
	store, err := OpenStore(opt.DataDir)
	if err != nil {
		return nil, err
	}
	s := &Server{
		port:    opt.Port,
		dataDir: opt.DataDir,
		store:   store,
		assets:  &AssetStore{dir: filepath.Join(opt.DataDir, "assets"), db: store, remote: opt.Remote},
		fonts:   NewFontStore(opt.DataDir),
		hub:     newHub(nil),
		eng:     engines{byID: map[string]*doc.Engine{}, thumbs: map[string]*time.Timer{}},
		quiet:   opt.Quiet,
		done:    make(chan struct{}),
	}
	if id, err := newID("boot"); err == nil {
		s.boot = id
	} else {
		// The boot id only needs to be unique per process on this machine, so a
		// clock reading is an acceptable stand-in if the random source fails.
		s.boot = fmt.Sprintf("boot_%d", time.Now().UnixNano())
	}
	s.hub.srv = s
	s.renderer = render.New(opt.DataDir, s.Addr(), s.logf)
	s.build()
	return s, nil
}

// DataDir is the directory holding the database, assets and fonts.
func (s *Server) DataDir() string { return s.dataDir }

// BootID identifies this server process; pages served by it carry it.
func (s *Server) BootID() string { return s.boot }

// Port is the listening port.
func (s *Server) Port() int { return s.port }

// Addr is the base URL of this server.
func (s *Server) Addr() string { return fmt.Sprintf("http://127.0.0.1:%d", s.port) }

// Store exposes the SQLite store (used by the CLI's in-process commands).
func (s *Server) Store() *Store { return s.store }

// Hub exposes the WebSocket rooms.
func (s *Server) Hub() *Hub { return s.hub }

// Assets exposes the asset store.
func (s *Server) Assets() *AssetStore { return s.assets }

// Fonts exposes the font store.
func (s *Server) Fonts() *FontStore { return s.fonts }

func (s *Server) logf(format string, args ...any) {
	if s.quiet {
		return
	}
	log.Printf(format, args...)
}

// build assembles the Echo application and routes.
func (s *Server) build() {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	if s.quiet {
		e.Logger.SetOutput(io.Discard)
	} else {
		e.Logger.SetOutput(os.Stderr)
	}
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"}, // the Vite dev server runs on another port
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders: []string{"*"},
	}))

	s.registerAPI(e)
	s.registerStatic(e)

	s.e = e
	s.srv = &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", s.port), Handler: e}
}

// registerStatic serves assets, user fonts, the WebSocket and the frontend.
func (s *Server) registerStatic(e *echo.Echo) {
	e.GET("/ws", func(c echo.Context) error {
		s.hub.ServeWS(c.Response(), c.Request())
		return nil
	})

	e.GET("/assets/*", func(c echo.Context) error {
		ref := "/assets/" + c.Param("*")
		path, err := s.assets.Path(ref)
		if err != nil {
			return echo.ErrNotFound
		}
		w, r := c.Response(), c.Request()
		setCacheHeaders(w)
		http.ServeFile(w, r, path)
		return nil
	})

	e.GET("/fonts/*", func(c echo.Context) error {
		name := filepath.Base(c.Param("*"))
		if name == "" || name == "." || name == "/" {
			return echo.ErrNotFound
		}
		path := filepath.Join(s.dataDir, "fonts", name)
		if !within(s.dataDir, path) {
			return echo.ErrNotFound
		}
		if _, err := os.Stat(path); err != nil {
			return echo.ErrNotFound
		}
		setCacheHeaders(c.Response())
		http.ServeFile(c.Response(), c.Request(), path)
		return nil
	})

	if !web.Built() {
		e.Any("/*", func(c echo.Context) error {
			return c.HTML(http.StatusOK, noFrontendPage(s.port))
		})
		return
	}
	h, err := web.Handler(s.boot)
	if err != nil {
		e.Logger.Errorf("frontend: %v", err)
		return
	}
	e.Any("/*", func(c echo.Context) error {
		h.ServeHTTP(c.Response(), c.Request())
		return nil
	})
}

func setCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
}

// within reports whether target sits under root (guards against ../ escapes).
func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Start begins listening. It returns once the listener is up (or has failed),
// leaving the server running in the background.
func (s *Server) Start() error {
	errCh := make(chan error, 1)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// After Start returns there is nowhere to report this, so log it.
			s.logf("[davinci] listener stopped: %v", err)
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-time.After(200 * time.Millisecond):
		// No immediate failure: assume the port was free and the listener is up.
		s.logf("[davinci] listening on %s (data: %s)", s.Addr(), s.dataDir)
		return nil
	}
}

// Shutdown stops the listener, stops the renderer and closes the database.
func (s *Server) Shutdown(ctx context.Context) error {
	err := s.srv.Shutdown(ctx)
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	s.renderer.Close()
	if cerr := s.store.Close(); err == nil {
		err = cerr
	}
	return err
}

// Done is closed when the server has shut down.
func (s *Server) Done() <-chan struct{} { return s.done }
