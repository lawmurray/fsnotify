package fsnotify

import (
  "runtime"
)

type recursive struct {
  b backend
	Events chan Event
	Errors chan error
}

func newRecursiveBackend(ev chan Event, errs chan error) (backend, error) {
  return newRecursiveBufferedBackend(0, ev, errs)
}

func newRecursiveBufferedBackend(sz uint, ev chan Event, errs chan error) (backend, error) {
  b, err := newBufferedBackend(sz, ev, errs)
  if err != nil {
    return nil, err
  }
  if runtime.GOOS != "windows" {
    // wrap in the recursive backend
		b = &recursive{
			b:      b,
			Events: ev,
			Errors: errs,
		}
	}
	return b, nil
}

func (w *recursive) Close() error {
  return w.b.Close()
}

func (w *recursive) Add(name string) error {
  return w.AddWith(name)
}

func (w *recursive) AddWith(path string, opts ...addOpt) error {
  return w.b.AddWith(path, opts...)
}

func (w *recursive) Remove(name string) error {
  return w.b.Remove(name)
}

func (w *recursive) WatchList() []string {
  return w.b.WatchList()
}

func (w *recursive) xSupports(op Op) bool {
	return w.b.xSupports(op);
}
