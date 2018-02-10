package main

import (
  "io"
  "os"
  "runtime/debug"
  "strconv"
  "sync"
  "syscall"
  "time"

  "github.com/rlmcpherson/s3gof3r"
)

const(
  _        = iota
  kb int64 = 1 << (10 * iota)
  mb
  gb
  tb
  pb
  eb
)

var concurrency = (func() int {
  val := os.Getenv("CONCURRENCY")

  if val == "" {
    return 1
  } else {
    i, _ := strconv.Atoi(val)

    return i
  }
})()

const partsize = 5 * mb

var gof3rConfig = &s3gof3r.Config{
  Concurrency: concurrency,
  PartSize: partsize,
  NTry: 10,
  Md5Check: false,
  Scheme: "https",
  Client: s3gof3r.ClientWithTimeout(5 * time.Second),
}

type orderedS3Reader struct {
  readBytesCount int64
  streamingReader io.ReadCloser
  readBufferLock sync.RWMutex
  readWaiters map[int64]chan struct{}
}

type orderedS3Writer struct {
  nextOffset int64
  writeBuffer map[int64]*[]byte
  writtenBytesCount int64
  streamingWriter io.WriteCloser
  writeBufferLock sync.RWMutex
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
  size        int64
  orderedS3Writer
  orderedS3Reader
  *s3fs
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
  return f.size
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

func (f *s3File) waitFor(offset int64) {
  waiting := make(chan struct{})
  defer close(waiting)

  f.readWaiters[offset] = waiting
  defer delete(f.readWaiters, offset)

  f.readBufferLock.Unlock()
  for range waiting {
    break
  }
  f.readBufferLock.Lock()
}

func (f *s3File) notifyWaiting(offset int64) {
  channel, ok := f.readWaiters[offset]

  if ok {
    channel <- struct{}{}
  }
}

func (f *s3File) ReadAt(buffer []byte, offset int64) (int, error) {
  f.readBufferLock.Lock()
  defer f.readBufferLock.Unlock()

  if offset != f.readBytesCount {
    f.waitFor(offset)
  }

  // read the data
  n, err := f.streamingReader.Read(buffer)

  // update our position
  f.readBytesCount += int64(n)

  // let the next guy know
  f.notifyWaiting(f.readBytesCount)

  return n, err
}

func (f *s3File) WriteAt(data []byte, offset int64) (int, error) {
  f.writeBufferLock.Lock()

  if f.writeBuffer == nil { // somebody's first time?
    f.writeBuffer = make(map[int64]*[]byte)
  }

  if offset == f.nextOffset { // we have a hit!
    f.writtenBytesCount += int64(len(data))
    f.streamingWriter.Write(data)

    delete(f.writeBuffer, offset) // if we recursed to get here this could happen

    f.nextOffset = offset + int64(len(data))
  } else {                    // we have a miss!
    f.writeBuffer[offset] = &data
  }

  // If we hit maybe we wrote some data that got us up to an offset that we
  // have in our map already. Write that piece and recurse
  if value, ok := f.writeBuffer[f.nextOffset]; ok {
    // someone else's problem
    f.writeBufferLock.Unlock()
    // Recurse on our data structure
    f.WriteAt(*value, f.nextOffset)

  } else {
    f.writeBufferLock.Unlock()
  }

  return len(data), nil
}

func (f *s3File) OpenStreamingReader(accessKey, secretKey string) (error) {
  f.readBufferLock.Lock()
  defer f.readBufferLock.Unlock()

  if f.streamingReader != nil {
    return nil
  }

  keys := s3gof3r.Keys{AccessKey: accessKey, SecretKey: secretKey}

  s3 := s3gof3r.New("", keys)
  b := s3.Bucket(f.bucket)

  r, _, err := b.GetReader(f.key, gof3rConfig)

  if err != nil {
    return err
  }

  f.readWaiters = make(map[int64] chan struct{})
  f.streamingReader = r

  return nil
}

func (f *s3File) OpenStreamingWriter(accessKey, secretKey string) (io.WriteCloser, error) {
  f.writeBufferLock.Lock()
  defer f.writeBufferLock.Unlock()

  if f.streamingWriter != nil {
    return f.streamingWriter, nil
  }

  keys := s3gof3r.Keys{AccessKey: accessKey, SecretKey: secretKey}

  s3 := s3gof3r.New("", keys)
  b := s3.Bucket(f.bucket)

  w, err := b.PutWriter(f.key, nil, gof3rConfig)

  if err != nil {
    return nil, err
  }

  f.streamingWriter = w

  return w, nil
}

func (f *s3File) Close() (error) {
  var err error

  if f.streamingWriter != nil {
    f.writeBufferLock.Lock()
    defer f.writeBufferLock.Unlock()

    err = f.streamingWriter.Close()
    f.streamingWriter = nil
    debug.FreeOSMemory()

    if f.writtenBytesCount > 0 {
      go persist_event(f.sessionID, f.accessKey, "WRITE", f.writtenBytesCount)
    }
  }

  if f.streamingReader != nil {
    f.readBufferLock.Lock()
    defer f.readBufferLock.Unlock()

    err = f.streamingReader.Close()
    f.streamingReader = nil
    debug.FreeOSMemory()

    if f.readBytesCount > 0 {
      go persist_event(f.sessionID, f.accessKey, "READ", f.readBytesCount)
    }
  }

  if err != nil {
    return err
  }

  return err
}
