package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/brand"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

// ------------------------------------------------------------------ logo and brand (FR-ORG-09, rev 0.200)

// BrandCleaned says what the SVG sanitiser took out of a file.
type BrandCleaned struct {
	Elements   []RemovedElement `json:"elements" doc:"The kinds of element removed, such as script or image."`
	Attributes int              `json:"attributes" doc:"How many attributes were removed: event handlers and anything outside the safe set." example:"1"`
	External   int              `json:"external" doc:"How many references to anything outside the file were removed." example:"1"`
}

// RemovedElement is a kind of SVG element the sanitiser removed.
type RemovedElement string

// Schema implements huma.SchemaProvider: a string, with an example.
func (RemovedElement) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "An SVG element's name.", Examples: []any{"script"}}
}

// BrandFileView is one of the brand's files.
type BrandFileView struct {
	MediaType string        `json:"media_type" enum:"image/svg+xml,image/png" doc:"An SVG cleaned to a safe subset, or a PNG written again without its metadata." example:"image/svg+xml"`
	Size      int           `json:"size_bytes" example:"6144"`
	SHA256    string        `json:"sha256" doc:"The SHA-256 of the file as kept, in hexadecimal." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Cleaned   *BrandCleaned `json:"cleaned,omitempty" doc:"What was taken out of the SVG when it was saved; absent when nothing was."`
}

// Brand is the organisation's logo and brand, as the latest version has them.
type Brand struct {
	Version   int            `json:"version" doc:"The version in force; 0 until the first is saved. For If-Match." example:"2"`
	Light     *BrandFileView `json:"light,omitempty" doc:"The logo for light backgrounds."`
	Dark      *BrandFileView `json:"dark,omitempty" doc:"The logo for dark backgrounds; until one is added, dark screens show the light one on a white tile."`
	Icon      *BrandFileView `json:"icon,omitempty" doc:"The square icon, for the browser tab and the app icon."`
	Print     *BrandFileView `json:"print,omitempty" doc:"The logo as documents show it: a PNG 192 pixels high, drawn from the logo for light backgrounds by the render service when it was uploaded (rev 0.201). Absent without a light logo, or when no render service is configured."`
	Accent    string         `json:"accent,omitempty" doc:"The accent colour of documents and emails; absent for the design's default. The app keeps its own colours." example:"#0F766E"`
	Accents   []AccentOption `json:"accent_options" doc:"The accent colours to choose from: each reads against white at 4.5:1 or more."`
	UpdatedAt *time.Time     `json:"updated_at,omitempty" example:"2026-11-07T09:00:00Z"`
}

// AccentOption is one of the accent colours to choose from.
type AccentOption struct {
	Colour string `json:"colour" pattern:"^#[0-9A-F]{6}$" example:"#0F766E"`
	Name   string `json:"name" example:"Deep teal"`
}

// BrandFileContent is a brand file's bytes.
type BrandFileContent struct {
	Kind      string `json:"kind" enum:"light,dark,icon,print" example:"light"`
	Version   int    `json:"version" example:"2"`
	MediaType string `json:"media_type" enum:"image/svg+xml,image/png" example:"image/svg+xml"`
	SHA256    string `json:"sha256" example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Content   string `json:"content_base64" doc:"The file, in base64: show it as an image, never inline as markup." example:"PHN2ZyB4bWxucz0i"`
}

// BrandUpload is a logo or icon file.
type BrandUpload struct {
	File huma.FormFile `form:"file" contentType:"application/octet-stream" required:"true" doc:"An SVG (up to 512 KB) or a PNG (up to 2 MB, at least 512 pixels wide; an icon square, at least 192). Its type is decided from its content."`
}

// BrandAccent sets the accent colour.
type BrandAccent struct {
	Accent string `json:"accent" maxLength:"7" doc:"One of accent_options; empty for the design's default." example:"#1E3A8A"`
}

// BrandKindPath names one of the brand's files.
type BrandKindPath struct {
	Kind string `path:"kind" enum:"light,dark,icon" doc:"light: the logo for light backgrounds; dark: for dark ones; icon: the square icon." example:"light"`
}

type brandOutput struct {
	ETag string `header:"ETag" doc:"The brand's version, for If-Match."`
	Body Brand
}

type brandWriteOutput struct {
	ETag     string `header:"ETag" doc:"The brand's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Brand
}

// BrandFilePath names one of the brand's files to read, the logo as documents show it included.
type BrandFilePath struct {
	Kind string `path:"kind" enum:"light,dark,icon,print" doc:"light: the logo for light backgrounds; dark: for dark ones; icon: the square icon; print: the logo as documents show it." example:"light"`
}

type brandFileInput struct {
	BrandFilePath
	Version int `query:"version" minimum:"0" doc:"The version to read; 0 or absent for the one in force." example:"2"`
}

type brandFileOutput struct {
	ETag string `header:"ETag" doc:"The brand version the file belongs to."`
	Body BrandFileContent
}

type brandUploadInput struct {
	BrandKindPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	RawBody huma.MultipartFormFiles[BrandUpload]
}

type brandRemoveInput struct {
	BrandKindPath
	IfMatchHeader
	PreferHeader
}

type brandAccentInput struct {
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body BrandAccent
}

// BrandCodes are the rules a brand file or colour keeps.
var BrandCodes = []string{brand.CodeFormat, brand.CodeSize, brand.CodeAccent, brand.CodeDraw}

func brandFileView(f *storage.BrandFile, cleaned map[string]brand.Removed, kind string) *BrandFileView {
	if f == nil {
		return nil
	}
	v := &BrandFileView{MediaType: f.MediaType, Size: f.Size, SHA256: hex.EncodeToString(f.SHA256)}
	if r, ok := cleaned[kind]; ok && r.Any() {
		v.Cleaned = &BrandCleaned{Elements: []RemovedElement{}, Attributes: r.Attributes, External: r.External}
		for _, n := range r.RemovedNames() {
			v.Cleaned.Elements = append(v.Cleaned.Elements, RemovedElement(n))
		}
	}
	return v
}

func brandOf(b storage.BrandVersion) Brand {
	out := Brand{Version: b.Version, Accent: b.Accent}
	for _, c := range brand.Accents {
		out.Accents = append(out.Accents, AccentOption{Colour: c, Name: brand.AccentNames[c]})
	}
	cleaned := map[string]brand.Removed{}
	_ = json.Unmarshal(b.Cleaned, &cleaned)
	out.Light, out.Dark, out.Icon = brandFileView(b.Light, cleaned, "light"), brandFileView(b.Dark, cleaned, "dark"), brandFileView(b.Icon, cleaned, "icon")
	out.Print = brandFileView(b.Print, nil, "print")
	if b.Version > 0 {
		t := b.CreatedAt.UTC()
		out.UpdatedAt = &t
	}
	return out
}

// currentBrand is the brand version in force (version 0 when none was saved), and all versions.
func currentBrand(ctx context.Context, u storage.UnitOfWork) (storage.BrandVersion, []storage.BrandVersion, error) {
	all, err := u.Brand().Versions(ctx)
	if err != nil || len(all) == 0 {
		return storage.BrandVersion{}, all, err
	}
	return all[0], all, nil
}

// nextBrand saves change applied to the version in force, at the version If-Match names.
func (a *API) nextBrand(ctx context.Context, u storage.UnitOfWork, ifMatch string, change func(b *storage.BrandVersion, cleaned map[string]brand.Removed)) (Brand, error) {
	cur, _, err := currentBrand(ctx, u)
	if err != nil {
		return Brand{}, err
	}
	if err := checkIfMatch(ifMatch, etag(cur.Version)); err != nil {
		return Brand{}, err
	}
	cleaned := map[string]brand.Removed{}
	_ = json.Unmarshal(cur.Cleaned, &cleaned)
	next := cur
	next.Version = cur.Version + 1
	change(&next, cleaned)
	if next.Cleaned, err = json.Marshal(cleaned); err != nil {
		return Brand{}, err
	}
	p, _ := authn.From(ctx)
	saved, err := u.Brand().Save(ctx, p.UserID, next, a.deps.Clock.Now())
	if err != nil {
		return Brand{}, err
	}
	return brandOf(saved), nil
}

// drawLogo asks the render service to draw logo, the cleaned logo for light backgrounds, as documents and emails show
// it (FR-ORG-09, rev 0.201): nil when no render service is configured, as then no document renders either.
func (a *API) drawLogo(ctx context.Context, logo []byte) ([]byte, error) {
	if a.deps.Renderer == nil {
		return nil, nil
	}
	res, err := a.deps.Renderer.Render(ctx, render.Request{Raster: logo})
	var rp *render.Problem
	switch {
	case err == nil && len(res.PNG) > 0 && len(res.PNG) <= brand.MaxPrint:
		return res.PNG, nil
	case errors.As(err, &rp) && rp.Code == render.CodeLogo:
		logFailure(ctx, err)
		return nil, invalid([]FieldProblem{{Code: brand.CodeDraw, Field: "body.file", Message: "cannot be drawn for documents and emails: it is too detailed, or uses what the drawing does not support",
			Fix: "Upload a simpler SVG, or the logo as a PNG"}})
	case errors.As(err, &rp) && rp.Code == render.CodeBusy:
		return nil, problem(doctemplate.CodeRenderer, "The render service is busy.", "Try again in a moment").withHeader("Retry-After", "1")
	default:
		if err != nil {
			logFailure(ctx, err)
		}
		return nil, problem(doctemplate.CodeRenderer, "The render service did not draw the logo for documents.", "Try again; if it persists, report it")
	}
}

func (a *API) brandPerson(ctx context.Context) error {
	p, _ := authn.From(ctx)
	if p.UserID == uuid.Nil {
		return problem(CodeSessionRequired, "Changing the brand needs a person with a Fyniq user.", "Sign in to Fyniq and do it there.")
	}
	return nil
}

func registerBrand(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType}, locks...)
	versioned := []string{CodeIfMatchRequired, CodeStale, storage.CodeVersion}

	register(a, authz.GetBrand, huma.Operation{
		OperationID: "getBrand", Method: http.MethodGet, Path: "/v1/organisation/brand", Tags: []string{"Organisation"},
		Summary: "Read the logo and brand",
		Description: "The organisation's logo for light and dark backgrounds, its icon and its accent colour, as the version in force has them " +
			"(FR-ORG-09), with the accent options. Every member reads it: the app's header shows the logo. Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*brandOutput, error) {
		p, _ := authn.From(ctx)
		var out brandOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			cur, _, err := currentBrand(ctx, u)
			out.Body, out.ETag = brandOf(cur), etag(cur.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.GetBrandFile, huma.Operation{
		OperationID: "getBrandFile", Method: http.MethodGet, Path: "/v1/organisation/brand/files/{kind}", Tags: []string{"Organisation"},
		Summary: "Read a logo or icon file",
		Description: "One of the brand's files, of the version in force or of an earlier one (a document keeps the logo it was issued with, " +
			"FR-ORG-09), in base64, the logo as documents show it included. An SVG is the cleaned copy: show it as an image, never inline as " +
			"markup. Read-only; no side effects.",
	}, append([]string{CodeNotFound, CodeUnavailable}, locks...), func(ctx context.Context, in *brandFileInput) (*brandFileOutput, error) {
		if a.deps.Staging == nil {
			return nil, problem(CodeUnavailable, "No file store is configured.", "Ask your administrator to configure it.")
		}
		p, _ := authn.From(ctx)
		var file *storage.BrandFile
		version := 0
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			_, all, err := currentBrand(ctx, u)
			for _, b := range all {
				if in.Version == 0 || b.Version == in.Version {
					file, version = map[string]*storage.BrandFile{"light": b.Light, "dark": b.Dark, "icon": b.Icon, "print": b.Print}[in.Kind], b.Version
					break
				}
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		if file == nil {
			return nil, problem(CodeNotFound, "The brand has no "+in.Kind+" file in that version.", "Read the brand to see which files it has")
		}
		r, err := a.deps.Staging.Get(ctx, brand.Key(p.Organisation, file.SHA256))
		if errors.Is(err, ports.ErrNotFound) {
			return nil, problem(CodeNotFound, "The file is missing from the store.", "Upload it again")
		}
		if err != nil {
			return nil, err
		}
		defer r.Close()
		data, err := io.ReadAll(io.LimitReader(r, brand.MaxPNG+1))
		if err != nil {
			return nil, err
		}
		if sum := sha256.Sum256(data); !bytes.Equal(sum[:], file.SHA256) {
			return nil, problem(CodeUnavailable, "The stored file does not match its record.", "Upload it again.")
		}
		return &brandFileOutput{ETag: etag(version), Body: BrandFileContent{Kind: in.Kind, Version: version, MediaType: file.MediaType, SHA256: hex.EncodeToString(file.SHA256),
			Content: base64.StdEncoding.EncodeToString(data)}}, nil
	})

	changeNote := " Needs the Owner, the Administrator or the Controller. Side effects: the next brand version and its event in the " +
		"organisation's trail; it takes effect at once. Needs If-Match with the brand's ETag (\"0\" before the first version). " +
		"Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing."
	register(a, authz.ChangeBrand, huma.Operation{
		OperationID: "uploadBrandFile", Method: http.MethodPut, Path: "/v1/organisation/brand/files/{kind}", Tags: []string{"Organisation"},
		Summary: "Upload a logo or icon",
		Description: "Replaces one of the brand's files, as multipart/form-data (FR-ORG-09). An SVG is reduced to a safe subset: no scripts, " +
			"event handlers, external references, embedded images or fonts, and no document type or entities; the answer says what was " +
			"removed. A PNG is written again without its metadata. The logo for light backgrounds is also drawn by the render service as " +
			"documents and emails show it: a PNG 192 pixels high (print)." + changeNote,
	}, append(append(append([]string{CodeInvalid, CodeUnavailable, CodeSessionRequired, doctemplate.CodeRenderer}, versioned...), writes...), BrandCodes...),
		func(ctx context.Context, in *brandUploadInput) (*brandWriteOutput, error) {
			if err := a.brandPerson(ctx); err != nil {
				return nil, err
			}
			if a.deps.Staging == nil {
				return nil, problem(CodeUnavailable, "No file store is configured.", "Ask your administrator to configure it.")
			}
			up := in.RawBody.Data()
			if up == nil || up.File.File == nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.file", Message: "is missing", Fix: "Send the file in the form field file"}})
			}
			defer up.File.Close()
			raw, err := io.ReadAll(io.LimitReader(up.File, brand.MaxPNG+1))
			if err != nil {
				return nil, problem(CodeMalformed, "The file could not be read in full.", "Send it again.")
			}
			clean, err := brand.Check(brand.Kind(in.Kind), raw)
			var be *brand.Error
			if errors.As(err, &be) {
				return nil, invalid([]FieldProblem{{Code: be.Code, Field: "body.file", Message: be.Message, Fix: "Upload an SVG or a PNG that keeps the rules"}})
			}
			if err != nil {
				return nil, err
			}
			if a.deps.Scanner != nil {
				if res, err := a.deps.Scanner.Scan(ctx, bytes.NewReader(raw)); err != nil || !res.Clean {
					return nil, invalid([]FieldProblem{{Code: brand.CodeFormat, Field: "body.file", Message: "did not pass the malware scan", Fix: "Upload another file"}})
				}
			}
			var drawn []byte
			if in.Kind == "light" {
				if drawn, err = a.drawLogo(ctx, clean.Data); err != nil {
					return nil, err
				}
			}
			sum, printSum := sha256.Sum256(clean.Data), sha256.Sum256(drawn)
			p, _ := authn.From(ctx)
			if !wantsPreview(in.Prefer) {
				for _, f := range []struct {
					sum  [32]byte
					data []byte
				}{{sum, clean.Data}, {printSum, drawn}} {
					if len(f.data) == 0 {
						continue
					}
					if err := a.deps.Staging.Create(ctx, brand.Key(p.Organisation, f.sum[:]), bytes.NewReader(f.data)); err != nil && !errors.Is(err, ports.ErrExists) {
						logFailure(ctx, err)
						return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
					}
				}
			}
			request := struct{ Kind, IfMatch, SHA256 string }{in.Kind, in.IfMatch, hex.EncodeToString(sum[:])}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Brand, error) {
				return a.nextBrand(ctx, u, in.IfMatch, func(b *storage.BrandVersion, cleaned map[string]brand.Removed) {
					f := &storage.BrandFile{MediaType: clean.MediaType, SHA256: sum[:], Size: len(clean.Data)}
					switch in.Kind {
					case "light":
						b.Light, b.Print = f, nil
						if len(drawn) > 0 {
							b.Print = &storage.BrandFile{MediaType: "image/png", SHA256: printSum[:], Size: len(drawn)}
						}
					case "dark":
						b.Dark = f
					default:
						b.Icon = f
					}
					delete(cleaned, in.Kind)
					if clean.Removed.Any() {
						cleaned[in.Kind] = clean.Removed
					}
				})
			})
			if err != nil {
				return nil, err
			}
			return brandWritten(w), nil
		})

	// huma describes the file as binary but takes no example for it from a tag.
	if op := a.OpenAPI().Paths["/v1/organisation/brand/files/{kind}"].Put; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"<svg xmlns=\"http://www.w3.org/2000/svg\" …> (the file's bytes)"}
		}
	}

	register(a, authz.RemoveBrandFile, exempt(huma.Operation{
		OperationID: "removeBrandFile", Method: http.MethodDelete, Path: "/v1/organisation/brand/files/{kind}", Tags: []string{"Organisation"},
		Summary:     "Remove a logo or icon",
		Description: "Removes one of the brand's files from the next version (FR-ORG-09), with the logo for light backgrounds its drawing " +
			"for documents; earlier versions keep them." + changeNote,
	}, "Idempotency-Key", "Removing a file from the brand twice changes nothing more (FR-INT-08, rev 0.200)."),
		append(append([]string{CodeNotFound, CodeSessionRequired}, versioned...), locks...), func(ctx context.Context, in *brandRemoveInput) (*brandWriteOutput, error) {
			if err := a.brandPerson(ctx); err != nil {
				return nil, err
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Brand, error) {
				cur, _, err := currentBrand(ctx, u)
				if err != nil {
					return Brand{}, err
				}
				if map[string]*storage.BrandFile{"light": cur.Light, "dark": cur.Dark, "icon": cur.Icon}[in.Kind] == nil {
					return Brand{}, problem(CodeNotFound, "The brand has no "+in.Kind+" file.", "Read the brand to see which files it has")
				}
				return a.nextBrand(ctx, u, in.IfMatch, func(b *storage.BrandVersion, cleaned map[string]brand.Removed) {
					switch in.Kind {
					case "light":
						b.Light, b.Print = nil, nil
					case "dark":
						b.Dark = nil
					default:
						b.Icon = nil
					}
					delete(cleaned, in.Kind)
				})
			})
			if err != nil {
				return nil, err
			}
			return brandWritten(w), nil
		})

	register(a, authz.SetBrandAccent, huma.Operation{
		OperationID: "setBrandAccent", Method: http.MethodPut, Path: "/v1/organisation/brand/accent", Tags: []string{"Organisation"},
		Summary: "Set the accent colour",
		Description: "Sets the accent colour of documents and emails to one of the contrast-checked options, or back to the design's " +
			"default (FR-ORG-09). The app keeps its own colours, and violet stays for AI content." + changeNote,
	}, append(append(append([]string{CodeInvalid, CodeSessionRequired}, versioned...), writes...), BrandCodes...),
		func(ctx context.Context, in *brandAccentInput) (*brandWriteOutput, error) {
			if err := a.brandPerson(ctx); err != nil {
				return nil, err
			}
			accent, err := brand.CheckAccent(in.Body.Accent)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: brand.CodeAccent, Field: "body.accent", Message: "is not one of the accent options",
					Fix: "Choose one of accent_options, or send an empty accent for the default"}})
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Brand, error) {
				return a.nextBrand(ctx, u, in.IfMatch, func(b *storage.BrandVersion, _ map[string]brand.Removed) { b.Accent = accent })
			})
			if err != nil {
				return nil, err
			}
			return brandWritten(w), nil
		})
}

func brandWritten(w written[Brand]) *brandWriteOutput {
	out := &brandWriteOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
	if !w.Previewed {
		out.ETag = etag(w.Body.Version)
	}
	return out
}
