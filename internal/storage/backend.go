package storage

import "io"

// Backend abstracts object storage. Implemented by local FS and S3.
type Backend interface {
	// Read returns a reader for the object at the given path.
	Read(path string) (io.ReadCloser, error)

	// Write stores data at the given path.
	Write(path string, data []byte) error

	// Has returns true if the path exists.
	Has(path string) (bool, error)

	// Delete removes the object at the given path.
	Delete(path string) error

	// List returns all paths under the given prefix.
	List(prefix string) ([]string, error)
}
