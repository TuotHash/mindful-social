package server

import (
	"net/http"
	"os"
)

type noDirectoryListingFS struct {
	root http.FileSystem
}

func noDirectoryListing(root http.FileSystem) http.FileSystem {
	return noDirectoryListingFS{root: root}
}

func (fs noDirectoryListingFS) Open(name string) (http.File, error) {
	f, err := fs.root.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, os.ErrNotExist
	}
	return f, nil
}
