package pages

import (
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"kr.dev/diff"
)

var buildTests = []struct {
	name  string
	fs    stringFS
	data  any
	funcs template.FuncMap
	want  stringFS
}{
	{
		name: "none",
		fs:   stringFS{},
		want: stringFS{},
	},
	{
		name: "content with asset",
		fs: stringFS{
			"asset.css":    `test {}`,
			"content.tmpl": `test`,
		},
		want: stringFS{
			"asset.css":          "test {}",
			"content/index.html": "test",
		},
	},
	{
		name: "content",
		fs: stringFS{
			"content.tmpl": `test`,
		},
		want: stringFS{
			"content/index.html": "test",
		},
	},
	{
		name: "with data",
		data: "World.",
		fs: stringFS{
			"index.tmpl": `Hello, {{.Data}}`,
		},
		want: stringFS{
			"index.html": "Hello, World.",
		},
	},
	{
		name: "content layout",
		fs: stringFS{
			"_layout.tmpl": `this {{template "content"}}`,
			"index.tmpl":   `is a test`,
		},
		want: stringFS{
			"index.html": "this is a test",
		},
	},
	{
		name: "content index",
		fs: stringFS{
			"content.tmpl": `test`,
			"index.tmpl":   `index`,
		},
		want: stringFS{
			"content/index.html": "test",
			"index.html":         "index",
		},
	},
	{
		name: "content.tmpl.md index.tmpl.md",
		fs: stringFS{
			"content.tmpl.md": `# test`,
			"index.tmpl.md":   `# index`,
		},
		want: stringFS{
			"content/index.html": "<h1 id=\"test\">test</h1>\n",
			"index.html":         "<h1 id=\"index\">index</h1>\n",
		},
	},
	{
		name: "nested layout",
		fs: stringFS{
			"_layout.tmpl":         `it`,
			"content/the.tmpl.md":  "# content\n<pre></pre>",
			"content/_layout.tmpl": `this is {{template "content"}}`,
			"z.tmpl":               `not what it should be`,
		},
		want: stringFS{
			"content/the/index.html": "this is <h1 id=\"content\">content</h1>\n<pre></pre>",
			"z/index.html":           "it",
		},
	},
	{
		name: "traits",
		fs: stringFS{
			"_arm.tmpl":  `flap`,
			"_head.tmpl": `nod`,
			"index.tmpl": `{{ template "_arm.tmpl" }} {{ template "_head.tmpl" }}`,
		},
		want: stringFS{
			// traits should not be copied to public
			"index.html": "flap nod",
		},
	},
	{
		name: "inherited traits",
		fs: stringFS{
			"_arm.tmpl":    `flap`,
			"_head.tmpl":   `nod`,
			"a/index.tmpl": `{{ template "_arm.tmpl" }} {{ template "_head.tmpl" }}`,
		},
		want: stringFS{
			// traits should not be copied to public
			"a/index.html": "flap nod",
		},
	},
	{
		name: "inherited traits override",
		fs: stringFS{
			"_arm.tmpl":    `flap`,
			"_head.tmpl":   `nod`,
			"index.tmpl":   `{{ template "_arm.tmpl" }} {{ template "_head.tmpl" }}`,
			"a/_head.tmpl": `shake`,
			"a/index.tmpl": `{{ template "_arm.tmpl" }} {{ template "_head.tmpl" }}`,
		},
		want: stringFS{
			// traits should not be copied to public
			"index.html":   "flap nod",
			"a/index.html": "flap shake",
		},
	},
	// TODO: {
	// TODO: 	name: "traits differing template types",
	// TODO: 	fs: stringFS{
	// TODO: 		"_arm.tmpl":       `flap`,
	// TODO: 		"_head.tmpl":      `nod`,
	// TODO: 		"a/_head.tmpl.md": `shake`,
	// TODO: 		"a/index.tmpl":    `{{ template "_arm" }} {{ template "_head" }}`,
	// TODO: 	},
	// TODO: 	want: stringFS{
	// TODO: 		// traits should not be copied to public
	// TODO: 		"a/index.html": "flap <p>shake</p>\n",
	// TODO: 	},
	// TODO: },
	{
		name: "trait with same name as content",
		fs: stringFS{
			"_a.tmpl": `flap`,
			"a.tmpl":  `wing {{ template "_a.tmpl" }}`,
		},
		want: stringFS{
			// traits should not be copied to public
			"a/index.html": "wing flap",
		},
	},

	// TODO(bmizerany):  test with pluginData
	{
		name: "func",
		funcs: map[string]any{
			"hello": func(s string) string { return "hello, " + s },
		},
		fs: stringFS{
			"a.tmpl": `{{ hello "world" }}`,
		},
		want: stringFS{
			"a/index.html": "hello, world",
		},
	},
	{
		name: "func in layout",
		funcs: map[string]any{
			"hello": func(s string) string { return "hello, " + s },
		},
		fs: stringFS{
			"_layout.tmpl": `{{ hello "world" }} {{template "content"}}`,
			"index.tmpl":   `some content`,
		},
		want: stringFS{
			"index.html": "hello, world some content",
		},
	},
	{
		name: "traits in markdown",
		fs: stringFS{
			"_t.tmpl.md": `
# World`,
			"index.tmpl.md": `Hello, {{ template "_t.tmpl.md" }}`,
		},
		want: stringFS{
			"index.html": "<p>Hello,</p>\n<h1 id=\"world\">World</h1>\n",
		},
	},
}

func TestBuildFS(t *testing.T) {
	for _, tt := range buildTests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Funcs: tt.funcs,
				Data:  tt.data,
				Logf: func(format string, args ...any) {
					t.Helper()
					t.Logf(tt.name+": "+format, args...)
				},
				Markdown: DefaultMarkdown,
			}
			outDir, err := BuildFS(tt.fs.FS(), cfg)
			if err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			got := os.DirFS(outDir)
			diffFS(t, got, tt.want.FS())
		})
	}
}

type stringFS map[string]string

func (sfs stringFS) FS() fs.FS {
	mfs := fstest.MapFS{}
	for name, content := range sfs {
		mfs[name] = &fstest.MapFile{
			Mode:    fs.ModePerm, // TODO(bmizerany): this isn't do anything atm; investigate
			ModTime: time.Unix(0, 0),
			Data:    []byte(content),
		}
	}
	return mfs
}

func diffFS(t testing.TB, gotFS, wantFS fs.FS) {
	t.Helper()
	got := stringFS{}
	if err := fs.WalkDir(gotFS, ".", addToMapFS(gotFS, got)); err != nil {
		t.Fatal(err)
	}

	want := stringFS{}
	if err := fs.WalkDir(wantFS, ".", addToMapFS(wantFS, want)); err != nil {
		t.Fatal(err)
	}

	diff.Test(t, t.Errorf, got, want)
}

func addToMapFS(fsys fs.FS, sfs stringFS) fs.WalkDirFunc {
	return fs.WalkDirFunc(func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		var data []byte
		if d.Type().IsRegular() {
			var err error
			data, err = fs.ReadFile(fsys, path)
			if err != nil {
				return fmt.Errorf("ReadFile: %s: %s: %w", path, d.Type(), err)
			}
		}

		sfs[path] = string(data)

		return nil
	})
}
