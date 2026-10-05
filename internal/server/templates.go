package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
)

// The template library (模板库) is not a separate store: a template is a
// project like any other — layered, editable, exportable — whose kind is
// "template" instead of "design". What sets it apart is what it is for: it is
// kept as a reference, carries tags, a note (why it was kept) and a link
// (where it came from), and new designs start as copies of it.
//
// The routes here are the template-specific ones; listing, editing, moving
// between libraries (PATCH kind) and deleting go through /api/projects.

func (s *Server) registerTemplates(api *echo.Group) {
	api.GET("/projects/tags", s.handleProjectTags)
	api.POST("/projects/from-image", s.handleProjectFromImage)
	api.POST("/projects/:id/duplicate", s.handleDuplicateProject)
}

// handleProjectTags counts the tags in use: ?kind=template (the default) or design.
func (s *Server) handleProjectTags(c echo.Context) error {
	kind := c.QueryParam("kind")
	if kind == "" {
		kind = KindTemplate
	}
	if kind != KindDesign && kind != KindTemplate {
		return badRequest(c, `kind must be "design" or "template"`)
	}
	tags, err := s.store.ProjectTags(kind)
	if err != nil {
		return errJSON(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "tags": tags})
}

// handleDuplicateProject copies a project: {"kind": "design"} (the default)
// starts a design from a template, {"kind": "template"} keeps a copy in the
// template library. The name defaults to the original's.
func (s *Server) handleDuplicateProject(c echo.Context) error {
	src, err := s.store.GetProject(c.Param("id"))
	if err != nil {
		return errJSON(c, err)
	}
	if src == nil {
		return notFound(c, "project not found")
	}
	var req struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	}
	_ = c.Bind(&req)
	kind := firstNonEmptyStr(req.Kind, KindDesign)
	if kind != KindDesign && kind != KindTemplate {
		return badRequest(c, `kind must be "design" or "template"`)
	}
	name := strings.TrimSpace(firstNonEmptyStr(req.Name, src.Name))
	p, err := s.store.DuplicateProject(src.ID, name, kind)
	if err != nil {
		return errJSON(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"ok": true, "project": summaryOf(p), "editorURL": s.Addr() + "/editor/" + p.ID})
}

// handleProjectFromImage makes a project out of one picture — a cover saved
// from somewhere, usually, filed straight into the template library. The
// canvas is the picture's own size (within 3000px on the long side) and the
// picture fills it as an ordinary image layer, so it can be cut up later.
//
// It takes a multipart upload ("file" plus "name kind tags note link") or a
// JSON body naming the picture by "ref": a local path, an http(s) URL or an
// /assets/ URL already on the server.
func (s *Server) handleProjectFromImage(c echo.Context) error {
	var (
		ref, name, kind, note, link string
		tags                        []string
		url                         string
	)
	if strings.HasPrefix(c.Request().Header.Get("Content-Type"), "multipart/") {
		name, kind, note, link = c.FormValue("name"), c.FormValue("kind"), c.FormValue("note"), c.FormValue("link")
		tags = splitTags(c.FormValue("tags"))
		fh, err := c.FormFile("file")
		if err != nil {
			if ref = c.FormValue("ref"); ref == "" {
				return badRequest(c, `multipart field "file" (or a "ref") is required`)
			}
		} else {
			src, err := fh.Open()
			if err != nil {
				return errJSON(c, err)
			}
			b, err := io.ReadAll(io.LimitReader(src, maxAssetBytes+1))
			src.Close()
			if err != nil {
				return errJSON(c, err)
			}
			ext := safeExt(extOf(fh.Filename))
			if ext == "" {
				ext = "img"
			}
			as, err := s.assets.SaveBytes(b, ext)
			if err != nil {
				return badRequest(c, err.Error())
			}
			url = "/assets/" + as.SHA + "." + as.Ext
			if name == "" {
				name = strings.TrimSuffix(fh.Filename, filepath.Ext(fh.Filename))
			}
		}
	} else {
		var req struct {
			Ref  string   `json:"ref"`
			Name string   `json:"name"`
			Kind string   `json:"kind"`
			Tags []string `json:"tags"`
			Note string   `json:"note"`
			Link string   `json:"link"`
		}
		if err := c.Bind(&req); err != nil {
			return badRequest(c, "invalid JSON body: "+err.Error())
		}
		ref, name, kind, tags, note, link = req.Ref, req.Name, req.Kind, req.Tags, req.Note, req.Link
		if strings.TrimSpace(ref) == "" {
			return badRequest(c, `"ref" (a local path, an http(s) URL or an /assets/ URL) is required`)
		}
	}
	kind = firstNonEmptyStr(kind, KindTemplate)
	if kind != KindDesign && kind != KindTemplate {
		return badRequest(c, `kind must be "design" or "template"`)
	}
	if url == "" {
		u, _, err := s.assets.Add(ref)
		if err != nil {
			return badRequest(c, err.Error())
		}
		url = u
		if name == "" {
			base := filepath.Base(strings.SplitN(ref, "?", 2)[0])
			name = strings.TrimSuffix(base, filepath.Ext(base))
		}
	}
	path, err := s.assets.Path(url)
	if err != nil {
		return errJSON(c, err)
	}
	iw, ih := imageSize(path)
	if iw <= 0 || ih <= 0 {
		return badRequest(c, "could not read the picture's size — is it an image?")
	}
	w, h := fitLongSide(iw, ih, 3000)
	blank, err := blankDocument(w, h)
	if err != nil {
		return errJSON(c, err)
	}
	p, err := s.store.NewProject(strings.TrimSpace(firstNonEmptyStr(name, "未命名模板")), w, h, blank)
	if err != nil {
		return errJSON(c, err)
	}
	fail := func(err error) error {
		_ = s.store.DeleteProject(p.ID)
		s.forget(p.ID)
		return err
	}
	if err := s.store.SetProjectMeta(p.ID, ProjectMeta{Kind: &kind, Tags: &tags, Note: &note, Link: &link}); err != nil {
		return fail(badRequest(c, err.Error()))
	}
	cmd, _ := json.Marshal(map[string]any{"type": "addImage", "url": url, "name": "原图", "x": 0, "y": 0, "width": w, "height": h})
	res, err := s.Exec(p.ID, cmd)
	if err == nil && !res.OK {
		err = errors.New(res.Error)
	}
	if err != nil {
		return fail(errJSON(c, fmt.Errorf("place the picture: %w", err)))
	}
	full, err := s.store.GetProject(p.ID)
	if err != nil {
		return errJSON(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"ok": true, "project": summaryOf(full), "editorURL": s.Addr() + "/editor/" + p.ID})
}

// fitLongSide scales w×h down so neither side passes max.
func fitLongSide(w, h, max int) (int, int) {
	if w <= max && h <= max {
		return w, h
	}
	f := float64(max) / float64(w)
	if h > w {
		f = float64(max) / float64(h)
	}
	return int(math.Max(1, math.Round(float64(w)*f))), int(math.Max(1, math.Round(float64(h)*f)))
}

// summaryOf is the listing row for a project.
func summaryOf(p *Project) ProjectSummary {
	return ProjectSummary{ID: p.ID, Name: p.Name, Kind: p.Kind, Tags: p.Tags, Note: p.Note, Link: p.Link,
		Width: p.Width, Height: p.Height, Revision: p.Revision, UpdatedAt: p.UpdatedAt}
}
