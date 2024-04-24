package common

import (
	"context"
	"os"
	"time"
)

func IsExists(p string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := osStatWithContext(ctx, p)
	return err == nil
}

// GetSize return actual file size.
func GetSize(p string) (uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := osStatWithContext(ctx, p)
	if err != nil {
		return 0, err
	}
	return uint64(info.Size()), nil
}

func osStatWithContext(ctx context.Context, name string) (os.FileInfo, error) {
	dataCh := make(chan os.FileInfo)
	errCh := make(chan error)

	go func() { //nolint:gocritic // can't use goroutine.Run because of import cycle
		fileInfo, err := os.Stat(name)
		if err != nil {
			errCh <- err
			return
		}
		dataCh <- fileInfo
	}()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case err := <-errCh:
			return nil, err

		case data := <-dataCh:
			return data, nil
		}
	}
}
