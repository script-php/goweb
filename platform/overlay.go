package platform

import (
	"io/fs"
	"sort"
)

type overlayFS struct{ layers []fs.FS }

// Overlay returns a filesystem where earlier layers override later layers.
func Overlay(layers ...fs.FS) fs.FS { return &overlayFS{layers: layers} }

func (overlay *overlayFS) Open(name string) (fs.File, error) {
	var last error
	for _, layer := range overlay.layers {
		file, err := layer.Open(name)
		if err == nil {
			return file, nil
		}
		last = err
	}
	return nil, last
}

func (overlay *overlayFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries := make(map[string]fs.DirEntry)
	found := false
	var last error
	for _, layer := range overlay.layers {
		items, err := fs.ReadDir(layer, name)
		if err != nil {
			last = err
			continue
		}
		found = true
		for _, item := range items {
			if _, exists := entries[item.Name()]; !exists {
				entries[item.Name()] = item
			}
		}
	}
	if !found {
		return nil, last
	}
	result := make([]fs.DirEntry, 0, len(entries))
	for _, item := range entries {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name() < result[j].Name() })
	return result, nil
}
