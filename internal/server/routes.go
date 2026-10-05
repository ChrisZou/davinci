package server

import (
	"davinci/internal/doc"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// registerAPI mounts every REST route. The API is intentionally local-only: it
// binds 127.0.0.1 and is the surface AI (CLI, scripts, agents) drives.
func (s *Server) registerAPI(e *echo.Echo) {
	api := e.Group("/api")

	api.GET("/health", s.handleHealth)
	api.GET("/schema", s.handleSchema)
	api.GET("/canvas-presets", s.handleCanvasPresets)
	api.GET("/fonts", s.handleListFonts)
	api.GET("/fonts/face", s.handleFontFace)
	api.PUT("/fonts/prefs", s.handleFontPrefs)
	api.POST("/fonts", s.handleUploadFont)
	api.POST("/assets", s.handleUploadAsset)

	api.GET("/projects", s.handleListProjects)
	api.POST("/projects", s.handleCreateProject)

	api.GET("/projects/:id", s.handleGetProject)
	api.PUT("/projects/:id", s.handlePutProject)
	api.PATCH("/projects/:id", s.handlePatchProject)
	api.GET("/projects/:id/thumbnail", s.handleThumbnail)
	api.DELETE("/projects/:id", s.handleDeleteProject)
	api.POST("/projects/:id/commands", s.handleCommands)
	api.GET("/projects/:id/layers", s.handleLayers)
	api.GET("/projects/:id/boards", s.handleBoards)
	api.GET("/projects/:id/export.:ext", s.handleExport)

	s.registerLibrary(api)
	s.registerTemplates(api)
}

// --- health & metadata ---

func (s *Server) handleHealth(c echo.Context) error {
	projects, err := s.store.ListProjects(ProjectQuery{})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"ok":       true,
		"port":     s.port,
		"dataDir":  s.dataDir,
		"boot":     s.boot,
		"projects": len(projects),
	})
}

func (s *Server) handleSchema(c echo.Context) error {
	return c.JSON(http.StatusOK, doc.Schema())
}

func (s *Server) handleCanvasPresets(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "presets": Presets()})
}

func (s *Server) handleListFonts(c echo.Context) error {
	if c.QueryParam("rescan") != "" {
		if err := s.fonts.Scan(); err != nil {
			return errJSON(c, err)
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "fonts": s.fontList()})
}

// handleFontPrefs stars or hides a font: {"family":"…","favorite":true} or {"family":"…","hidden":true}.
func (s *Server) handleFontPrefs(c echo.Context) error {
	var req struct {
		Family   string `json:"family"`
		Favorite *bool  `json:"favorite"`
		Hidden   *bool  `json:"hidden"`
	}
	if err := c.Bind(&req); err != nil || strings.TrimSpace(req.Family) == "" {
		return badRequest(c, `give {"family": "...", "favorite": true|false} and/or "hidden"`)
	}
	if err := s.setFontPref(strings.TrimSpace(req.Family), req.Favorite, req.Hidden); err != nil {
		return errJSON(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "fonts": s.fontList()})
}

// --- projects ---

// handleListProjects lists works by default: ?kind=template lists the template
// library and ?kind=all both; ?q= and ?tag= narrow either.
func (s *Server) handleListProjects(c echo.Context) error {
	q := ProjectQuery{Kind: KindDesign, Q: c.QueryParam("q"), Tag: c.QueryParam("tag")}
	switch k := c.QueryParam("kind"); k {
	case "", KindDesign:
	case KindTemplate:
		q.Kind = KindTemplate
	case "all":
		q.Kind = ""
	default:
		return badRequest(c, `kind must be "design", "template" or "all"`)
	}
	list, err := s.store.ListProjects(q)
	if err != nil {
		return errJSON(c, err)
	}
	// Canvas sizes live inside the document, which is the authority once an
	// editor has touched the project; fall back to the columns otherwise.
	for i := range list {
		if w, h := canvasSizeOf(list[i].doc(s)); w > 0 && h > 0 {
			list[i].Width, list[i].Height = w, h
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "projects": list})
}

type createProjectReq struct {
	Name   string `json:"name"`
	Preset string `json:"preset"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	ProjectMeta
}

func (s *Server) handleCreateProject(c echo.Context) error {
	var req createProjectReq
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid JSON body: "+err.Error())
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "未命名设计"
	}
	w, h := req.Width, req.Height
	if p := Preset(strings.TrimSpace(req.Preset)); p != nil {
		w, h = p.Width, p.Height
	}
	if w <= 0 || h <= 0 {
		p := Preset("xhs-3-4")
		w, h = p.Width, p.Height
	}
	doc, err := blankDocument(w, h)
	if err != nil {
		return errJSON(c, err)
	}
	p, err := s.store.NewProject(name, w, h, doc)
	if err != nil {
		return errJSON(c, err)
	}
	if err := s.store.SetProjectMeta(p.ID, req.ProjectMeta); err != nil {
		_ = s.store.DeleteProject(p.ID)
		return badRequest(c, err.Error())
	}
	if p, err = s.store.GetProject(p.ID); err != nil {
		return errJSON(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"ok": true, "project": p, "editorURL": s.Addr() + "/editor/" + p.ID})
}

func (s *Server) handleGetProject(c echo.Context) error {
	p, err := s.store.GetProject(c.Param("id"))
	if err != nil {
		return errJSON(c, err)
	}
	if p == nil {
		return c.JSON(http.StatusNotFound, map[string]any{"ok": false, "error": "project not found"})
	}
	w, h := canvasSizeOf(p.Document)
	p.Width, p.Height = firstNonZero(w, p.Width), firstNonZero(h, p.Height)
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "project": p})
}

func (s *Server) handlePutProject(c echo.Context) error {
	id := c.Param("id")
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 64<<20))
	if err != nil {
		return badRequest(c, err.Error())
	}
	if len(body) == 0 || !json.Valid(body) {
		return badRequest(c, "body must be a valid davinci document JSON")
	}
	if p, _ := s.store.GetProject(id); p == nil {
		return c.JSON(http.StatusNotFound, map[string]any{"ok": false, "error": "project not found"})
	}
	if err := s.ReplaceDocument(id, body); err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "id": id})
}

type patchProjectReq struct {
	Name *string `json:"name"`
	ProjectMeta
}

// handlePatchProject changes a project's metadata — its name, which library it
// is in (kind), tags, note and link — without touching the document, which
// only commands change.
func (s *Server) handlePatchProject(c echo.Context) error {
	id := c.Param("id")
	var req patchProjectReq
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid JSON body: "+err.Error())
	}
	if p, _ := s.store.GetProject(id); p == nil {
		return c.JSON(http.StatusNotFound, map[string]any{"ok": false, "error": "project not found"})
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return badRequest(c, "name is empty")
		}
		if err := s.store.RenameProject(id, name); err != nil {
			return errJSON(c, err)
		}
	}
	if err := s.store.SetProjectMeta(id, req.ProjectMeta); err != nil {
		return badRequest(c, err.Error())
	}
	p, err := s.store.GetProject(id)
	if err != nil {
		return errJSON(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "project": map[string]any{
		"id": p.ID, "name": p.Name, "kind": p.Kind, "tags": p.Tags, "note": p.Note, "link": p.Link,
	}})
}

// handleThumbnail serves the thumbnail the editor last saved as an image, so
// the project list can use plain <img> tags (and the browser cache, keyed by
// the ?rev= the list adds) instead of shipping every data URL in one listing.
func (s *Server) handleThumbnail(c echo.Context) error {
	p, err := s.store.GetProject(c.Param("id"))
	if err != nil {
		return errJSON(c, err)
	}
	if p == nil || p.Thumbnail == "" {
		return c.NoContent(http.StatusNotFound)
	}
	data, err := decodeDataURL(p.Thumbnail)
	if err != nil {
		return c.NoContent(http.StatusNotFound)
	}
	mime := http.DetectContentType(data)
	c.Response().Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	return c.Blob(http.StatusOK, mime, data)
}

func (s *Server) handleDeleteProject(c echo.Context) error {
	if err := s.store.DeleteProject(c.Param("id")); err != nil {
		return errJSON(c, err)
	}
	s.forget(c.Param("id"))
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

// --- commands (the AI path) ---

type commandsReq struct {
	Commands []json.RawMessage `json:"commands"`
}

func (s *Server) handleCommands(c echo.Context) error {
	id := c.Param("id")
	if p, _ := s.store.GetProject(id); p == nil {
		return c.JSON(http.StatusNotFound, map[string]any{"ok": false, "error": "project not found"})
	}
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 16<<20))
	if err != nil {
		return badRequest(c, err.Error())
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return badRequest(c, "empty body")
	}

	var res *Result
	switch {
	case strings.HasPrefix(trimmed, "["):
		var raw []json.RawMessage
		if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
			return badRequest(c, "commands must be a JSON array of command objects")
		}
		res, err = s.ExecBatch(id, raw)
	case strings.HasPrefix(trimmed, "{"):
		// Either {"commands":[...]} or a single command object.
		var wrapper commandsReq
		if err := json.Unmarshal([]byte(trimmed), &wrapper); err == nil && len(wrapper.Commands) > 0 {
			res, err = s.ExecBatch(id, wrapper.Commands)
			break
		}
		res, err = s.Exec(id, json.RawMessage(trimmed))
	default:
		return badRequest(c, "body must be a JSON object or array")
	}
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]any{"ok": false, "error": err.Error()})
	}
	if !res.OK {
		msg := res.Error
		if msg == "" {
			msg = "command failed"
		}
		return c.JSON(http.StatusBadRequest, map[string]any{"ok": false, "error": msg, "document": res.Document})
	}
	return c.JSON(http.StatusOK, res)
}

func (s *Server) handleLayers(c echo.Context) error {
	doc, _, err := s.loadDocument(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
	}
	rows, err := LayerRows(doc, strings.TrimSpace(c.QueryParam("board")))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "layers": rows})
}

func (s *Server) handleBoards(c echo.Context) error {
	doc, _, err := s.loadDocument(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "boards": Boards(doc)})
}

// --- export ---

func (s *Server) handleExport(c echo.Context) error {
	id := c.Param("id")
	ext := strings.ToLower(strings.TrimPrefix(c.Param("ext"), "."))
	if ext == "" {
		ext = "png"
	}
	scale := 1.0
	if v := strings.TrimSpace(c.QueryParam("scale")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 || f > 8 {
			return badRequest(c, "scale must be a number between 0 and 8")
		}
		scale = f
	}
	export := map[string]any{"type": "export", "format": ext, "multiplier": scale}
	// ?board= renders one board of the project; without it, the active one.
	if b := strings.TrimSpace(c.QueryParam("board")); b != "" {
		export["board"] = b
	}
	cmd, err := json.Marshal(export)
	if err != nil {
		return errJSON(c, err)
	}
	res, err := s.Exec(id, cmd)
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]any{"ok": false, "error": err.Error()})
	}
	if !res.OK {
		msg := res.Error
		if msg == "" {
			msg = "export failed"
		}
		return c.JSON(http.StatusBadRequest, map[string]any{"ok": false, "error": msg})
	}
	var out struct {
		DataURL string `json:"dataURL"`
		Width   int    `json:"width"`
		Height  int    `json:"height"`
		Format  string `json:"format"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		return errJSON(c, fmt.Errorf("unexpected export payload: %w", err))
	}
	raw, err := decodeDataURL(out.DataURL)
	if err != nil {
		return errJSON(c, err)
	}
	ct := "image/png"
	switch ext {
	case "jpg", "jpeg":
		ct = "image/jpeg"
	case "webp":
		ct = "image/webp"
	}
	return c.Blob(http.StatusOK, ct, raw)
}

// decodeDataURL turns "data:image/png;base64,...." into bytes.
func decodeDataURL(s string) ([]byte, error) {
	i := strings.Index(s, ",")
	if i < 0 || !strings.HasPrefix(s, "data:") {
		return nil, fmt.Errorf("not a data URL")
	}
	if !strings.Contains(s[:i], "base64") {
		return nil, fmt.Errorf("only base64 data URLs are supported")
	}
	return base64.StdEncoding.DecodeString(s[i+1:])
}

// --- uploads ---

func (s *Server) handleUploadAsset(c echo.Context) error {
	// A "url" field imports a remote file, a "path" field imports a local one,
	// and otherwise a multipart "file" uploads bytes. The browser cannot read the
	// local filesystem, so "path" exists for the editor page: when a command
	// carries a local image reference, the page hands the reference here and gets
	// an /assets/ URL back.
	var as *Asset
	switch ref := strings.TrimSpace(c.FormValue("path")); {
	case ref != "":
		assetURL, imported, err := s.assets.Add(ref)
		if err != nil {
			return errJSON(c, err)
		}
		as = imported
		if as.SHA == "" {
			as = &Asset{
				SHA: strings.TrimSuffix(strings.TrimPrefix(assetURL, "/assets/"), "."+strings.TrimPrefix(filepath.Ext(assetURL), ".")),
				Ext: strings.TrimPrefix(filepath.Ext(assetURL), "."),
			}
		}
	default:
		if url := strings.TrimSpace(c.FormValue("url")); url != "" {
			// A remote URL is fetched server-side; the browser never sees the
			// origin, so cross-origin images stay painless.
			assetURL, fetched, err := s.assets.Add(url)
			if err != nil {
				return errJSON(c, err)
			}
			as = fetched
			if as.SHA == "" {
				as = &Asset{
					SHA: strings.TrimSuffix(strings.TrimPrefix(assetURL, "/assets/"), "."+strings.TrimPrefix(filepath.Ext(assetURL), ".")),
					Ext: strings.TrimPrefix(filepath.Ext(assetURL), "."),
				}
			}
		} else {
			fh, err := c.FormFile("file")
			if err != nil {
				return badRequest(c, "multipart field \"file\" (or a \"url\"/\"path\" field) is required")
			}
			src, err := fh.Open()
			if err != nil {
				return errJSON(c, err)
			}
			defer src.Close()
			b, err := io.ReadAll(io.LimitReader(src, maxAssetBytes+1))
			if err != nil {
				return errJSON(c, err)
			}
			ext := safeExt(extOf(fh.Filename))
			if ext == "" {
				ext = "img"
			}
			if as, err = s.assets.SaveBytes(b, ext); err != nil {
				return errJSON(c, err)
			}
		}
	}
	return c.JSON(http.StatusCreated, map[string]any{
		"ok": true,
		"asset": map[string]any{
			"sha": as.SHA, "ext": as.Ext, "mime": as.MIME, "size": as.Size,
			"url": "/assets/" + as.SHA + "." + as.Ext,
		},
	})
}

func (s *Server) handleUploadFont(c echo.Context) error {
	fh, err := c.FormFile("file")
	if err != nil {
		return badRequest(c, "multipart field \"file\" is required")
	}
	ext := safeExt(extOf(fh.Filename))
	if ext != "ttf" && ext != "otf" {
		return badRequest(c, "only .ttf and .otf fonts can be uploaded")
	}
	src, err := fh.Open()
	if err != nil {
		return errJSON(c, err)
	}
	defer src.Close()
	b, err := io.ReadAll(io.LimitReader(src, maxAssetBytes+1))
	if err != nil {
		return errJSON(c, err)
	}
	sha := sha1Hex(b)
	path := fmt.Sprintf("%s/%s.%s", s.fonts.dir, sha, ext)
	if err := writeFile(path, b); err != nil {
		return errJSON(c, err)
	}
	families := fontFamiliesOf(path)
	if len(families) == 0 {
		return badRequest(c, "could not read a family name from that font file")
	}
	if len(families) > 1 {
		return badRequest(c, "that file is a font collection; upload a single face instead")
	}
	_ = s.store.SetMeta("font:"+sha, families[0])
	if err := s.fonts.Scan(); err != nil {
		return errJSON(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{
		"ok":     true,
		"family": families[0],
		"url":    "/fonts/" + sha + "." + ext,
	})
}

// --- small helpers ---

func errJSON(c echo.Context, err error) error {
	return c.JSON(http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
}

func badRequest(c echo.Context, msg string) error {
	return c.JSON(http.StatusBadRequest, map[string]any{"ok": false, "error": msg})
}

func firstNonZero(vals ...int) int {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

// handleFontFace serves the font file a renderer should use for a family,
// weight and style (the editor's CanvasKit and the export renderer alike).
// The headers say what the face really is, so the renderer can fake a bolder
// weight or a slant the family does not have.
func (s *Server) handleFontFace(c echo.Context) error {
	family := strings.TrimSpace(c.QueryParam("family"))
	weight, _ := strconv.Atoi(c.QueryParam("weight"))
	if weight <= 0 {
		weight = 400
	}
	italic := c.QueryParam("italic") == "1" || c.QueryParam("italic") == "true"
	b, m, err := s.fonts.Face(family, weight, italic)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
	}
	h := c.Response().Header()
	h.Set("X-Font-Weight", strconv.Itoa(m.Weight))
	h.Set("X-Font-Italic", strconv.FormatBool(m.Italic))
	h.Set("Access-Control-Expose-Headers", "X-Font-Weight, X-Font-Italic")
	h.Set("Cache-Control", "public, max-age=86400")
	return c.Blob(http.StatusOK, "font/ttf", b)
}
