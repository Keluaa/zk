package paths

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/zk-org/zk/internal/util"
)

// Walk emits the metadata of each file stored in the directory if they pass
// the given shouldIgnorePath closure. Hidden files and directories are ignored.
func Walk(basePath string, logger util.Logger, notebookRoot string, shouldIgnorePath func(string) (bool, error)) <-chan Metadata {
	c := make(chan Metadata, 50)
	go func() {
		defer close(c)

		err := filepath.Walk(basePath, func(abs string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			filename := info.Name()
			isHidden := strings.HasPrefix(filename, ".")
			isNotebookRoot := filename == notebookRoot

			if info.IsDir() {
				if isHidden && !isNotebookRoot {
					return filepath.SkipDir
				}

			} else {
				path, err := filepath.Rel(basePath, abs)
				if err != nil {
					logger.Println(err)
					return nil
				}
				shouldIgnore, err := shouldIgnorePath(path)
				if err != nil {
					logger.Println(err)
					return nil
				}
				if isHidden || shouldIgnore {
					return nil
				}

				c <- Metadata{
					Path:     path,
					Modified: info.ModTime().UTC(),
				}
			}

			return nil
		})

		if err != nil {
			logger.Println(err)
		}
	}()

	return c
}

// ParallelWalk behaves the same as [Walk], returning the metadata in the same order.
// Retrival of file metadata is split between multiple workers.
// shouldIgnorePath is not expected to be thread-safe.
func ParallelWalk(basePath string, logger util.Logger, notebookRoot string, shouldIgnorePath func(string) (bool, error)) <-chan Metadata {
	out := make(chan Metadata)

	// Avoid using all cores at once
	numWorkers := max(runtime.GOMAXPROCS(0)-2, 1)

	go func() {
		defer close(out)

		var mu sync.Mutex
		var metadata []Metadata

		err := ParallelWalkDir(numWorkers, basePath, func(abs string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			filename := info.Name()
			isHidden := strings.HasPrefix(filename, ".")
			isNotebookRoot := filename == notebookRoot

			if info.IsDir() {
				if isHidden && !isNotebookRoot {
					return filepath.SkipDir
				}
			} else {
				path, err := filepath.Rel(basePath, abs)
				if err != nil {
					logger.Println(err)
					return nil
				}

				// We assume that shouldIgnorePath is not thread-safe, so both it and 'append' are in
				// a thread-safe region
				mu.Lock()
				shouldIgnore, err := shouldIgnorePath(path)
				if !isHidden && !shouldIgnore {
					metadata = append(metadata, Metadata{
						Path:     path,
						Modified: info.ModTime().UTC(),
					})
				}
				mu.Unlock()

				if err != nil {
					logger.Println(err)
					return nil
				}
			}

			return nil
		})
		if err != nil {
			logger.Println(err)
			return
		}

		// The metadata array must be sorted by lexicographical order
		sort.Slice(metadata, func(i, j int) bool {
			return comparePaths(metadata[i].Path, metadata[j].Path)
		})

		for _, m := range metadata {
			out <- m
		}
	}()

	return out
}

// ParallelWalkDir behaves similarly to [filepath.WalkDir] but reads the filesystem
// using up to 'numWorkers' goroutines at once.
// Only the first I/O error encountered is returned.
func ParallelWalkDir(numWorkers int, root string, walkFn filepath.WalkFunc) error {
	var wg sync.WaitGroup
	sem := make(chan struct{}, numWorkers) // the semaphore limits the amount of active workers
	errChan := make(chan error, 1)
	info, err := os.Lstat(root)
	if err != nil {
		errChan <- err
	} else {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			parallelWalkDir(root, fs.FileInfoToDirEntry(info), sem, &wg, errChan, walkFn)
		})
	}
	wg.Wait()
	select {
	case err = <-errChan:
		return err
	default:
		return nil
	}
}

func parallelWalkDir(path string, d fs.DirEntry, sem chan struct{}, wg *sync.WaitGroup, errChan chan error, walkFn filepath.WalkFunc) {
	var err error = nil

	defer func() {
		if err != nil {
			// Propagate the first error to the main subroutine
			select {
			case errChan <- err:
			default:
			}
		}
	}()

	info, err := d.Info()
	if err != nil {
		return
	}

	if err = walkFn(path, info, nil); err != nil || !d.IsDir() {
		if err == filepath.SkipDir && d.IsDir() {
			err = nil
		}
		return
	}

	entries, err := os.ReadDir(path)
	for _, entry := range entries {
		fullPath := filepath.Join(path, entry.Name())
		// Instead of delegating the work to a goroutine on folders only, we always try to
		// delegate the work, therefore we can process any kind of workload more efficiently
		// (e.g. a complex folder structure, or a single folder with a large amount of files)
		select {
		case sem <- struct{}{}:
			wg.Go(func() {
				defer func() { <-sem }()
				parallelWalkDir(fullPath, entry, sem, wg, errChan, walkFn)
			})
		default:
			parallelWalkDir(fullPath, entry, sem, wg, errChan, walkFn)
		}
	}
}

// comparePaths lexicographically sorts paths, component by component
func comparePaths(a, b string) bool {
	sep := byte(filepath.Separator)
	for {
		aPos := strings.IndexByte(a, sep)
		bPos := strings.IndexByte(b, sep)

		if aPos == -1 && bPos == -1 {
			return a < b // 'a.txt' < 'b.txt'
		} else if aPos == -1 && bPos != -1 {
			return a < b[:bPos] // 'a.txt' < 'bDir'
		} else if aPos != -1 && bPos == -1 {
			return a[:aPos] < b // 'aDir' < 'b.txt'
		} else if a[:aPos] != b[:bPos] {
			return a[:aPos] < b[:bPos]
		}

		a = a[aPos+1:]
		b = b[bPos+1:]
	}
}
