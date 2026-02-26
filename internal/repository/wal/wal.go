package wal

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"murl/internal/model/event"
	"os"
	"sync"
)

type WAL interface {
	Push(e event.Event) error
	Close() error
}

type wal struct {
	mu      sync.Mutex
	walFile *os.File
}

func NewWAL(path string) (WAL, error) {

	if path == "" {
		return nil, errors.New("invalid wal path: ''")
	}
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("wal open fail: %w", err)
	}

	w := &wal{
		walFile: fh,
	}

	return w, nil
}

func (w *wal) Push(e event.Event) error {
	data, err := e.Serialize()

	if err != nil {
		return fmt.Errorf("wal push fail: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.walFile.Write(data); err != nil {
		return fmt.Errorf("wal write fail: %w", err)
	}
	if _, err := w.walFile.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("wal write fail: %w", err)
	}

	if err := w.walFile.Sync(); err != nil {
		return fmt.Errorf("wal sync fail: %w", err)
	}

	return nil
}

func (w *wal) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.walFile.Close()
}

func LoadWAL(ctx context.Context, path string) (chan event.Event, error) {
	if path == "" {
		return nil, errors.New("invalid wal path: ''")
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("wal not found: %w", err)
	}

	fh, err := os.OpenFile(path, os.O_RDONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("wal open fail: %w", err)
	}

	ch := make(chan event.Event)

	go func() {
		defer fh.Close()
		defer close(ch)
		r := bufio.NewReader(fh)

		for {
			data, err := r.ReadBytes('\n')
			if len(data) > 0 {
				evt, err := event.Parse(data)
				if err != nil {
					slog.Error("cannot parse event",
						slog.Any("err", err),
					)
					continue
				}

				select {
				case <-ctx.Done():
					return
				case ch <- evt:
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					slog.Error("cannot read event",
						slog.Any("err", err),
					)
				}
				return
			}
		}
	}()

	return ch, nil
}
