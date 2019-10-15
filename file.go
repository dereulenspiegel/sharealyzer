package main

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const folderTimeFormat = "2006-01-02"

type GZippedFileWriter struct {
	BaseDir string
}

type ScrapeFile interface {
	ScrapeDate() time.Time
	Content() []byte
	Provider() string
}

type FileWriteError struct {
	FilePath string
	Err      error
}

func (f FileWriteError) Error() string {
	return "Writing " + f.FilePath + " failed: " + f.Err.Error()
}

func (g *GZippedFileWriter) Write(ctx context.Context, in chan ScrapeFile) chan error {
	errChan := make(chan error, 10)
	go func() {
		for {
			select {
			case <-ctx.Done():
				close(errChan)
				return
			case scrapeFile := <-in:
				if err := g.writeTo(scrapeFile); err != nil {
					errChan <- err
				}
			}
		}
	}()
	return errChan
}

func (g *GZippedFileWriter) writeTo(f ScrapeFile) error {
	folderName := fmt.Sprintf("%s_%s", f.Provider(), f.ScrapeDate().Format(folderTimeFormat))
	fileName := fmt.Sprintf("%s_%s.json.gz", f.Provider(), f.ScrapeDate().Format(time.RFC3339))
	outFolder := filepath.Join(g.BaseDir, folderName)

	if !fileDoesExist(outFolder) {
		if err := os.MkdirAll(outFolder, 0770); err != nil {
			return err
		}
	}

	outFile, err := os.Create(filepath.Join(outFolder, fileName))
	if err != nil {
		return err
	}
	defer outFile.Close()

	gzipWriter, err := gzip.NewWriterLevel(outFile, gzip.BestCompression)
	if err != nil {
		return err
	}
	defer gzipWriter.Close()

	data := f.Content()
	n, err := gzipWriter.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return errors.New("Written less data than expected")
	}
	return nil
}

func fileDoesExist(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	} else if os.IsExist(err) {
		return true
	} else if err == nil {
		return true
	} else {
		return false
	}
}
