package main

import (
  "bytes"
  "io"
  "os"
  "sync"
  "syscall"
  "time"
)

// Implements os.FileInfo, Reader and Writer interfaces.
// These are the 3 interfaces necessary for the Handlers.
type s3File struct {
  name        string
  modtime     time.Time
  symlink     string
  isdir       bool
  content     []byte
  contentLock sync.RWMutex
  bucket      string
  key         string
}

// Have s3File fulfill os.FileInfo interface
func (f *s3File) IsDir() bool {
  return f.isdir
}

func (f *s3File) Mode() os.FileMode {
  ret := os.FileMode(0644)

  if f.isdir {
    ret = os.FileMode(0755) | os.ModeDir
  }
  if f.symlink != "" {
    ret = os.FileMode(0777) | os.ModeSymlink
  }

  return ret
}

func (f *s3File) ModTime() time.Time {
  return time.Now()
}

func (f *s3File) Name() string {
  return f.name
}

func (f *s3File) Size() int64  {
  return 100
}

func (f *s3File) Sys() interface{} {
  return fakeFileInfoSys()
}

func fakeFileInfoSys() interface{} {
  return &syscall.Stat_t{Uid: 65534, Gid: 65534}
}

func (f *s3File) WriterAt() (io.WriterAt, error) {
  if f.isdir {
    return nil, os.ErrInvalid
  }
  return nil, nil
}

func (f *s3File) ReaderAt() (io.ReaderAt, error) {
  if f.isdir {
    return nil, os.ErrInvalid
  }

  return bytes.NewReader(f.content), nil
}
