package snapshot

import (
	"fmt"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/PlakarKorp/plakar/events"
	"github.com/PlakarKorp/plakar/snapshot/exporter"
	"github.com/PlakarKorp/plakar/snapshot/vfs"
)

type RestoreOptions struct {
	MaxConcurrency uint64
	Strip          string
}

type restoreContext struct {
	hardlinks      map[string]string
	hardlinksMutex sync.Mutex
	maxConcurrency chan bool
}

func snapshotRestorePath(snap *Snapshot, exp exporter.Exporter, target string, opts *RestoreOptions, restoreContext *restoreContext, wg *sync.WaitGroup) func(entrypath string, e *vfs.Entry, err error) error {
	return func(entrypath string, e *vfs.Entry, err error) error {
		restoreContext.maxConcurrency <- true
		wg.Add(1)
		go func(entrypath string, e *vfs.Entry, err error) {
			defer func() { <-restoreContext.maxConcurrency }()
			defer wg.Done()
			if err != nil {
				snap.Event(events.PathErrorEvent(snap.Header.Identifier, entrypath, err.Error()))
				return
			}

			snap.Event(events.PathEvent(snap.Header.Identifier, entrypath))

			// Determine destination path by stripping the prefix.
			dest := path.Join(target, strings.TrimPrefix(entrypath, opts.Strip))

			// Directory processing.
			if e.IsDir() {
				snap.Event(events.DirectoryEvent(snap.Header.Identifier, entrypath))
				// Create directory if not root.
				if entrypath != "/" {
					if err := exp.CreateDirectory(dest); err != nil {
						snap.Event(events.DirectoryErrorEvent(snap.Header.Identifier, entrypath, err.Error()))
						return
					}
				}

				// WalkDir handles recursion so we don’t need to iterate children manually.
				if entrypath != "/" {
					if err := exp.SetPermissions(dest, e.Stat()); err != nil {
						snap.Event(events.DirectoryErrorEvent(snap.Header.Identifier, entrypath, err.Error()))
						return
					}
				}
				snap.Event(events.DirectoryOKEvent(snap.Header.Identifier, entrypath))
				return
			}

			// For non-directory entries, only process regular files.
			if !e.Stat().Mode().IsRegular() {
				snap.Event(events.FileErrorEvent(snap.Header.Identifier, entrypath, "unexpected vfs entry type"))
				return
			}

			snap.Event(events.FileEvent(snap.Header.Identifier, entrypath))
			restoreContext.maxConcurrency <- true
			wg.Add(1)
			go func(e *vfs.Entry, entrypath string) {
				defer wg.Done()
				defer func() { <-restoreContext.maxConcurrency }()

				// Handle hard links.
				if e.Stat().Nlink() > 1 {
					key := fmt.Sprintf("%d:%d", e.Stat().Dev(), e.Stat().Ino())
					restoreContext.hardlinksMutex.Lock()
					v, ok := restoreContext.hardlinks[key]
					restoreContext.hardlinksMutex.Unlock()
					if ok {
						// Create a new link and return.
						if err := os.Link(v, dest); err != nil {
							snap.Event(events.FileErrorEvent(snap.Header.Identifier, entrypath, err.Error()))
						}
						return
					} else {
						restoreContext.hardlinksMutex.Lock()
						restoreContext.hardlinks[key] = dest
						restoreContext.hardlinksMutex.Unlock()
					}
				}

				rd, err := snap.NewReader(entrypath)
				if err != nil {
					snap.Event(events.FileErrorEvent(snap.Header.Identifier, entrypath, err.Error()))
					return
				}
				defer rd.Close()

				// Ensure the parent directory exists.
				if err := exp.CreateDirectory(path.Dir(dest)); err != nil {
					snap.Event(events.FileErrorEvent(snap.Header.Identifier, entrypath, err.Error()))
				}

				// Restore the file content.
				if err := exp.StoreFile(dest, rd); err != nil {
					snap.Event(events.FileErrorEvent(snap.Header.Identifier, entrypath, err.Error()))
				} else if err := exp.SetPermissions(dest, e.Stat()); err != nil {
					snap.Event(events.FileErrorEvent(snap.Header.Identifier, entrypath, err.Error()))
				} else {
					snap.Event(events.FileOKEvent(snap.Header.Identifier, entrypath, e.Size()))
				}
			}(e, entrypath)
		}(entrypath, e, err)
		return nil
	}
}

func (snap *Snapshot) Restore(exp exporter.Exporter, base string, pathname string, opts *RestoreOptions) error {
	snap.Event(events.StartEvent())
	defer snap.Event(events.DoneEvent())

	fs, err := snap.Filesystem()
	if err != nil {
		return err
	}

	maxConcurrency := opts.MaxConcurrency
	if maxConcurrency == 0 {
		maxConcurrency = uint64(snap.AppContext().MaxConcurrency)
	}

	restoreContext := &restoreContext{
		hardlinks:      make(map[string]string),
		hardlinksMutex: sync.Mutex{},
		maxConcurrency: make(chan bool, maxConcurrency),
	}
	defer close(restoreContext.maxConcurrency)

	base = path.Clean(base)
	if base != "/" && !strings.HasSuffix(base, "/") {
		base = base + "/"
	}

	wg := sync.WaitGroup{}
	defer wg.Wait()

	return fs.WalkDir(pathname, snapshotRestorePath(snap, exp, base, opts, restoreContext, &wg))
}
