package platform

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

type Resources struct {
	FS           fs.FS
	Root         string
	TemplatesDir string
	StaticDir    string
}

func resourceFiles(resources Resources) (fs.FS, error) {
	platformFiles, err := fs.Sub(builtins, "web")
	if err != nil {
		return nil, fmt.Errorf("open platform resources: %w", err)
	}
	if resources.FS == nil {
		return platformFiles, nil
	}
	application := resources.FS
	if resources.Root != "" && resources.Root != "." {
		application, err = fs.Sub(application, strings.Trim(resources.Root, "/"))
		if err != nil {
			return nil, fmt.Errorf("open application resource root: %w", err)
		}
	}
	templates := strings.Trim(resources.TemplatesDir, "/")
	if templates == "" {
		templates = "templates"
	}
	static := strings.Trim(resources.StaticDir, "/")
	if static == "" {
		static = "static"
	}
	return Overlay(&mappedResources{source: application, templates: templates, static: static}, platformFiles), nil
}

type mappedResources struct {
	source            fs.FS
	templates, static string
}

func (mapped *mappedResources) translate(name string) string {
	if name == "templates" {
		return mapped.templates
	}
	if strings.HasPrefix(name, "templates/") {
		return path.Join(mapped.templates, strings.TrimPrefix(name, "templates/"))
	}
	if name == "static" {
		return mapped.static
	}
	if strings.HasPrefix(name, "static/") {
		return path.Join(mapped.static, strings.TrimPrefix(name, "static/"))
	}
	return name
}
func (mapped *mappedResources) Open(name string) (fs.File, error) {
	return mapped.source.Open(mapped.translate(name))
}
func (mapped *mappedResources) ReadDir(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(mapped.source, mapped.translate(name))
}
