package pages

import (
	"bytes"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	hhtml "github.com/alecthomas/chroma/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithExtensions(
		highlighting.NewHighlighting(
			highlighting.WithStyle("monokai"),
			highlighting.WithFormatOptions(
				hhtml.WithLineNumbers(false),
			),
		),
	),
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(),
		parser.WithAttribute(),
	),
	goldmark.WithRendererOptions(
		html.WithXHTML(),
		html.WithUnsafe(), // we own the content; all good
	),
)

var DefaultMarkdown = func(source []byte, w io.Writer) error {
	return markdown.Convert(source, w)
}

func discard(format string, args ...any) {}

type Context struct {
	Data any

	// TODO(bmizerany): Root & Page for site maps
}

type Config struct {
	Funcs template.FuncMap // User-defined functions passed through to all traits and templates.
	Data  any              // User-defined data passed through as .Data to all traits and templates.

	Logf func(format string, args ...any)

	Markdown func(source []byte, w io.Writer) error
}

func (c Config) context() Context {
	return Context{Data: c.Data}
}

func Run(fsys fs.FS, cfg *Config) error {
	if cfg.Markdown == nil {
		cfg.Markdown = DefaultMarkdown
	}

	// TODO(bmizerany): make public dir configurable
	hasPublic, err := exists(fsys, "public")
	if err != nil {
		return err
	}

	if hasPublic {
		return errors.New("public directory already exists; please backup and/or remove and try again.")
	}

	// TODO(bmizerany): make pages dir configurable
	pagesFS, err := fs.Sub(fsys, "pages")
	if err != nil {
		return err
	}

	outDir, err := BuildFS(pagesFS, cfg)
	if err != nil {
		return err
	}

	return os.Rename(outDir, "public")
}

func BuildFS(fsys fs.FS, cfg *Config) (outDir string, err error) {
	dstDir, err := os.MkdirTemp("", "")
	if err != nil {
		return "", err
	}

	var c Config
	if cfg != nil {
		c = *cfg
	}

	if c.Logf == nil {
		c.Logf = discard
	}

	if err := c.buildDir(nil, dstDir, ".", fsys); err != nil {
		return "", err
	}

	return dstDir, nil
}

func (c Config) buildDir(traits *template.Template, dstDir, srcDir string, fsys fs.FS) error {
	c.Logf("building %s", srcDir)

	if traits == nil {
		traits = template.New("___traits___")
	}

	// any new traits we find apply only to us, and our children
	traits, err := traits.Clone()
	if err != nil {
		return err
	}

	tr, err := ReadTree(fsys)
	if err != nil {
		return err
	}

	c.logTree(srcDir, tr)

	if len(tr.Traits) > 0 {
		traitNames := namesOf(tr.Traits)
		c.Logf("traits found in %s: %s", srcDir, strings.Join(traitNames, ", "))

		_, err = traits.ParseFS(fsys, traitNames...)
		if err != nil {
			return err
		}

		c.Logf("parsed traits%s", traits.DefinedTemplates())
	}

	layout := traits.Lookup("_layout.tmpl")
	if layout == nil {
		// TODO(bmizerany): this could possibly be done at the start of
		// buildDir without affecting the semantics.
		layout, err = traits.New("_layout.tmpl").Parse(`
			{{- template "content" . -}}
		`)
		if err != nil {
			return err
		}
	} else {
		c.Logf("using layout in %s", srcDir)
	}

	for _, d := range tr.Templates {
		src, err := c.execTemplate(layout, fsys, d.Name())
		if err != nil {
			return err
		}

		dstPath := toIndexPath(dstDir, d.Name())

		c.Logf("writing %q to %q", d.Name(), dstPath)
		if err := c.copyData(dstPath, src); err != nil {
			return err
		}
	}

	for _, d := range tr.Assets {
		dstPath := filepath.Join(dstDir, d.Name())
		if err := c.copyFile(dstPath, fsys, d.Name()); err != nil {
			return err
		}
	}

	for _, d := range tr.Sections {
		sub, err := fs.Sub(fsys, d.Name())
		if err != nil {
			return err
		}

		if err := c.buildDir(
			traits,
			filepath.Join(dstDir, d.Name()),
			path.Join(srcDir, d.Name()),
			sub,
		); err != nil {
			return err
		}
	}

	return nil
}

func (c Config) execTemplate(layout *template.Template, fsys fs.FS, name string) (io.Reader, error) {
	c.Logf("executing template %q [context: %+v]", name, c.context())

	source, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}

	tmpl, err := layout.Clone()
	if err != nil {
		return nil, err
	}

	_, err = tmpl.New("content").Funcs(c.Funcs).Parse(string(source))
	if err != nil {
		return nil, err
	}

	if path.Ext(name) == ".md" {
		c.Logf("converting markdown in %q to html", name)

		source, err := slurpTmpl(tmpl, "content", c.context())
		if err != nil {
			return nil, err
		}

		var md bytes.Buffer
		if err := c.Markdown(source, &md); err != nil {
			return nil, err
		}

		_, err = tmpl.New("content").Parse(md.String())
		if err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "_layout.tmpl", c.context()); err != nil {
		return nil, err
	}

	return &buf, nil
}

func slurpTmpl(t *template.Template, name string, data any) ([]byte, error) {
	tmpl, err := t.Clone()
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (c Config) copyData(dstPath string, src io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), fs.ModePerm); err != nil {
		return err
	}

	// TODO(bmizerany): copy perms from src? seems daunting; we already say
	// only regular files are recognized so probably not worth it.
	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

func (c Config) copyFile(dstPath string, fsys fs.FS, name string) error {
	src, err := fsys.Open(name)
	if err != nil {
		return err
	}
	defer src.Close()
	return c.copyData(dstPath, src)
}

func (c Config) logTree(srcDir string, tr Tree) {
	logNames := func(prefix string, dd []fs.DirEntry) {
		c.Logf(prefix+" found in %s: %s", srcDir, strings.Join(namesOf(dd), ", "))
	}
	logNames("traits   ", tr.Traits)
	logNames("templates", tr.Templates)
	logNames("assets   ", tr.Assets)
	logNames("sections ", tr.Sections)
	logNames("unknown  ", tr.Unknown)
}

func namesOf(dd []fs.DirEntry) []string {
	var names []string
	for _, d := range dd {
		names = append(names, d.Name())
	}
	return names
}

type Tree struct {
	Traits    []fs.DirEntry
	Templates []fs.DirEntry
	Assets    []fs.DirEntry
	Sections  []fs.DirEntry
	Unknown   []fs.DirEntry
}

func ReadTree(fsys fs.FS) (Tree, error) {
	var tr Tree
	list, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return Tree{}, err
	}
	for _, d := range list {
		if d.Name() == "." {
			continue
		}

		switch {
		case d.Type().IsRegular() && matchAny(d.Name(), "_*.tmpl", "_*.tmpl.md"):
			tr.Traits = append(tr.Traits, d)
		case d.Type().IsRegular() && matchAny(d.Name(), "*.tmpl", "*.tmpl.md"):
			tr.Templates = append(tr.Templates, d)
		case d.Type().IsRegular():
			tr.Assets = append(tr.Assets, d)
		case d.IsDir():
			tr.Sections = append(tr.Sections, d)
		default:
			tr.Unknown = append(tr.Unknown, d)
		}
	}
	return tr, nil
}

func matchAny(name string, patterns ...string) bool {
	for _, pattern := range patterns {
		ok, err := path.Match(pattern, name)
		if err != nil {
			panic(err) // can only be ErrBadPattern
		}
		if ok {
			return true
		}
	}
	return false
}

func replaceTmplExt(name, with string) string {
	// TODO(bmizerany): This may be better written if/when there is a
	// longer list of supported types/extensions and they are defined in
	// some slice/map.
	bare := strings.TrimSuffix(name, ".tmpl")
	if bare == name {
		bare = strings.TrimSuffix(name, ".tmpl.md")
	}
	if bare == name {
		return bare
	}
	return bare + with
}

func replaceExt(name, with string) string {
	ext := path.Ext(name)
	return name[:len(name)-len(ext)] + with
}

func toIndexPath(dstDir, name string) string {
	dstPath := filepath.Join(dstDir, replaceTmplExt(name, ".html"))
	dir, file := filepath.Split(dstPath)
	if file != "index.html" {
		// expand from a.html to a/index.html
		dstPath = filepath.Join(dir, replaceExt(file, ""), "index.html")
	}
	return dstPath
}

func exists(fsys fs.FS, name string) (bool, error) {
	_, err := fs.Stat(fsys, "public")
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, err
}
