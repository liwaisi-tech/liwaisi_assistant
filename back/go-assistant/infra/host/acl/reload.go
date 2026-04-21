package acl

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
	"time"
)

const defaultPollInterval = 5 * time.Second

type Watcher struct {
	path         string
	current      atomic.Pointer[ACL]
	logger       *slog.Logger
	lastMtime    atomic.Int64
	pollInterval time.Duration
}

func NewWatcher(path string, logger *slog.Logger) (*Watcher, error) {
	if logger == nil {
		logger = slog.Default()
	}
	w := &Watcher{path: path, logger: logger, pollInterval: defaultPollInterval}
	a, err := LoadACL(path)
	if err != nil {
		return nil, err
	}
	w.current.Store(a)
	if fi, err := os.Stat(path); err == nil {
		w.lastMtime.Store(fi.ModTime().UnixNano())
	}
	return w, nil
}

func (w *Watcher) Snapshot() *ACL {
	return w.current.Load()
}

func (w *Watcher) Reload() error {
	a, err := LoadACL(w.path)
	if err != nil {
		w.logger.Error("acl: reload failed, keeping previous snapshot", "path", w.path, "err", err)
		return err
	}
	w.current.Store(a)
	if fi, ferr := os.Stat(w.path); ferr == nil {
		w.lastMtime.Store(fi.ModTime().UnixNano())
	}
	w.logger.Info("acl: reloaded", "path", w.path, "rules", len(a.Rules))
	return nil
}

func (w *Watcher) SetPollInterval(d time.Duration) {
	if d > 0 {
		w.pollInterval = d
	}
}

func (w *Watcher) Start(ctx context.Context) {
	go w.pollLoop(ctx)
}

func (w *Watcher) pollLoop(ctx context.Context) {
	t := time.NewTicker(w.pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.checkMtime()
		}
	}
}

func (w *Watcher) checkMtime() {
	fi, err := os.Stat(w.path)
	if err != nil {
		w.logger.Warn("acl: stat failed", "path", w.path, "err", err)
		return
	}
	mt := fi.ModTime().UnixNano()
	if mt == w.lastMtime.Load() {
		return
	}
	_ = w.Reload()
}
