package common

import (
	"errors"
	"io"
	"log"
	"os"
)

const (
	KiB = 1024
	MiB = 1024 * KiB
	GiB = 1024 * MiB
)

func CloseWrapper(r ...io.Closer) {
	for _, v := range r {
		if v == nil {
			continue
		}
		if err := v.Close(); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
			log.Printf("failed to close output resource %#v: %s", v, err)
		}
	}
}

func Copy(srcFile, dstFile string) error {
	out, err := os.Create(dstFile)
	defer CloseWrapper(out)
	if err != nil {
		return err
	}

	in, err := os.Open(srcFile)
	defer CloseWrapper(in)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return nil
}
