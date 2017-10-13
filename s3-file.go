package main

import (
  "bytes"
  "errors"
  "io"
  "os"
  "sync"
  "syscall"
  "time"

  "github.com/rlmcpherson/s3gof3r"
)

type orderedS3Writer struct {
  nextOffset int64
  dataBuffer map[int64][]byte
  streamingWriter io.WriteCloser
  dataBufferLock sync.RWMutex
}

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
  orderedS3Writer
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
  if f.isdir { // This can't happen
    return nil, os.ErrInvalid
  }
  return f, nil
}

func (f *s3File) WriteAt(data []byte, offset int64) (int, error) {
  f.dataBufferLock.Lock()

  if f.dataBuffer == nil { // somebody's first time?
    f.dataBuffer = make(map[int64][]byte)
  }

  if offset == f.nextOffset { // we have a hit!
    f.streamingWriter.Write(data)

    delete(f.dataBuffer, offset) // if we recursed to get here this could happen

    f.nextOffset = offset + int64(len(data))
  } else {                    // we have a miss!
    f.dataBuffer[offset] = data
  }

  // If we hit maybe we wrote some data that got us up to an offset that we
  // have in our map already. Write that piece and recurse
  if value, ok := f.dataBuffer[f.nextOffset]; ok {
    // someone else's problem
    f.dataBufferLock.Unlock()
    // Recurse on our data structure
    f.WriteAt(value, f.nextOffset)

  } else {
    f.dataBufferLock.Unlock()
  }

  return len(data), nil
}

func (f *s3File) ReaderAt() (io.ReaderAt, error) {
  if f.isdir {
    return nil, os.ErrInvalid
  }

  return bytes.NewReader(f.content), nil
}


func (f *s3File) OpenStreamingWriter(accessKey, secretKey string) (io.WriteCloser, error) {
  if f.streamingWriter != nil {
    return f.streamingWriter, nil
  }

  keys := s3gof3r.Keys{AccessKey: accessKey, SecretKey: secretKey}

  s3 := s3gof3r.New("", keys)
  b := s3.Bucket(f.bucket)

  w, err := b.PutWriter(f.key, nil, nil)

  if err != nil {
      return nil, err
  }

  f.streamingWriter = w

  return w, nil
}

func (f *s3File) Close() (error) {
  if f.streamingWriter == nil {
    return errors.New("streaming writer not open")
  }

  f.streamingWriter.Close()

  return nil
}
