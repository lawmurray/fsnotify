package fsnotify

import (
	"fmt"
	"io/fs"
	"path/filepath"
  "runtime"
  "strings"
  "sync"
)

type recursive struct {
  b backend
  paths map[string]withOpts
  pathsMu sync.Mutex
	ev chan Event
	errs chan error
	ev_wrapped chan Event
	errs_wrapped chan error
	done chan struct{}
	doneMu sync.Mutex
	doneResp chan struct{}
}

func newRecursiveBackend(ev chan Event, errs chan error) (backend, error) {
  return newRecursiveBufferedBackend(0, ev, errs)
}

func newRecursiveBufferedBackend(sz uint, ev chan Event, errs chan error) (backend, error) {
  // Make base backend
  ev_wrapped := make(chan Event)
  errs_wrapped := make(chan error)
  b, err := newBufferedBackend(sz, ev_wrapped, errs_wrapped)
  if err != nil {
    return nil, err
  }

  // Wrap in recursive backend
	w := &recursive{
		b:      b,
		paths:  make(map[string]withOpts),
		ev: ev,
		errs: errs,
		ev_wrapped: ev_wrapped,
		errs_wrapped: errs_wrapped,
		done:        make(chan struct{}),
		doneResp:    make(chan struct{}),
	}

	// Start watch
	go w.pipeEvents()

	return w, nil
}

func (w *recursive) getOptions(path string) (withOpts, error) {
  w.pathsMu.Lock()
  defer w.pathsMu.Unlock()
	for prefix, with := range w.paths {
	  if strings.HasPrefix(path, prefix) {
		  return with, nil
	  }
	}
	return defaultOpts, fmt.Errorf("%w: %s", ErrNonExistentWatch, path)
}

func (w *recursive) pipeEvents() {
	defer func() {
	  close(w.doneResp)
		close(w.errs)
		close(w.ev)
	}()

	for {
		select {
		case <-w.done:
		  return
		case evt, ok := <-w.ev_wrapped:
		  if !ok {
		    return
		  }
		  w.sendEvent(evt)

		  if evt.Has(Create) {
				// Establish recursive watch and, if requested, send create events
				// for all children
		    with, err := w.getOptions(evt.Name)
		    if err == nil && with.recurse {
				  first := true
					filepath.WalkDir(evt.Name, func(path string, d fs.DirEntry, err error) error {
						if err != nil {
							return err
						}
						if d.IsDir() && runtime.GOOS != "windows" {
						  return w.b.Add(path)
							//return w.b.AddWith(path, with)
						}
						if !first && with.sendCreate {  // event for first already sent above
							w.sendEvent(Event{Name: path, Op: Create})
						}
						first = false
						return nil
					})
				}
			}
		case err, ok := <-w.errs_wrapped:
			if !ok {
				return
			}
			w.sendError(err)
		}
	}
}

// Returns true if the event was sent, or false if watcher is closed.
func (w *recursive) sendEvent(e Event) bool {
	select {
	case <-w.done:
		return false
	case w.ev <- e:
		return true
	}
}

// Returns true if the error was sent, or false if watcher is closed.
func (w *recursive) sendError(err error) bool {
	if err == nil {
		return true
	}
	select {
	case <-w.done:
		return false
	case w.errs <- err:
		return true
	}
}

func (w *recursive) isClosed() bool {
	select {
	case <-w.done:
		return true
	default:
		return false
	}
}

func (w *recursive) Close() error {
	w.doneMu.Lock()
	if w.isClosed() {
		w.doneMu.Unlock()
		return nil
	}
	close(w.done)
	w.doneMu.Unlock()
	<-w.doneResp
  return w.b.Close()
}

func (w *recursive) Add(path string) error {
  return w.AddWith(path)
}

func (w *recursive) AddWith(path string, opts ...addOpt) error {
  path, recurse := recursivePath(path);
	with := getOptions(opts...)
  with.recurse = recurse
  w.pathsMu.Lock()
  w.paths[path] = with
  w.pathsMu.Unlock()

  if recurse && (runtime.GOOS != "windows" || with.sendCreate) {
		return filepath.WalkDir(path, func(root string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Send create events for all directories and files recursively when
			// a new directory is created. This ensures that any files created
			// while the recursive watch is being established are reported as
			// created, in addition to any existing files and directories from an
			// existing directory hierarchy moved in. It includes the special case
			// of `mkdir -p one/two/three` on some systems, where only the creation
			// of `one` may be reported. More generally, it includes the case
			// `mkdir -p /tmp/one/two/three && mv /tmp/one one`, i.e. an existing
			// directory hierarchy moved in, which also only the create of `one`
			// may be reported.
			if with.sendCreate && d.IsDir() {
				w.ev <- Event{Name: root, Op: Create}
			}

			// Recursively watch directories if backend does not support natively
			if d.IsDir() && runtime.GOOS != "windows" {
				return w.b.AddWith(root, opts...)
			} else {
			  return nil
			}
		})
  } else {
	  return w.b.AddWith(path, opts...)
  }
}

func (w *recursive) Remove(path string) error {
  path, recurse := recursivePath(path);
	with, err := w.getOptions(path)
	if err != nil {
	  return err
	}
	if recurse && !with.recurse {
		return fmt.Errorf("can't use /... with non-recursive watch %q", path)
	}
	w.pathsMu.Lock()
  delete(w.paths, path)
  w.pathsMu.Unlock()

  if with.recurse && runtime.GOOS != "windows" {
		// Recursively remove directories
		return filepath.WalkDir(path, func(root string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			} else if d.IsDir() {
				return w.b.Remove(root)
			} else {
			  return nil
			}
		})
  } else {
	  return w.b.Remove(path)
  }
}

func (w *recursive) WatchList() []string {
  return w.b.WatchList()
}

func (w *recursive) xSupports(op Op) bool {
	return w.b.xSupports(op);
}
